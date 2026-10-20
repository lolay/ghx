package ghapi

import (
	"context"
	"sync"
	"time"

	"github.com/google/go-github/v67/github"
)

// unknownLimitWait is how long a Pacer waits out a rate limit that says
// neither when to retry nor when it resets: GitHub's advice for a secondary
// limit with no Retry-After is at least a minute.
const unknownLimitWait = time.Minute

// Pacer holds every request that goes through it while a rate limit lasts,
// across goroutines: when one request hits a limit, the others wait too
// rather than spend more of it. Its zero value is ready to use.
type Pacer struct {
	// OnPause, when set, is called each time a limit pushes the resume time
	// later, with that time and the limit.
	OnPause func(until time.Time, limit *RateLimitError)
	// Sleep waits for d or until ctx is done; nil uses a timer. Tests swap it.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now is the clock; nil uses time.Now.
	Now func() time.Time

	mu    sync.Mutex
	until time.Time
}

// Ready waits until no limit holds requests back, or ctx is done.
func (p *Pacer) Ready(ctx context.Context) error {
	p.mu.Lock()
	until := p.until
	p.mu.Unlock()
	if wait := until.Sub(p.now()); wait > 0 {
		return p.sleep(ctx, wait)
	}
	return ctx.Err()
}

// Limited records a limit and waits it out: until its Retry-After, else a
// second past its reset, else a minute.
func (p *Pacer) Limited(ctx context.Context, limit *RateLimitError) error {
	now := p.now()
	var until time.Time
	switch {
	case limit.RetryAfter > 0:
		until = now.Add(limit.RetryAfter)
	case !limit.Reset.IsZero():
		// The reset is whole seconds; a second's slack keeps the retry after it.
		until = limit.Reset.Add(time.Second)
		if !until.After(now) {
			until = now.Add(time.Second)
		}
	default:
		until = now.Add(unknownLimitWait)
	}

	p.mu.Lock()
	later := until.After(p.until)
	if later {
		p.until = until
	}
	p.mu.Unlock()
	if later && p.OnPause != nil {
		p.OnPause(until, limit)
	}
	return p.Ready(ctx)
}

func (p *Pacer) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Pacer) sleep(ctx context.Context, d time.Duration) error {
	if p.Sleep != nil {
		return p.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// pacedClient is a go-github client whose calls wait for the pacer, and wait
// out and retry a rate limit, when it has one.
type pacedClient struct {
	token  string
	client *github.Client
	pacer  *Pacer
}

// do runs call, again after each rate limit while there is a pacer.
func (c *pacedClient) do(ctx context.Context, call func(*github.Client) error) error {
	for {
		if c.pacer != nil {
			if err := c.pacer.Ready(ctx); err != nil {
				return err
			}
		}
		err := call(c.client)
		limit := asRateLimit(err)
		if limit == nil || c.pacer == nil {
			return err
		}
		if err := c.pacer.Limited(ctx, limit); err != nil {
			return err
		}
		// go-github remembers a limit and refuses requests itself until its
		// own clock says it has lifted; the pacer's wait is the one that counts.
		c.client = newClient(c.token)
	}
}
