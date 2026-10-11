package cloner_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
)

func TestResolveURL(t *testing.T) {
	repo := ghapi.RepoInfo{
		CloneURL: "https://github.com/acme/app.git",
		SSHURL:   "git@github.com:acme/app.git",
	}
	tests := []struct {
		name   string
		repo   ghapi.RepoInfo
		useSSH bool
		want   string
	}{
		{"https uses the plain clone url", repo, false, "https://github.com/acme/app.git"},
		{"ssh uses the ssh url as is", repo, true, "git@github.com:acme/app.git"},
		{
			"a clone url without .git is used as is",
			ghapi.RepoInfo{CloneURL: "https://github.com/acme/app"},
			false, "https://github.com/acme/app",
		},
		{
			"a local clone url passes through unchanged",
			ghapi.RepoInfo{CloneURL: "/srv/git/app.git"},
			false, "/srv/git/app.git",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cloner.ResolveURL(tt.repo, tt.useSSH))
		})
	}
}

func TestResolveWikiURL(t *testing.T) {
	tests := []struct {
		name   string
		repo   ghapi.RepoInfo
		useSSH bool
		want   string
	}{
		{
			"https swaps .git for .wiki.git",
			ghapi.RepoInfo{CloneURL: "https://github.com/acme/app.git"},
			false, "https://github.com/acme/app.wiki.git",
		},
		{
			"https without .git gains .wiki.git",
			ghapi.RepoInfo{CloneURL: "https://github.com/acme/app"},
			false, "https://github.com/acme/app.wiki.git",
		},
		{
			"ssh swaps .git for .wiki.git",
			ghapi.RepoInfo{SSHURL: "git@github.com:acme/app.git"},
			true, "git@github.com:acme/app.wiki.git",
		},
		{
			"ssh without .git gains .wiki",
			ghapi.RepoInfo{SSHURL: "git@github.com:acme/app"},
			true, "git@github.com:acme/app.wiki",
		},
		{
			"a local clone url keeps its path",
			ghapi.RepoInfo{CloneURL: "/srv/git/app.git"},
			false, "/srv/git/app.wiki.git",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cloner.ResolveWikiURL(tt.repo, tt.useSSH))
		})
	}
}

func TestStripUserInfo(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		want   string
		wantOK bool
	}{
		{"an old ghx origin loses its token", "https://ghp_tok@github.com/acme/app.git", "https://github.com/acme/app.git", true},
		{"a user and password are both removed", "https://user:pass@github.com/acme/app.git", "https://github.com/acme/app.git", true},
		{"a port is kept", "https://tok@git.example.com:8443/acme/app.git", "https://git.example.com:8443/acme/app.git", true},
		{"a plain https url is left alone", "https://github.com/acme/app.git", "", false},
		{"an scp-style ssh url is left alone", "git@github.com:acme/app.git", "", false},
		{"an ssh url is left alone", "ssh://git@github.com/acme/app.git", "", false},
		{"plain http is left alone", "http://tok@example.com/app.git", "", false},
		{"a local path is left alone", "/srv/git/app.git", "", false},
		{"a Windows path is left alone", `C:\src\upstream\app.git`, "", false},
		{"an empty url is left alone", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := cloner.StripUserInfo(tt.url)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
