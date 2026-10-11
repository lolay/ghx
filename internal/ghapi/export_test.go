package ghapi

import (
	"net/url"
	"testing"
)

// These aliases give the black-box tests in package ghapi_test the unexported
// helpers, without widening the package API.
var (
	ToRepoInfo     = toRepoInfo
	FormatAPIError = formatAPIError
)

// SetBaseURL points the go-github client at rawURL (an httptest server, whose
// URL has no trailing slash) for the rest of the test and restores the public
// API afterwards. It changes a package variable, so the test can't run in
// parallel.
func SetBaseURL(t *testing.T, rawURL string) {
	t.Helper()
	u, err := url.Parse(rawURL + "/")
	if err != nil {
		t.Fatalf("parse base URL %q: %v", rawURL, err)
	}
	orig := baseURL
	baseURL = u
	t.Cleanup(func() { baseURL = orig })
}
