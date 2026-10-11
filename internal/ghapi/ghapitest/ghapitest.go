// Package ghapitest is a fake of the two GitHub REST endpoints ghx calls,
// GET /user and GET /orgs/{org}/repos, served by httptest so tests never reach
// api.github.com. It paginates with a Link header the way GitHub does and
// records every request for assertions.
package ghapitest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
)

// Repo is the subset of GitHub's repository JSON that ghx reads.
type Repo struct {
	Name          string `json:"name"`
	CloneURL      string `json:"clone_url"`
	SSHURL        string `json:"ssh_url"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
	Private       bool   `json:"private"`
	Fork          bool   `json:"fork"`
	Size          int    `json:"size"`
	Language      string `json:"language,omitempty"`
	HasWiki       bool   `json:"has_wiki"`
	PushedAt      string `json:"pushed_at,omitempty"`
}

// Request is one request the server received.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	// Authorization is the raw Authorization header.
	Authorization string
}

// Server is the fake GitHub API.
type Server struct {
	srv   *httptest.Server
	token string
	login string

	mu       sync.Mutex
	repos    map[string][]Repo
	pageSize int
	requests []Request
}

// NewServer starts a fake API that accepts only the given token (anything else
// gets GitHub's 401) and reports login as the authenticated user. It closes
// with the test.
func NewServer(t *testing.T, token, login string) *Server {
	t.Helper()
	s := &Server{token: token, login: login, repos: map[string][]Repo{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /user", s.handleUser)
	mux.HandleFunc("GET /orgs/{org}/repos", s.handleOrgRepos)
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		if r.Header.Get("Authorization") != "Bearer "+s.token {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// URL is the server's base URL without a trailing slash.
func (s *Server) URL() string { return s.srv.URL }

// Close stops the server early, for a test of a network failure.
func (s *Server) Close() { s.srv.Close() }

// SetRepos replaces the repositories listed for org.
func (s *Server) SetRepos(org string, repos []Repo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos[org] = append([]Repo(nil), repos...)
}

// SetPageSize makes the server return at most n repos per page, whatever
// per_page asks for, so a short list still spans several pages.
func (s *Server) SetPageSize(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pageSize = n
}

// Requests returns a copy of every request received so far, in order.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// RedirectGitHub sends the requests ghx makes to api.github.com to this server
// for the rest of the test, by swapping http.DefaultTransport, which go-github
// uses when it is not given a client. Any request to another host fails, so a
// test can't reach the real network by accident. It changes a process-wide
// variable, so the calling test can't run in parallel.
func (s *Server) RedirectGitHub(t *testing.T) {
	t.Helper()
	target, err := url.Parse(s.srv.URL)
	if err != nil {
		t.Fatalf("ghapitest: parse server URL: %v", err)
	}
	orig := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.github.com" {
			return nil, fmt.Errorf("ghapitest: unexpected request to %s", r.URL.Host)
		}
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return orig.RoundTrip(r)
	})
	t.Cleanup(func() { http.DefaultTransport = orig })
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func (s *Server) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, Request{
		Method:        r.Method,
		Path:          r.URL.Path,
		Query:         r.URL.Query(),
		Authorization: r.Header.Get("Authorization"),
	})
}

func (s *Server) handleUser(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"login": s.login})
}

func (s *Server) handleOrgRepos(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	repos, ok := s.repos[r.PathValue("org")]
	pageSize := s.pageSize
	s.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}

	q := r.URL.Query()
	if pageSize <= 0 {
		pageSize = atoiOr(q.Get("per_page"), 30)
	}
	page := max(atoiOr(q.Get("page"), 1), 1)

	start := min((page-1)*pageSize, len(repos))
	end := min(start+pageSize, len(repos))
	if end < len(repos) {
		next := url.Values{}
		for k, v := range q {
			next[k] = v
		}
		next.Set("page", strconv.Itoa(page+1))
		w.Header().Set("Link", fmt.Sprintf(`<%s%s?%s>; rel="next"`, s.srv.URL, r.URL.Path, next.Encode()))
	}
	writeJSON(w, http.StatusOK, repos[start:end])
}

func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The client is a test; a failed write shows up as its own failure.
	_ = json.NewEncoder(w).Encode(v)
}
