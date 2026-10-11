package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/config"
)

func ptr[T any](v T) *T { return &v }

func TestSourceLabels(t *testing.T) {
	assert.Equal(t, config.Source("<path>/.ghx.toml"), config.SourceLocal)
	assert.Equal(t, config.Source("~/.ghx.toml"), config.SourceHome)
}

func TestResolveString(t *testing.T) {
	tests := []struct {
		name       string
		flag       *string
		home       config.Config
		local      config.Config
		want       string
		wantSource config.Source
	}{
		{"flag beats local and home", ptr("flag"), config.Config{"k": "home"}, config.Config{"k": "local"}, "flag", config.SourceFlag},
		{"empty flag still counts as set", ptr(""), config.Config{"k": "home"}, nil, "", config.SourceFlag},
		{"local beats home", nil, config.Config{"k": "home"}, config.Config{"k": "local"}, "local", config.SourceLocal},
		{"home when only home has it", nil, config.Config{"k": "home"}, config.Config{}, "home", config.SourceHome},
		{"default when unset", nil, config.Config{}, config.Config{}, "def", config.SourceDefault},
		{"default when configs are nil", nil, nil, nil, "def", config.SourceDefault},
		{"wrong type in local falls through to home", nil, config.Config{"k": "home"}, config.Config{"k": int64(1)}, "home", config.SourceHome},
		{"wrong type everywhere falls to default", nil, config.Config{"k": true}, config.Config{"k": int64(1)}, "def", config.SourceDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, src := config.ResolveString(tt.flag, tt.home, tt.local, "k", "def")
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantSource, src)
		})
	}
}

func TestResolveBool(t *testing.T) {
	tests := []struct {
		name       string
		flag       *bool
		home       config.Config
		local      config.Config
		want       bool
		wantSource config.Source
	}{
		{"flag false beats local and home true", ptr(false), config.Config{"k": true}, config.Config{"k": true}, false, config.SourceFlag},
		{"local beats home", nil, config.Config{"k": false}, config.Config{"k": true}, true, config.SourceLocal},
		{"home when only home has it", nil, config.Config{"k": true}, config.Config{}, true, config.SourceHome},
		{"default when unset", nil, config.Config{}, config.Config{}, true, config.SourceDefault},
		{"wrong type in local falls through to home", nil, config.Config{"k": false}, config.Config{"k": "yes"}, false, config.SourceHome},
		{"wrong type everywhere falls to default", nil, config.Config{"k": "no"}, config.Config{"k": "yes"}, true, config.SourceDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, src := config.ResolveBool(tt.flag, tt.home, tt.local, "k", true)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantSource, src)
		})
	}
}

func TestResolveInt(t *testing.T) {
	tests := []struct {
		name       string
		flag       *int
		home       config.Config
		local      config.Config
		want       int
		wantSource config.Source
	}{
		{"flag beats local and home", ptr(9), config.Config{"k": int64(1)}, config.Config{"k": int64(2)}, 9, config.SourceFlag},
		{"local beats home", nil, config.Config{"k": int64(1)}, config.Config{"k": int64(2)}, 2, config.SourceLocal},
		{"home when only home has it", nil, config.Config{"k": int64(1)}, config.Config{}, 1, config.SourceHome},
		{"default when unset", nil, config.Config{}, config.Config{}, 4, config.SourceDefault},
		{"TOML float truncates", nil, config.Config{}, config.Config{"k": 2.9}, 2, config.SourceLocal},
		{"int value", nil, config.Config{}, config.Config{"k": 3}, 3, config.SourceLocal},
		{"int32 value", nil, config.Config{}, config.Config{"k": int32(5)}, 5, config.SourceLocal},
		{"wrong type in local falls through to home", nil, config.Config{"k": int64(1)}, config.Config{"k": "two"}, 1, config.SourceHome},
		{"wrong type everywhere falls to default", nil, config.Config{"k": true}, config.Config{"k": "two"}, 4, config.SourceDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, src := config.ResolveInt(tt.flag, tt.home, tt.local, "k", 4)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantSource, src)
		})
	}
}

func TestResolveOptionalInt(t *testing.T) {
	tests := []struct {
		name        string
		flag        *int
		home        config.Config
		local       config.Config
		want        int
		wantSource  config.Source
		wantPresent bool
	}{
		{"flag beats local and home", ptr(0), config.Config{"k": int64(1)}, config.Config{"k": int64(2)}, 0, config.SourceFlag, true},
		{"local beats home", nil, config.Config{"k": int64(1)}, config.Config{"k": int64(2)}, 2, config.SourceLocal, true},
		{"home when only home has it", nil, config.Config{"k": int64(1)}, config.Config{}, 1, config.SourceHome, true},
		{"absent when unset", nil, config.Config{}, config.Config{}, 0, config.SourceDefault, false},
		{"wrong type everywhere is absent", nil, config.Config{"k": "x"}, config.Config{"k": false}, 0, config.SourceDefault, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, src, present := config.ResolveOptionalInt(tt.flag, tt.home, tt.local, "k")
			assert.Equal(t, tt.wantPresent, present)
			assert.Equal(t, tt.wantSource, src)
			if tt.wantPresent {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestResolveOptionalString(t *testing.T) {
	tests := []struct {
		name        string
		flag        *string
		home        config.Config
		local       config.Config
		want        string
		wantSource  config.Source
		wantPresent bool
	}{
		{"flag beats local and home", ptr("flag"), config.Config{"k": "home"}, config.Config{"k": "local"}, "flag", config.SourceFlag, true},
		{"local beats home", nil, config.Config{"k": "home"}, config.Config{"k": "local"}, "local", config.SourceLocal, true},
		{"home when only home has it", nil, config.Config{"k": "home"}, config.Config{}, "home", config.SourceHome, true},
		{"absent when unset", nil, config.Config{}, config.Config{}, "", config.SourceDefault, false},
		{"wrong type everywhere is absent", nil, config.Config{"k": int64(1)}, config.Config{"k": true}, "", config.SourceDefault, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, src, present := config.ResolveOptionalString(tt.flag, tt.home, tt.local, "k")
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantSource, src)
			assert.Equal(t, tt.wantPresent, present)
		})
	}
}

func TestResolveToken(t *testing.T) {
	tests := []struct {
		name       string
		flag       string
		env        string
		home       config.Config
		local      config.Config
		want       string
		wantSource config.Source
	}{
		{"flag beats env, local and home", "flag", "env", config.Config{"token": "home"}, config.Config{"token": "local"}, "flag", config.SourceFlag},
		{"env beats local and home", "", "env", config.Config{"token": "home"}, config.Config{"token": "local"}, "env", config.SourceEnv},
		{"local beats home", "", "", config.Config{"token": "home"}, config.Config{"token": "local"}, "local", config.SourceLocal},
		{"home when only home has it", "", "", config.Config{"token": "home"}, config.Config{}, "home", config.SourceHome},
		{"empty local token falls through to home", "", "", config.Config{"token": "home"}, config.Config{"token": ""}, "home", config.SourceHome},
		{"non-string local token falls through to home", "", "", config.Config{"token": "home"}, config.Config{"token": int64(1)}, "home", config.SourceHome},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GITHUB_TOKEN", tt.env)

			got, src, err := config.ResolveToken(tt.flag, tt.home, tt.local)
			require.NoError(t, err)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantSource, src)
		})
	}
}

func TestResolveToken_ReturnsErrNoTokenWhenNothingIsSet(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	got, src, err := config.ResolveToken("", config.Config{"token": ""}, config.Config{})

	require.ErrorIs(t, err, config.ErrNoToken)
	assert.Empty(t, got)
	assert.Empty(t, src)
}
