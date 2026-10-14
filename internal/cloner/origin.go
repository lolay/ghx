package cloner

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// ErrNoOrigin is returned by ReadOrigin for a clone with no remote.origin.url.
var ErrNoOrigin = errors.New("no origin URL")

// OriginRepoName returns the repo name an origin URL points at: its last
// non-empty path segment, split on "/", "\" and ":", without a trailing
// ".git". Every form a clone's origin takes reduces to the same name:
// "https://github.com/org/x.git", "https://user:tok@github.com/org/x",
// "git@github.com:org/x.git", "ssh://git@github.com/org/x/", "/srv/git/x.git"
// and "C:\git\x.git" all give "x". Host and owner are dropped on purpose, so a
// renamed org, a GitHub Enterprise host or an SSH host alias still matches.
// ok is false when there is no name to give, such as for an empty URL.
func OriginRepoName(originURL string) (name string, ok bool) {
	segments := strings.FieldsFunc(strings.TrimSpace(originURL), func(r rune) bool {
		return r == '/' || r == '\\' || r == ':'
	})
	if len(segments) == 0 {
		return "", false
	}
	name = segments[len(segments)-1]
	if strings.HasSuffix(strings.ToLower(name), ".git") {
		name = name[:len(name)-len(".git")]
	}
	return name, name != ""
}

// ReadOrigin returns repoDir's remote.origin.url as its config holds it. It
// asks git config rather than git remote get-url, which applies any
// url.<base>.insteadOf rewrite and so would report where git fetches from, not
// what the clone says it is. git is pointed at repoDir's own .git directory,
// so a directory whose .git is broken can't borrow the origin of a repo
// around it. gitPath is the git FindGit returned; "" looks it up. A clone with
// no origin gives ErrNoOrigin. Errors don't name repoDir: the caller does.
func ReadOrigin(ctx context.Context, gitPath, repoDir string) (string, error) {
	if gitPath == "" {
		found, err := FindGit(ctx)
		if err != nil {
			return "", err
		}
		gitPath = found
	}
	g := gitRunner{path: gitPath}
	res := g.run(ctx, "", "--git-dir="+filepath.Join(repoDir, ".git"), "config", "--local", "--get", "remote.origin.url")
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// git config --get exits 1, printing nothing, when the key isn't set.
	origin := strings.TrimSpace(res.Stdout)
	switch {
	case res.ExitCode == 1 && origin == "", res.Success() && origin == "":
		return "", ErrNoOrigin
	case !res.Success():
		return "", fmt.Errorf("reading the origin URL: %s", res.Message())
	}
	return origin, nil
}

// DisplayURL returns rawURL fit to show the user: a URL's user info, which a
// clone made by an older ghx holds the token in, is dropped. Anything that
// doesn't parse as a URL with user info, such as an scp-style SSH address or a
// path, comes back as it is.
func DisplayURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.User == nil || u.Scheme == "" {
		return rawURL
	}
	u.User = nil
	return u.String()
}
