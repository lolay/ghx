package ghapi_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/ghapi"
	"github.com/lolay/ghx/internal/ghapi/ghapitest"
)

// fakeClock is a clock that only sleeps move, and records each sleep.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return ctx.Err()
}

func (c *fakeClock) Sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.sleeps...)
}

type pause struct {
	until time.Time
	limit string
}

// newPacer is a Pacer on clock that records each pause.
func newPacer(clock *fakeClock) (*ghapi.Pacer, func() []pause) {
	var mu sync.Mutex
	var pauses []pause
	p := &ghapi.Pacer{
		Now:   clock.Now,
		Sleep: clock.Sleep,
		OnPause: func(until time.Time, limit *ghapi.RateLimitError) {
			mu.Lock()
			defer mu.Unlock()
			pauses = append(pauses, pause{until, limit.Limit()})
		},
	}
	return p, func() []pause {
		mu.Lock()
		defer mu.Unlock()
		return append([]pause(nil), pauses...)
	}
}

func TestListBranches_WaitsOutARateLimitAndRetries(t *testing.T) {
	// The fake's reset is 14:05 UTC; the clock starts before it.
	start := ghapitest.RateLimitReset.Add(-10 * time.Minute)
	tests := []struct {
		name      string
		failure   ghapitest.Failure
		wantSleep time.Duration
		wantLimit string
	}{
		{"secondary, for its Retry-After", ghapitest.SecondaryLimit, time.Minute, "GitHub secondary rate limit"},
		{"primary over REST, to a second past the reset", ghapitest.PrimaryLimit, 10*time.Minute + time.Second, "GitHub rate limit"},
		{"GraphQL RATE_LIMITED, to a second past the reset", ghapitest.RateLimited, 10*time.Minute + time.Second, "GitHub rate limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newAPI(t)
			srv.SetBranches("acme", "api", "main", numbered(250))
			// Fail the second page twice: the retry repeats that page, not the
			// whole repo.
			clock := &fakeClock{now: start}
			pacer, pauses := newPacer(clock)
			var pages []int
			opts := ghapi.BranchOptions{Pacer: pacer, OnPage: func(p int) {
				pages = append(pages, p)
				if p == 2 {
					srv.Fail("acme", "api", tt.failure, 2)
				}
			}}

			got, err := ghapi.ListBranches(t.Context(), testToken, "acme", "api", opts)
			require.NoError(t, err)

			assert.Len(t, got.Branches, 250, "the whole list, none repeated")
			assert.Equal(t, []int{1, 2, 3}, pages)
			assert.Equal(t, 5, countPath(srv, "/graphql"), "three pages and two retries")
			require.NotEmpty(t, clock.Sleeps())
			assert.Equal(t, tt.wantSleep, clock.Sleeps()[0])
			require.NotEmpty(t, pauses())
			assert.Equal(t, tt.wantLimit, pauses()[0].limit)
			assert.True(t, pauses()[0].until.Equal(start.Add(tt.wantSleep)))
		})
	}
}

func TestPacer(t *testing.T) {
	start := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

	t.Run("a limit with nothing to go on waits a minute", func(t *testing.T) {
		clock := &fakeClock{now: start}
		pacer, _ := newPacer(clock)

		require.NoError(t, pacer.Limited(t.Context(), &ghapi.RateLimitError{Secondary: true}))
		assert.Equal(t, []time.Duration{time.Minute}, clock.Sleeps())
	})

	t.Run("a reset already past waits a second", func(t *testing.T) {
		clock := &fakeClock{now: start}
		pacer, _ := newPacer(clock)

		require.NoError(t, pacer.Limited(t.Context(), &ghapi.RateLimitError{Reset: start.Add(-time.Hour)}))
		assert.Equal(t, []time.Duration{time.Second}, clock.Sleeps())
	})

	t.Run("an earlier limit doesn't shorten the pause or announce it again", func(t *testing.T) {
		clock := &fakeClock{now: start}
		pacer, pauses := newPacer(clock)
		pacer.Sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }

		require.NoError(t, pacer.Limited(t.Context(), &ghapi.RateLimitError{RetryAfter: 5 * time.Minute}))
		require.NoError(t, pacer.Limited(t.Context(), &ghapi.RateLimitError{RetryAfter: time.Minute}))

		require.Len(t, pauses(), 1)
		assert.True(t, pauses()[0].until.Equal(start.Add(5*time.Minute)))
	})

	t.Run("Ready holds every caller until the pause ends", func(t *testing.T) {
		clock := &fakeClock{now: start}
		pacer, _ := newPacer(clock)
		pacer.Sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
		require.NoError(t, pacer.Limited(t.Context(), &ghapi.RateLimitError{RetryAfter: time.Minute}))

		var waited []time.Duration
		pacer.Sleep = func(ctx context.Context, d time.Duration) error { waited = append(waited, d); return ctx.Err() }
		clock.now = start.Add(20 * time.Second)
		require.NoError(t, pacer.Ready(t.Context()))
		clock.now = start.Add(2 * time.Minute)
		require.NoError(t, pacer.Ready(t.Context()))

		assert.Equal(t, []time.Duration{40 * time.Second}, waited, "the second call is past the pause")
	})

	t.Run("a cancel ends the wait", func(t *testing.T) {
		pacer := &ghapi.Pacer{}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := pacer.Limited(ctx, &ghapi.RateLimitError{RetryAfter: time.Hour})
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("the real timer waits", func(t *testing.T) {
		pacer := &ghapi.Pacer{}
		began := time.Now()

		require.NoError(t, pacer.Limited(t.Context(), &ghapi.RateLimitError{RetryAfter: 20 * time.Millisecond}))
		assert.GreaterOrEqual(t, time.Since(began), 20*time.Millisecond)
	})
}

func TestListBranches_ACancelDuringARateLimitWaitStops(t *testing.T) {
	srv := newAPI(t)
	srv.SetBranches("acme", "api", "main", numbered(3))
	srv.Fail("acme", "api", ghapitest.SecondaryLimit, 0)
	ctx, cancel := context.WithCancel(t.Context())
	pacer := &ghapi.Pacer{OnPause: func(time.Time, *ghapi.RateLimitError) { cancel() }}

	_, err := ghapi.ListBranches(ctx, testToken, "acme", "api", ghapi.BranchOptions{Pacer: pacer})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, countPath(srv, "/graphql"))
}
