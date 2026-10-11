// Package buildinfo holds the version a build stamped into the binary, so
// `ghx --version` can name the exact commit it was built from.
//
// The Makefile's build target, and goreleaser for release builds, set the
// three variables with the linker:
//
//	go build -ldflags "-X github.com/lolay/ghx/internal/buildinfo.Version=0.1.0 \
//	  -X github.com/lolay/ghx/internal/buildinfo.Sha=abc1234 \
//	  -X github.com/lolay/ghx/internal/buildinfo.BuildCode=42"
//
// A plain `go build` or `go run` leaves them at their defaults and reports
// "dev".
package buildinfo

// Version is the release in version.txt: the last version released, not the
// next one. Unstamped builds report "dev".
var Version = "dev"

// Sha is the short HEAD commit the binary was built from, with ".dirty"
// appended when the working tree had uncommitted changes. Empty when
// unstamped.
var Sha string

// BuildCode is the commit count reachable from HEAD (git rev-list --count
// HEAD). Empty when unstamped.
var BuildCode string

// String returns the stamped build version, as Format does.
func String() string {
	return Format(Version, Sha, BuildCode)
}

// Format returns the build version in the form the versioning standard gives
// every channel, "<release>+<sha> (<buildCode>)", for example
// "0.1.0+abc1234 (42)" or "0.1.0+abc1234.dirty (42)" for a dirty tree. A part
// the build didn't stamp is left out with its punctuation rather than shown
// empty, so a build outside a git checkout still reads cleanly; an empty
// version reads "dev".
func Format(version, sha, buildCode string) string {
	s := version
	if s == "" {
		s = "dev"
	}
	if sha != "" {
		s += "+" + sha
	}
	if buildCode != "" {
		s += " (" + buildCode + ")"
	}
	return s
}
