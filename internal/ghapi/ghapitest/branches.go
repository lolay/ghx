package ghapitest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Branch is a branch the fake serves, in the shape of ghb's GraphQL query.
type Branch struct {
	Name string
	OID  string
	// CommittedDate is RFC 3339.
	CommittedDate string
	AuthorName    string
	// AuthorLogin is empty for an author with no GitHub account.
	AuthorLogin string
	Protected   bool
	OpenPRs     int
	// Merged is the most recently updated merged PR from this branch, if any.
	Merged *MergedPR
}

// MergedPR is a merged pull request whose head is a Branch.
type MergedPR struct {
	Number int
	// MergedAt is RFC 3339.
	MergedAt   string
	HeadRefOID string
}

// Failure is how the fake refuses a repo's GraphQL requests or an org's repo
// listing (see Fail and FailRepos).
type Failure int

const (
	// SecondaryLimit answers 403 with a Retry-After of 60 seconds, as GitHub
	// does for a secondary rate limit.
	SecondaryLimit Failure = iota + 1
	// PrimaryLimit answers 403 with X-RateLimit-Remaining 0, as GitHub does
	// when the hourly REST limit is spent.
	PrimaryLimit
	// RateLimited answers 200 with a GraphQL RATE_LIMITED error, as GitHub
	// does when the hourly GraphQL limit is spent.
	RateLimited
)

// RateLimitReset is the X-RateLimit-Reset the PrimaryLimit and RateLimited
// failures send.
var RateLimitReset = time.Date(2026, 10, 20, 14, 5, 0, 0, time.UTC)

// graphQLPageSize is the page the fake serves, whatever the query asks for:
// ghb always asks for GraphQL's maximum.
const graphQLPageSize = 100

type repoBranches struct {
	defaultBranch string
	branches      []Branch
}

// SetBranches sets the branches of owner/repo and its default branch. An
// empty defaultBranch serves an empty repository (a null defaultBranchRef).
func (s *Server) SetBranches(owner, repo, defaultBranch string, branches []Branch) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.branches[owner+"/"+repo] = repoBranches{defaultBranch: defaultBranch, branches: append([]Branch(nil), branches...)}
}

// Fail makes the next times GraphQL requests for owner/repo fail with f, or
// every one when times is 0 or less.
func (s *Server) Fail(owner, repo string, f Failure, times int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[owner+"/"+repo] = failure{kind: f, left: times, forever: times <= 0}
}

// FailRepos makes the next times requests to GET /orgs/{org}/repos fail with
// f, or every one when times is 0 or less. Only SecondaryLimit and
// PrimaryLimit make sense there: RateLimited is a GraphQL answer.
func (s *Server) FailRepos(org string, f Failure, times int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repoFailures[org] = failure{kind: f, left: times, forever: times <= 0}
}

type failure struct {
	kind    Failure
	left    int
	forever bool
}

// takeFailureLocked is the failure to answer a request for key with, counting
// it down in failures, or 0 for none. The caller holds s.mu.
func takeFailureLocked(failures map[string]failure, key string) Failure {
	f, ok := failures[key]
	if !ok {
		return 0
	}
	if !f.forever {
		if f.left--; f.left <= 0 {
			delete(failures, key)
		} else {
			failures[key] = f
		}
	}
	return f.kind
}

// writeFailure answers with f and reports true, or reports false for 0 and
// writes nothing.
func writeFailure(w http.ResponseWriter, f Failure) bool {
	switch f {
	case SecondaryLimit:
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusForbidden, map[string]string{
			"message":           "You have exceeded a secondary rate limit. Please wait a few minutes before you try again.",
			"documentation_url": "https://docs.github.com/rest/overview/rate-limits-for-the-rest-api#about-secondary-rate-limits",
		})
	case PrimaryLimit:
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(RateLimitReset.Unix(), 10))
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "API rate limit exceeded for user ID 1."})
	case RateLimited:
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(RateLimitReset.Unix(), 10))
		writeJSON(w, http.StatusOK, map[string]any{
			"data":   nil,
			"errors": []map[string]string{{"type": "RATE_LIMITED", "message": "API rate limit exceeded for user ID 1."}},
		})
	default:
		return false
	}
	return true
}

func (s *Server) handleBranches(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	rb, ok := s.branches[r.PathValue("owner")+"/"+r.PathValue("repo")]
	s.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	onlyProtected := r.URL.Query().Get("protected") == "true"
	type restBranch struct {
		Name      string `json:"name"`
		Protected bool   `json:"protected"`
	}
	var out []restBranch
	for _, b := range rb.branches {
		if !onlyProtected || b.Protected {
			out = append(out, restBranch{Name: b.Name, Protected: b.Protected})
		}
	}
	servePage(s, w, r, out)
}

type graphQLRequest struct {
	Query     string `json:"query"`
	Variables struct {
		Owner  string  `json:"owner"`
		Name   string  `json:"name"`
		Cursor *string `json:"cursor"`
	} `json:"variables"`
}

func (s *Server) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	var req graphQLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Problems parsing JSON"})
		return
	}
	full := req.Variables.Owner + "/" + req.Variables.Name

	s.mu.Lock()
	rb, ok := s.branches[full]
	failure := takeFailureLocked(s.failures, full)
	s.mu.Unlock()

	if writeFailure(w, failure) {
		return
	}

	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{"repository": nil},
			"errors": []map[string]any{{
				"type":    "NOT_FOUND",
				"path":    []string{"repository"},
				"message": fmt.Sprintf("Could not resolve to a Repository with the name '%s'.", full),
			}},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": rb.page(req.Variables.Cursor)}})
}

// page renders the repository for one page of refs. A cursor is the index of
// the page's first branch, opaque to the client as GitHub's are.
func (rb repoBranches) page(cursor *string) map[string]any {
	start := 0
	if cursor != nil {
		start, _ = strconv.Atoi(strings.TrimPrefix(*cursor, "cursor:"))
	}
	start = min(start, len(rb.branches))
	end := min(start+graphQLPageSize, len(rb.branches))

	nodes := make([]map[string]any, 0, end-start)
	for _, b := range rb.branches[start:end] {
		nodes = append(nodes, b.node())
	}
	repo := map[string]any{
		"defaultBranchRef": nil,
		"refs": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": end < len(rb.branches), "endCursor": "cursor:" + strconv.Itoa(end)},
			"nodes":    nodes,
		},
	}
	if rb.defaultBranch != "" {
		repo["defaultBranchRef"] = map[string]string{"name": rb.defaultBranch}
	}
	return repo
}

func (b Branch) node() map[string]any {
	var user any
	if b.AuthorLogin != "" {
		user = map[string]string{"login": b.AuthorLogin}
	}
	var committed any
	if b.CommittedDate != "" {
		committed = b.CommittedDate
	}
	merged := []map[string]any{}
	if b.Merged != nil {
		merged = append(merged, map[string]any{
			"number": b.Merged.Number, "mergedAt": b.Merged.MergedAt, "headRefOid": b.Merged.HeadRefOID,
		})
	}
	return map[string]any{
		"name": b.Name,
		"target": map[string]any{
			"oid":           b.OID,
			"committedDate": committed,
			"author":        map[string]any{"name": b.AuthorName, "user": user},
		},
		"open":   map[string]any{"totalCount": b.OpenPRs},
		"merged": map[string]any{"nodes": merged},
	}
}
