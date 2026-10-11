package cloner

import (
	"encoding/base64"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// credential hands git the token for HTTPS through git's environment, never
// its arguments or a file. GIT_CONFIG_COUNT, GIT_CONFIG_KEY_<n> and
// GIT_CONFIG_VALUE_<n> (git 2.31 and later) set http.<scheme>://<host>/.extraheader
// for the one command, so:
//
//   - the token is in no clone URL, so git never writes it to .git/config and
//     it isn't in the process list (another user can read a process's
//     arguments, not its environment);
//   - the header goes only to the clone URL's own scheme and host;
//   - it is sent with the first request, so a credential helper (osxkeychain,
//     Git Credential Manager) is never asked, can't prompt and can't store it.
//
// This works the same on macOS, Linux and Windows, unlike GIT_ASKPASS, which
// needs a helper executable (a .exe or .bat on Windows) and is consulted only
// after the credential helpers.
type credential struct {
	token string
	// basic is the base64 of x-access-token:<token>, the header's value.
	basic string
}

func newCredential(token string) credential {
	if token == "" {
		return credential{}
	}
	return credential{
		token: token,
		basic: base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token)),
	}
}

// secrets are the strings a git message must never show: the token and the
// header value that encodes it.
func (c credential) secrets() []string {
	return []string{c.token, c.basic}
}

// env returns the environment for a git command that fetches from the host of
// scopeURL: this process's environment plus the header, scoped to that URL's
// scheme and host. It returns nil, so the command inherits the environment
// unchanged, when there is no token or scopeURL isn't HTTPS: the token is
// never sent in the clear or to a local path.
func (c credential) env(scopeURL string) []string {
	if c.token == "" {
		return nil
	}
	u, err := url.Parse(scopeURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil
	}
	environ := os.Environ()
	// Add to any GIT_CONFIG_COUNT the user set rather than replacing it, so
	// their own entries still apply.
	n := 0
	for _, kv := range environ {
		if v, ok := strings.CutPrefix(kv, "GIT_CONFIG_COUNT="); ok {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				n = parsed
			}
		}
	}
	i := strconv.Itoa(n)
	return append(environ,
		"GIT_CONFIG_COUNT="+strconv.Itoa(n+1),
		"GIT_CONFIG_KEY_"+i+"=http."+u.Scheme+"://"+u.Host+"/.extraheader",
		"GIT_CONFIG_VALUE_"+i+"=AUTHORIZATION: basic "+c.basic,
	)
}

// stripUserInfo returns rawURL without its user info when it is an HTTPS URL
// that has some, as clones made by ghx before the token moved to the
// environment do (https://<token>@github.com/...). ok is false for anything
// else, SSH and local paths included, which are left alone.
func stripUserInfo(rawURL string) (clean string, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User == nil {
		return "", false
	}
	u.User = nil
	return u.String(), true
}
