package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lolay/ghx/internal/cli"
	"github.com/lolay/ghx/internal/config"
	"github.com/lolay/ghx/internal/ghapi/ghapitest"
	"github.com/lolay/ghx/internal/gittest"
	"github.com/lolay/ghx/internal/manifest"
	"github.com/lolay/ghx/internal/ui"
)

const (
	testToken = "ghp_cli_test_token"
	testOrg   = "acme"
)

// lockedBuffer is a bytes.Buffer safe to write from the progress renderer's
// goroutine while the test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// world is one test's GitHub (a fake API plus bare upstream repos) and the
// directory ghx mirrors into.
type world struct {
	t        *testing.T
	api      *ghapitest.Server
	upstream string
	// out is the mirror directory handed to ghx.
	out   string
	repos []ghapitest.Repo
}

// newWorld isolates git, the home directory and the token, starts the fake
// GitHub and routes api.github.com to it.
func newWorld(t *testing.T) *world {
	t.Helper()
	gittest.Isolate(t)
	home := t.TempDir()
	for _, key := range homeEnvKeys {
		t.Setenv(key, home)
	}
	t.Setenv("GITHUB_TOKEN", testToken)

	api := ghapitest.NewServer(t, testToken, "octocat")
	api.RedirectGitHub(t)
	return &world{
		t:        t,
		api:      api,
		upstream: t.TempDir(),
		out:      filepath.Join(t.TempDir(), "mirror"),
	}
}

// addRepo creates a bare upstream repo with one commit on main and lists it in
// the org. The repo's clone and ssh URLs are the bare repo's path.
func (w *world) addRepo(name string, edit ...func(*ghapitest.Repo)) {
	w.t.Helper()
	bare := gittest.NewBareRepo(w.t, w.upstream, name, "main")
	r := ghapitest.Repo{Name: name, CloneURL: bare, SSHURL: bare, DefaultBranch: "main"}
	for _, e := range edit {
		e(&r)
	}
	w.repos = append(w.repos, r)
	w.publish()
}

// bare is the path of the upstream bare repo for name.
func (w *world) bare(name string) string {
	return filepath.Join(w.upstream, name+".git")
}

// removeRepo drops name from the org listing, as if it were deleted upstream.
func (w *world) removeRepo(name string) {
	w.repos = slices.DeleteFunc(w.repos, func(r ghapitest.Repo) bool { return r.Name == name })
	w.publish()
}

// archiveRepo marks name as archived in the org listing.
func (w *world) archiveRepo(name string) {
	w.t.Helper()
	i := slices.IndexFunc(w.repos, func(r ghapitest.Repo) bool { return r.Name == name })
	require.GreaterOrEqual(w.t, i, 0, "no repo %q", name)
	w.repos[i].Archived = true
	w.publish()
}

// pushCommit adds a commit to the upstream repo's main branch.
func (w *world) pushCommit(name, file string) {
	w.t.Helper()
	gittest.AddCommit(w.t, w.bare(name), "main", file, "content\n")
}

func (w *world) publish() { w.api.SetRepos(testOrg, w.repos) }

// run executes the root command with <org> <out> and extra args, the way
// `ghx acme <dir> ...` would, and returns what it printed.
func (w *world) run(extra ...string) (string, error) {
	w.t.Helper()
	return runCLI(w.t, append([]string{testOrg, w.out}, extra...)...)
}

// mustRun is run for a command that must succeed.
func (w *world) mustRun(extra ...string) string {
	w.t.Helper()
	out, err := w.run(extra...)
	require.NoError(w.t, err, "output:\n%s", out)
	return out
}

// runCLI drives the cobra command in-process and returns everything written to
// ui.Out, which is where the settings table, progress bar and summary go.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out := &lockedBuffer{}
	orig := ui.Out
	ui.Out = out
	t.Cleanup(func() { ui.Out = orig })

	cmd := cli.NewRootCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(t.Context())
	return out.String(), err
}

// repoDirs lists, sorted and slash-separated, every directory under root that
// is a git clone. It does not descend into a clone.
func repoDirs(t *testing.T, root string) []string {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || path == root {
			return nil
		}
		if info, statErr := os.Stat(filepath.Join(path, ".git")); statErr == nil && info.IsDir() {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			dirs = append(dirs, filepath.ToSlash(rel))
			return filepath.SkipDir
		}
		return nil
	})
	require.NoError(t, err)
	slices.Sort(dirs)
	return dirs
}

// snapshot maps every path under root, git internals included, to a hash of
// its content (empty for a directory), so a test can prove nothing changed.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			out[filepath.ToSlash(rel)] = ""
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		sum := sha256.Sum256(data)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	require.NoError(t, err)
	return out
}

// readManifest loads the mirror's .ghx.json and fails when it is missing.
func readManifest(t *testing.T, dir string) manifest.Manifest {
	t.Helper()
	m, _, err := manifest.Read(dir)
	require.NoError(t, err)
	require.NotNil(t, m, "no %s in %s", config.ManifestFilename, dir)
	return *m
}

// manifestNames is the repo names in a manifest, sorted.
func manifestNames(m manifest.Manifest) []string {
	var names []string
	for _, r := range m.Repos {
		names = append(names, r.Name)
	}
	slices.Sort(names)
	return names
}
