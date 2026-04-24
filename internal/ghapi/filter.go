package ghapi

import (
	"fmt"
	"regexp"
)

// FilterRepos filters by repo type ("all", "public", "private", "forks",
// "sources") and optional include/exclude regular expressions (substring
// match, equivalent to Python's re.search).
//
// An unknown repoType is treated as "all", mirroring the Python code.
// Returns an error only when include/exclude fail to compile.
func FilterRepos(repos []RepoInfo, repoType, include, exclude string) ([]RepoInfo, error) {
	var pred func(RepoInfo) bool
	switch repoType {
	case "public":
		pred = func(r RepoInfo) bool { return !r.Private }
	case "private":
		pred = func(r RepoInfo) bool { return r.Private }
	case "forks":
		pred = func(r RepoInfo) bool { return r.Fork }
	case "sources":
		pred = func(r RepoInfo) bool { return !r.Fork }
	case "all", "":
		pred = nil
	default:
		pred = nil
	}

	var includeRe, excludeRe *regexp.Regexp
	var err error
	if include != "" {
		includeRe, err = regexp.Compile(include)
		if err != nil {
			return nil, fmt.Errorf("invalid --include regex: %w", err)
		}
	}
	if exclude != "" {
		excludeRe, err = regexp.Compile(exclude)
		if err != nil {
			return nil, fmt.Errorf("invalid --exclude regex: %w", err)
		}
	}

	out := make([]RepoInfo, 0, len(repos))
	for _, r := range repos {
		if pred != nil && !pred(r) {
			continue
		}
		if includeRe != nil && !includeRe.MatchString(r.Name) {
			continue
		}
		if excludeRe != nil && excludeRe.MatchString(r.Name) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}
