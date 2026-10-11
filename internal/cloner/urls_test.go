package cloner_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
)

func TestAuthenticatedHTTPS(t *testing.T) {
	tests := []struct {
		name  string
		url   string
		token string
		want  string
	}{
		{"https gets the token as userinfo", "https://github.com/acme/app.git", "tok", "https://tok@github.com/acme/app.git"},
		{"https without .git gets the token too", "https://github.com/acme/app", "tok", "https://tok@github.com/acme/app"},
		{"ssh is left alone", "git@github.com:acme/app.git", "tok", "git@github.com:acme/app.git"},
		{"a local path is left alone", "/srv/git/app.git", "tok", "/srv/git/app.git"},
		{"plain http is left alone", "http://example.com/app.git", "tok", "http://example.com/app.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cloner.AuthenticatedHTTPS(tt.url, tt.token))
		})
	}
}

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
		{"https carries the token", repo, false, "https://tok@github.com/acme/app.git"},
		{"ssh uses the ssh url as is", repo, true, "git@github.com:acme/app.git"},
		{
			"a clone url without .git still gets the token",
			ghapi.RepoInfo{CloneURL: "https://github.com/acme/app"},
			false, "https://tok@github.com/acme/app",
		},
		{
			"a local clone url passes through unchanged",
			ghapi.RepoInfo{CloneURL: "/srv/git/app.git"},
			false, "/srv/git/app.git",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cloner.ResolveURL(tt.repo, "tok", tt.useSSH))
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
			"https swaps .git for .wiki.git and adds the token",
			ghapi.RepoInfo{CloneURL: "https://github.com/acme/app.git"},
			false, "https://tok@github.com/acme/app.wiki.git",
		},
		{
			"https without .git gains .wiki.git",
			ghapi.RepoInfo{CloneURL: "https://github.com/acme/app"},
			false, "https://tok@github.com/acme/app.wiki.git",
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
			"a local clone url keeps its path and gets no token",
			ghapi.RepoInfo{CloneURL: "/srv/git/app.git"},
			false, "/srv/git/app.wiki.git",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cloner.ResolveWikiURL(tt.repo, "tok", tt.useSSH))
		})
	}
}
