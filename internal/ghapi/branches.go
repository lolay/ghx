package ghapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/go-github/v67/github"
)

// ErrRateLimited is wrapped by every error that means GitHub refused a request
// for its rate limits, primary or secondary, over REST or GraphQL.
var ErrRateLimited = errors.New("GitHub rate limit")

// RateLimitError says which rate limit GitHub applied and when to try again.
// It unwraps to ErrRateLimited.
type RateLimitError struct {
	// Secondary is true for a secondary (abuse) limit, false for the hourly
	// primary limit.
	Secondary bool
	// RetryAfter is GitHub's Retry-After, zero when it sent none.
	RetryAfter time.Duration
	// Reset is when the primary limit resets, zero when unknown.
	Reset time.Time
}

func (e *RateLimitError) Error() string {
	kind := "rate limit"
	if e.Secondary {
		kind = "secondary rate limit"
	}
	switch {
	case e.RetryAfter > 0:
		return fmt.Sprintf("GitHub %s hit; retry after %s", kind, e.RetryAfter)
	case !e.Reset.IsZero():
		return fmt.Sprintf("GitHub %s hit; it resets at %s", kind, e.Reset.UTC().Format("15:04 UTC"))
	default:
		return fmt.Sprintf("GitHub %s hit; try again later", kind)
	}
}

// Unwrap lets errors.Is match ErrRateLimited.
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// Branch is one branch of a repository, with what ghb needs to classify it.
type Branch struct {
	Name string
	// CommitOID is the sha the branch points at.
	CommitOID string
	// CommittedAt is the tip commit's committer date, zero when the tip is not
	// a commit.
	CommittedAt time.Time
	// AuthorName is the tip commit's author as git recorded it, and AuthorLogin
	// the GitHub account it maps to, empty when it maps to none.
	AuthorName  string
	AuthorLogin string
	Protected   bool
	// OpenPRs counts open pull requests whose head is this branch.
	OpenPRs int
	// MergedPR is the most recently updated merged pull request whose head is
	// this branch, nil when there is none.
	MergedPR *MergedPR
}

// MergedPR is a merged pull request, as far as classifying its head branch
// needs it.
type MergedPR struct {
	Number   int
	MergedAt time.Time
	// HeadOID is the sha of the PR's head when it merged.
	HeadOID string
}

// RepoBranches is every branch of one repository.
type RepoBranches struct {
	// DefaultBranch is empty for an empty repository.
	DefaultBranch string
	// Branches are in GitHub's order, which is by name.
	Branches []Branch
}

// branchesQuery reads 100 branches a page, GraphQL's maximum for a
// connection. Open and merged PRs are separate
// connections so that closed PRs can't crowd the one that matters out of a
// short window.
const branchesQuery = `query($owner: String!, $name: String!, $cursor: String) {
  repository(owner: $owner, name: $name) {
    defaultBranchRef { name }
    refs(refPrefix: "refs/heads/", first: 100, after: $cursor) {
      pageInfo { hasNextPage endCursor }
      nodes {
        name
        target {
          oid
          ... on Commit {
            committedDate
            author { name user { login } }
          }
        }
        open: associatedPullRequests(states: OPEN, first: 1) { totalCount }
        merged: associatedPullRequests(states: MERGED, first: 1, orderBy: {field: UPDATED_AT, direction: DESC}) {
          nodes { number mergedAt headRefOid }
        }
      }
    }
  }
}`

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type graphQLError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type branchesResponse struct {
	Data struct {
		Repository *struct {
			DefaultBranchRef *struct {
				Name string `json:"name"`
			} `json:"defaultBranchRef"`
			Refs struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []refNode `json:"nodes"`
			} `json:"refs"`
		} `json:"repository"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type refNode struct {
	Name   string `json:"name"`
	Target struct {
		OID           string    `json:"oid"`
		CommittedDate time.Time `json:"committedDate"`
		Author        *struct {
			Name string `json:"name"`
			User *struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"author"`
	} `json:"target"`
	Open struct {
		TotalCount int `json:"totalCount"`
	} `json:"open"`
	Merged struct {
		Nodes []struct {
			Number     int       `json:"number"`
			MergedAt   time.Time `json:"mergedAt"`
			HeadRefOID string    `json:"headRefOid"`
		} `json:"nodes"`
	} `json:"merged"`
}

// ListBranches reads every branch of owner/repo: one GraphQL request per 100
// branches, then one REST listing of the protected ones. onPage, when not nil,
// is called with the page number before each GraphQL request, for progress.
//
// A rate limit fails the call with a *RateLimitError; nothing is retried, so a
// caller never gets a partial list.
func ListBranches(ctx context.Context, token, owner, repo string, onPage func(page int)) (RepoBranches, error) {
	client := newClient(token)
	prefix := fmt.Sprintf("Failed to list branches of %s/%s", owner, repo)

	var out RepoBranches
	var cursor *string
	for page := 1; ; page++ {
		if onPage != nil {
			onPage(page)
		}
		resp, err := queryBranches(ctx, client, owner, repo, cursor)
		if err != nil {
			return RepoBranches{}, wrapAPIError(prefix, err, token)
		}
		r := resp.Data.Repository
		if r.DefaultBranchRef != nil {
			out.DefaultBranch = r.DefaultBranchRef.Name
		}
		for _, n := range r.Refs.Nodes {
			out.Branches = append(out.Branches, toBranch(n))
		}
		if !r.Refs.PageInfo.HasNextPage {
			break
		}
		next := r.Refs.PageInfo.EndCursor
		cursor = &next
	}

	protected, err := listProtected(ctx, client, owner, repo)
	if err != nil {
		return RepoBranches{}, wrapAPIError(prefix, err, token)
	}
	for i := range out.Branches {
		out.Branches[i].Protected = protected[out.Branches[i].Name]
	}
	return out, nil
}

// queryBranches posts one page of branchesQuery through go-github, which
// brings its auth, base URL and typed rate-limit errors with it.
func queryBranches(ctx context.Context, client *github.Client, owner, repo string, cursor *string) (*branchesResponse, error) {
	body := graphQLRequest{
		Query:     branchesQuery,
		Variables: map[string]any{"owner": owner, "name": repo, "cursor": cursor},
	}
	req, err := client.NewRequest(http.MethodPost, "graphql", body)
	if err != nil {
		return nil, err
	}
	var out branchesResponse
	resp, err := client.Do(ctx, req, &out)
	if err != nil {
		return nil, err
	}
	if len(out.Errors) > 0 {
		return nil, graphQLErrors(out.Errors, resp)
	}
	if out.Data.Repository == nil {
		return nil, errors.New("GraphQL returned no repository")
	}
	return &out, nil
}

// graphQLErrors turns the errors GraphQL reports with an HTTP 200 into one
// error. RATE_LIMITED, the primary limit, comes back this way rather than as
// a 403, so it maps to a *RateLimitError like the REST limits do.
func graphQLErrors(errs []graphQLError, resp *github.Response) error {
	var joined []error
	for _, e := range errs {
		if e.Type == "RATE_LIMITED" {
			return &RateLimitError{Reset: rateReset(resp)}
		}
		joined = append(joined, errors.New(e.Message))
	}
	return errors.Join(joined...)
}

// rateReset reads X-RateLimit-Reset, zero when it is absent.
func rateReset(resp *github.Response) time.Time {
	if resp == nil || resp.Response == nil {
		return time.Time{}
	}
	secs, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}
	}
	return time.Unix(secs, 0)
}

func listProtected(ctx context.Context, client *github.Client, owner, repo string) (map[string]bool, error) {
	protected := map[string]bool{}
	isProtected := true
	opts := &github.BranchListOptions{Protected: &isProtected, ListOptions: github.ListOptions{PerPage: 100}}
	for {
		branches, resp, err := client.Repositories.ListBranches(ctx, owner, repo, opts)
		if err != nil {
			return nil, err
		}
		for _, b := range branches {
			protected[b.GetName()] = true
		}
		if resp.NextPage == 0 {
			return protected, nil
		}
		opts.Page = resp.NextPage
	}
}

func toBranch(n refNode) Branch {
	b := Branch{
		Name:        n.Name,
		CommitOID:   n.Target.OID,
		CommittedAt: n.Target.CommittedDate,
		OpenPRs:     n.Open.TotalCount,
	}
	if a := n.Target.Author; a != nil {
		b.AuthorName = a.Name
		if a.User != nil {
			b.AuthorLogin = a.User.Login
		}
	}
	if len(n.Merged.Nodes) > 0 {
		m := n.Merged.Nodes[0]
		b.MergedPR = &MergedPR{Number: m.Number, MergedAt: m.MergedAt, HeadOID: m.HeadRefOID}
	}
	return b
}

// wrapAPIError is formatAPIError with go-github's rate-limit errors mapped to
// a *RateLimitError first, so the message says what to do rather than echoing
// the request.
func wrapAPIError(prefix string, err error, token string) error {
	var primary *github.RateLimitError
	var secondary *github.AbuseRateLimitError
	switch {
	case errors.As(err, &primary):
		err = &RateLimitError{Reset: primary.Rate.Reset.Time}
	case errors.As(err, &secondary):
		rl := &RateLimitError{Secondary: true}
		if secondary.RetryAfter != nil {
			rl.RetryAfter = *secondary.RetryAfter
		}
		err = rl
	}
	var rl *RateLimitError
	if errors.As(err, &rl) {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	return formatAPIError(prefix, err, token)
}
