package redact_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/redact"
)

func TestString(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		secrets []string
		want    string
	}{
		{"no secrets leaves the text", "fatal: not found", nil, "fatal: not found"},
		{"an empty secret is ignored", "fatal: not found", []string{""}, "fatal: not found"},
		{"every occurrence is masked", "tok and tok", []string{"tok"}, "*** and ***"},
		{"each secret is masked", "https://tok@github.com AUTHORIZATION: basic dG9r", []string{"tok", "dG9r"}, "https://***@github.com AUTHORIZATION: basic ***"},
		{"text without the secret is untouched", "all good", []string{"tok"}, "all good"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, redact.String(tt.in, tt.secrets...))
		})
	}
}

func TestError(t *testing.T) {
	t.Parallel()
	cause := errors.New("401 for token ghp_secret")
	wrapped := fmt.Errorf("listing repos: %w", cause)

	got := redact.Error(wrapped, "ghp_secret")

	assert.Equal(t, "listing repos: 401 for token ***", got.Error())
	require.ErrorIs(t, got, cause, "the cause stays reachable")
	assert.Same(t, wrapped, redact.Error(wrapped, "other"), "an error without the secret is returned as is")
	assert.NoError(t, redact.Error(nil, "ghp_secret"))
}
