// Package ghapi is a thin wrapper around go-github that exposes just
// the operations ghx needs: token validation and listing of all
// repositories in an organization.
package ghapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/go-github/v67/github"
)

// RepoInfo is the slim struct used throughout the rest of the code.
// It mirrors the Python RepoInfo dataclass so the JSON manifest format
// remains identical.
type RepoInfo struct {
	Name          string
	CloneURL      string
	SSHURL        string
	DefaultBranch string
	Archived      bool
	Private       bool
	Fork          bool
	SizeKB        int
	Language      string // empty string when unknown
	HasWiki       bool
	PushedAt      string // RFC3339, empty when unset
}

// ValidateToken calls GET /user to confirm the token is valid, returning
// the authenticated login.
func ValidateToken(ctx context.Context, token string) (string, error) {
	client := github.NewClient(nil).WithAuthToken(token)
	user, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return "", formatAPIError("Token validation failed", err)
	}
	if user.Login == nil {
		return "", errors.New("token validation returned empty login")
	}
	return *user.Login, nil
}

// ListOrgRepos pages through all repositories in the given org and
// returns them as RepoInfo values. Uses type=all to match the Python
// behavior (subsequent --type filtering happens in FilterRepos).
func ListOrgRepos(ctx context.Context, token, org string) ([]RepoInfo, error) {
	client := github.NewClient(nil).WithAuthToken(token)

	opts := &github.RepositoryListByOrgOptions{
		Type:        "all",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var all []RepoInfo
	for {
		repos, resp, err := client.Repositories.ListByOrg(ctx, org, opts)
		if err != nil {
			return nil, formatAPIError(
				fmt.Sprintf("Failed to access organization %q", org), err,
			)
		}
		for _, r := range repos {
			all = append(all, toRepoInfo(r))
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

func toRepoInfo(r *github.Repository) RepoInfo {
	info := RepoInfo{
		Name:          deref(r.Name),
		CloneURL:      deref(r.CloneURL),
		SSHURL:        deref(r.SSHURL),
		DefaultBranch: deref(r.DefaultBranch),
		Archived:      derefBool(r.Archived),
		Private:       derefBool(r.Private),
		Fork:          derefBool(r.Fork),
		SizeKB:        derefInt(r.Size),
		Language:      deref(r.Language),
		HasWiki:       derefBool(r.HasWiki),
	}
	if info.DefaultBranch == "" {
		info.DefaultBranch = "main"
	}
	if r.PushedAt != nil && !r.PushedAt.Time.IsZero() {
		info.PushedAt = r.PushedAt.Time.UTC().Format(time.RFC3339)
	}
	return info
}

// formatAPIError wraps a go-github error with a friendly prefix,
// surfacing the HTTP status and response message when available.
func formatAPIError(prefix string, err error) error {
	var ghErr *github.ErrorResponse
	if errors.As(err, &ghErr) {
		status := 0
		if ghErr.Response != nil {
			status = ghErr.Response.StatusCode
		}
		msg := ghErr.Message
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s (%d): %s", prefix, status, msg)
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefBool(p *bool) bool {
	return p != nil && *p
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
