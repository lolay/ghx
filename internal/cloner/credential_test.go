package cloner_test

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cloner"
	"github.com/lolay/ghx/internal/ghapi"
	"github.com/lolay/ghx/internal/gittest"
)

const (
	token = "ghp_cloner_test_token"
	// githubPrefix is where the test's HTTPS repos live; gittest.RewriteURL
	// serves it from the fixture's upstream directory.
	githubPrefix = "https://github.com/acme/"
	headerKey    = "http.https://github.com/.extraheader"
)

// basic is the header value git is given for token.
var basic = base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))

// httpsFixture is a fixture whose repos have real GitHub HTTPS clone URLs,
// which git fetches from the local upstream directory.
func httpsFixture(t *testing.T) fixture {
	t.Helper()
	f := newFixture(t)
	gittest.RewriteURL(t, githubPrefix, f.upstream)
	return f
}

// httpsRepo makes a bare upstream repo and returns the RepoInfo GitHub would
// list for it: HTTPS and SSH URLs on github.com.
func (f fixture) httpsRepo(t *testing.T, name string) ghapi.RepoInfo {
	t.Helper()
	gittest.NewBareRepo(t, f.upstream, name, "main")
	return ghapi.RepoInfo{
		Name:          name,
		CloneURL:      githubPrefix + name + ".git",
		SSHURL:        "git@github.com:acme/" + name + ".git",
		DefaultBranch: "main",
	}
}

func (f fixture) tokenOpts() cloner.Options {
	opts := f.opts()
	opts.Token = token
	return opts
}

// envValue returns the value of key in env, and whether it is set.
func envValue(env []string, key string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], key+"="); ok {
			return v, true
		}
	}
	return "", false
}

// subcommand is the git subcommand of a recorded call, after any -c options.
func subcommand(c cloner.GitCall) string {
	for i := 0; i < len(c.Args); i++ {
		if c.Args[i] == "-c" {
			i++
			continue
		}
		return c.Args[i]
	}
	return ""
}

// gitConfigs returns the contents of every .git/config under root.
func gitConfigs(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "config" && filepath.Base(filepath.Dir(path)) == ".git" {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			out[path] = string(data)
		}
		return nil
	})
	require.NoError(t, err)
	return out
}

func origin(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gittest.Git(t, dir, "config", "--local", "--get", "remote.origin.url"))
}

func TestCloneRepos_GivesGitTheTokenOnlyThroughItsEnvironment(t *testing.T) {
	f := httpsFixture(t)
	repo := f.httpsRepo(t, "app")
	repo.HasWiki = true
	gittest.NewBareRepo(t, f.upstream, "app.wiki", "master")
	opts := f.tokenOpts()
	opts.CloneWiki = true
	log := cloner.RecordGit(t, nil)

	first, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
	require.NoError(t, err)
	require.ElementsMatch(t, []cloner.Action{cloner.ActionCloned, cloner.ActionClonedWiki}, actionsByName(first)["app"])
	gittest.AddCommit(t, filepath.Join(f.upstream, "app.git"), "main", "NEWS.md", "news\n")
	second, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
	require.NoError(t, err)
	require.Equal(t, []cloner.Action{cloner.ActionPulled}, actionsByName(second)["app"])
	assert.FileExists(t, filepath.Join(f.out, "app", "NEWS.md"), "the pull went through the HTTPS URL")

	fetches := map[string]int{}
	for _, c := range log.Calls() {
		for _, arg := range c.Args {
			assert.NotContains(t, arg, token, "git argument in %v", c.Args)
			assert.NotContains(t, arg, basic, "git argument in %v", c.Args)
		}
		sub := subcommand(c)
		if sub != "clone" && sub != "pull" {
			assert.Nil(t, c.Env, "%v inherits the environment and gets no token", c.Args)
			continue
		}
		fetches[sub]++
		count, _ := envValue(c.Env, "GIT_CONFIG_COUNT")
		assert.Equal(t, "1", count, "%v", c.Args)
		key, _ := envValue(c.Env, "GIT_CONFIG_KEY_0")
		assert.Equal(t, headerKey, key, "the header goes to the clone URL's scheme and host only")
		value, _ := envValue(c.Env, "GIT_CONFIG_VALUE_0")
		assert.Equal(t, "AUTHORIZATION: basic "+basic, value)
	}
	assert.Equal(t, map[string]int{"clone": 2, "pull": 2}, fetches, "repo and wiki, cloned then pulled")

	for path, cfg := range gitConfigs(t, f.out) {
		assert.NotContains(t, cfg, token, path)
		assert.NotContains(t, cfg, basic, path)
	}
	assert.Equal(t, "https://github.com/acme/app.git", origin(t, filepath.Join(f.out, "app")))
	assert.Equal(t, "https://github.com/acme/app.wiki.git", origin(t, filepath.Join(f.out, "app.wiki")))
}

func TestCloneRepos_RewritesATokenBearingOriginBeforeAPull(t *testing.T) {
	f := httpsFixture(t)
	repo := f.httpsRepo(t, "app")
	repo.HasWiki = true
	wiki := gittest.NewBareRepo(t, f.upstream, "app.wiki", "master")
	opts := f.tokenOpts()
	opts.CloneWiki = true
	_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)
	require.NoError(t, err)

	// What an older ghx left behind.
	dest, wikiDest := filepath.Join(f.out, "app"), filepath.Join(f.out, "app.wiki")
	gittest.Git(t, dest, "remote", "set-url", "origin", "https://"+token+"@github.com/acme/app.git")
	gittest.Git(t, wikiDest, "remote", "set-url", "origin", "https://"+token+"@github.com/acme/app.wiki.git")
	gittest.AddCommit(t, filepath.Join(f.upstream, "app.git"), "main", "NEWS.md", "news\n")
	gittest.AddCommit(t, wiki, "master", "Home.md", "home\n")

	summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)

	require.NoError(t, err)
	assert.Equal(t, []cloner.Action{cloner.ActionPulled}, actionsByName(summary)["app"])
	assert.Equal(t, "https://github.com/acme/app.git", origin(t, dest))
	assert.Equal(t, "https://github.com/acme/app.wiki.git", origin(t, wikiDest))
	assert.FileExists(t, filepath.Join(dest, "NEWS.md"), "the pull ran on the cleaned URL")
	assert.FileExists(t, filepath.Join(wikiDest, "Home.md"))
	for path, cfg := range gitConfigs(t, f.out) {
		assert.NotContains(t, cfg, token, path)
	}
}

func TestCloneRepos_SSHClonesGetNoToken(t *testing.T) {
	f := newFixture(t)
	repo := f.repo(t, "app", "main")
	repo.CloneURL = githubPrefix + "app.git" // never contacted in SSH mode
	opts := f.tokenOpts()
	opts.UseSSH = true
	log := cloner.RecordGit(t, nil)

	summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, opts)

	require.NoError(t, err)
	assert.Equal(t, 1, summary.Count(cloner.ActionCloned))
	for _, c := range log.Calls() {
		if subcommand(c) == "clone" {
			assert.Nil(t, c.Env, "an SSH clone URL gets no header")
		}
	}
}

func TestCloneRepos_KeepsTheUsersOwnGitConfigEnvironment(t *testing.T) {
	f := httpsFixture(t)
	repo := f.httpsRepo(t, "app")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.abbrev")
	t.Setenv("GIT_CONFIG_VALUE_0", "12")
	log := cloner.RecordGit(t, nil)

	_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.tokenOpts())

	require.NoError(t, err)
	var clone cloner.GitCall
	for _, c := range log.Calls() {
		if subcommand(c) == "clone" {
			clone = c
		}
	}
	count, _ := envValue(clone.Env, "GIT_CONFIG_COUNT")
	assert.Equal(t, "2", count)
	userKey, _ := envValue(clone.Env, "GIT_CONFIG_KEY_0")
	assert.Equal(t, "core.abbrev", userKey, "the user's entry is still there")
	key, _ := envValue(clone.Env, "GIT_CONFIG_KEY_1")
	assert.Equal(t, headerKey, key, "the header comes after it")
}

func TestCloneRepos_MasksTheTokenInFailureDetails(t *testing.T) {
	t.Run("a clone whose stderr quotes the token and its header", func(t *testing.T) {
		f := httpsFixture(t)
		repo := f.httpsRepo(t, "app")
		cloner.RecordGit(t, func(cmd *exec.Cmd) error {
			if !slices.Contains(cmd.Args, "clone") {
				return cmd.Run()
			}
			header, _ := envValue(cmd.Env, "GIT_CONFIG_VALUE_0")
			_, _ = fmt.Fprintf(cmd.Stderr, "fatal: unable to access 'https://%s@github.com/acme/app.git/': %s\n", token, header)
			return errors.New("exit status 128")
		})

		summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.tokenOpts())

		require.NoError(t, err)
		require.Len(t, summary.Results, 1)
		detail := summary.Results[0].Detail
		assert.Equal(t, cloner.ActionFailed, summary.Results[0].Action)
		assert.NotContains(t, detail, token)
		assert.NotContains(t, detail, basic)
		assert.Contains(t, detail, "https://***@github.com/acme/app.git")
	})

	t.Run("a real clone of a URL that holds the token", func(t *testing.T) {
		f := httpsFixture(t)
		repo := ghapi.RepoInfo{Name: "ghost", CloneURL: githubPrefix + "ghost-" + token + ".git", DefaultBranch: "main"}

		summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.tokenOpts())

		require.NoError(t, err)
		require.Len(t, summary.Results, 1)
		assert.Equal(t, cloner.ActionFailed, summary.Results[0].Action)
		assert.Contains(t, summary.Results[0].Detail, "ghost-***", "git quoted the URL, and the token in it was masked")
		assert.NotContains(t, summary.Results[0].Detail, token)
	})
}

func TestCleanOrigins(t *testing.T) {
	f := httpsFixture(t)
	repo := f.httpsRepo(t, "app")
	parent := filepath.Join(f.out, "DELETED")
	_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, cloner.Options{
		OutputDir: parent, Concurrency: 1, ProgressOut: f.opts().ProgressOut,
	})
	require.NoError(t, err)
	src := filepath.Join(parent, "app")
	// Copies of one clone, each with a different origin.
	origins := map[string]string{
		"token": "https://" + token + "@github.com/acme/token.git",
		"ssh":   "git@github.com:acme/ssh.git",
		"plain": "https://github.com/acme/plain.git",
		"local": filepath.Join(f.upstream, "local.git"),
	}
	for name, url := range origins {
		dir := filepath.Join(parent, name)
		gittest.Git(t, "", "clone", "--quiet", src, dir)
		gittest.Git(t, dir, "remote", "set-url", "origin", url)
	}
	require.NoError(t, os.MkdirAll(filepath.Join(parent, "not-a-clone"), 0o755))
	// An "@" in its config (an email) but no origin at all.
	noOrigin := filepath.Join(parent, "no-origin")
	gittest.Git(t, "", "clone", "--quiet", src, noOrigin)
	gittest.Git(t, noOrigin, "remote", "remove", "origin")
	gittest.Git(t, noOrigin, "config", "user.email", "ada@example.com")

	failures, err := cloner.CleanOrigins(t.Context(), f.tokenOpts(), parent, filepath.Join(f.out, "missing"))

	require.NoError(t, err)
	assert.Empty(t, failures)
	assert.Equal(t, "https://github.com/acme/token.git", origin(t, filepath.Join(parent, "token")))
	for _, name := range []string{"ssh", "plain", "local"} {
		assert.Equal(t, origins[name], origin(t, filepath.Join(parent, name)), "%s is left alone", name)
	}
	assert.Empty(t, strings.TrimSpace(localConfig(t, noOrigin, "remote.origin.url")), "a clone without an origin gets none")
}

func TestCleanOrigins_StopsWhenGitIsMissing(t *testing.T) {
	f := httpsFixture(t)
	repo := f.httpsRepo(t, "app")
	_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.tokenOpts())
	require.NoError(t, err)
	gittest.Git(t, filepath.Join(f.out, "app"), "remote", "set-url", "origin", "https://"+token+"@github.com/acme/app.git")
	t.Setenv("PATH", t.TempDir())

	_, err = cloner.CleanOrigins(t.Context(), f.tokenOpts(), f.out)

	require.ErrorIs(t, err, cloner.ErrGitNotFound)
}

func TestCleanOrigins_ReportsAnOriginItCannotRewrite(t *testing.T) {
	f := httpsFixture(t)
	repo := f.httpsRepo(t, "app")
	_, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.tokenOpts())
	require.NoError(t, err)
	dir := filepath.Join(f.out, "app")
	gittest.Git(t, dir, "remote", "set-url", "origin", "https://"+token+"@github.com/acme/app.git")
	cloner.RecordGit(t, func(cmd *exec.Cmd) error {
		if slices.Contains(cmd.Args, "set-url") {
			_, _ = fmt.Fprintf(cmd.Stderr, "error: could not lock config file for %s\n", token)
			return errors.New("exit status 255")
		}
		return cmd.Run()
	})

	failures, err := cloner.CleanOrigins(t.Context(), f.tokenOpts(), f.out)

	require.NoError(t, err)
	require.Len(t, failures, 1)
	assert.Equal(t, dir, failures[0].Name)
	assert.Equal(t, cloner.ActionFailed, failures[0].Action)
	assert.Contains(t, failures[0].Detail, "removing the credential from the origin URL")
	assert.NotContains(t, failures[0].Detail, token)

	// processRepo reports the same failure instead of pulling.
	summary, err := cloner.CloneRepos(t.Context(), []ghapi.RepoInfo{repo}, f.tokenOpts())
	require.NoError(t, err)
	require.Len(t, summary.Results, 1)
	assert.Equal(t, cloner.ActionFailed, summary.Results[0].Action)
	assert.NotContains(t, summary.Results[0].Detail, token)
}
