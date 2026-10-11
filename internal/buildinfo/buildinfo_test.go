package buildinfo_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lolay/ghx/internal/buildinfo"
)

func TestFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		version   string
		sha       string
		buildCode string
		want      string
	}{
		{"stamped from a clean tree", "0.1.0", "abc1234", "42", "0.1.0+abc1234 (42)"},
		{"stamped from a dirty tree", "0.1.0", "abc1234.dirty", "42", "0.1.0+abc1234.dirty (42)"},
		{"unstamped", "dev", "", "", "dev"},
		{"an empty version reads dev", "", "abc1234", "42", "dev+abc1234 (42)"},
		{"no git: version only", "0.1.0", "", "", "0.1.0"},
		{"no build code", "0.1.0", "abc1234", "", "0.1.0+abc1234"},
		{"no sha", "0.1.0", "", "42", "0.1.0 (42)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, buildinfo.Format(tt.version, tt.sha, tt.buildCode))
		})
	}
}

// TestString_Unstamped checks the defaults: `go test` builds this package
// without -ldflags, as `go build` and `go run` do.
func TestString_Unstamped(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "dev", buildinfo.Version)
	assert.Empty(t, buildinfo.Sha)
	assert.Empty(t, buildinfo.BuildCode)
	assert.Equal(t, "dev", buildinfo.String())
}
