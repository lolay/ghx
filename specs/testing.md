# Testing

How ghx's tests are laid out, the seams they use, and which packages are exempt from the coverage target. It follows Gary's `testing.md` and `go/testing.md` standards; this file records only the ghx-specific decisions.

## Running

| Command | What it does |
|---|---|
| `make test` | `go test -race -cover ./...`, one coverage line per package |
| `make ci` | build, lint, then `make test` |
| `go test -race -coverprofile=coverage.out ./... && go tool cover -func=coverage.out` | coverage per function (`coverage.*` is gitignored) |

## Layout and style

- **Colocated** `_test.go` files beside the code, as black-box tests (`package config_test`) that use only the exported API. A test moves into the package itself only when it can't reach a branch any other way; none do today.
- **testify**: `require` for preconditions and anything later lines depend on, `assert` for the checks that follow, so one failure doesn't hide the rest.
- **Table-driven**: a `tests := []struct{ name string; ... }` slice and `t.Run(tt.name, ...)`, with case names that read as sentences (`"local beats home"`).
- **Fixtures** under the package's `testdata/`. `.gitattributes` keeps `testdata/` LF on every checkout, so byte comparisons hold on Windows.
- **Golden manifest**: `internal/manifest/testdata/golden/.ghx.json` was written by the Python implementation's own code (`json.dump(manifest, f, indent=2)` plus a newline). `manifest.Write` must reproduce it byte for byte except `exported_at`, and `manifest.Read` must load it. Never regenerate it from Go output: that would turn the test into a check of Go against itself.

## Seams

Apart from the `ghapi` client seam below, no production code changed to make these packages testable; the tests use what the standard library already reads.

| Dependency | Seam | Why this one |
|---|---|---|
| Home directory in `config.Load` | `os.UserHomeDir` reads `HOME` (Unix, macOS) or `USERPROFILE` (Windows). Tests set both with `t.Setenv` to a `t.TempDir()` (`isolateHome`) | No signature change for `cli`; plan B's run-flow tests drive the cobra command and need the same environment seam |
| Safety net for the home directory | `config`'s `TestMain` points `HOME` and `USERPROFILE` at an empty temp directory before any test runs | A test that forgets `isolateHome` still can't read the developer's `~/.ghx.toml` |
| `GITHUB_TOKEN` in `config.ResolveToken` | `t.Setenv("GITHUB_TOKEN", ...)` per case; `ResolveToken` treats empty as unset. `config`'s `TestMain` unsets it | A token in the developer's shell or a CI secret can't change a result |
| Terminal output in `ui` | `ui.Out` is a package `io.Writer`; tests swap in a `bytes.Buffer` and restore it with `t.Cleanup` | Already there; no wrapper needed |
| ANSI colors in `ui` | `ui`'s `TestMain` calls go-pretty's `text.DisableColors()` | Output compares as plain text on any terminal or runner |
| GitHub API in `ghapi` | `newClient` builds the go-github client for both `ValidateToken` and `ListOrgRepos`, and takes its base URL from the unexported `baseURL`, which is nil (the public API) outside tests. `export_test.go` sets it with `SetBaseURL(t, url)` and restores it with `t.Cleanup`. `internal/ghapi/ghapitest` is the fake server: `GET /user` and `GET /orgs/{org}/repos` with Link-header pagination, a 401 for any other token, and a record of each request | Users see no change. The fake binds to loopback, and `newAPI` in the tests asserts that, so no test can reach `api.github.com` |
| GitHub API in `cli` | `ghapitest.Server.RedirectGitHub(t)` swaps `http.DefaultTransport` for one that sends `api.github.com` to the fake and fails every other host. go-github uses the default transport when `ghapi` gives it no client, so `cli` needs no seam of its own, and the `baseURL` variable stays private to `ghapi` | The run flow builds its clients inside `ghapi`; a swap of one process-wide variable, restored by `t.Cleanup`, reaches them without a production hook. It rules out `t.Parallel()` |
| Run flow in `cli` | `export_test.go` aliases `newRootCmd`; the tests pass args with `SetArgs`, point `ui.Out` at a mutex-guarded buffer (the progress renderer writes from its own goroutine), and use `t.Setenv` for `HOME`, `USERPROFILE` and `GITHUB_TOKEN`. `cli`'s `TestMain` isolates the home directory, unsets the token and disables colors, as `config`'s does | The tests drive the real cobra command against the fake API and bare repos from `gittest`, and assert on the directory tree and `.ghx.json`, never on the progress bar's frames |
| `git` in `cloner` | `internal/gittest` makes a bare repo with one commit on a named default branch under `t.TempDir()`; its path is the `RepoInfo.CloneURL`, and `resolveURL` passes anything that isn't `https://` through unchanged, so `CloneRepos` really clones and pulls. `gittest.Isolate` sets `GIT_CONFIG_GLOBAL` to the null device and `GIT_CONFIG_NOSYSTEM`, and every helper command passes `user.name`, `user.email` and `init.defaultBranch` with `-c` | A runner's global git config (a signing key, `pull.rebase`, another default branch) can't change a result |
| Unexported URL helpers in `cloner` | `export_test.go` aliases `resolveURL`, `resolveWikiURL` and `authenticatedHTTPS` for the black-box tests | Keeps the tests in `package cloner_test` without widening the package API |
| Clock in `manifest.Write` | None. Tests check `exported_at` parses as RFC 3339 in UTC within the test's time window, and swap the golden value in before comparing bytes | One field; a clock variable would be production state for no other caller |

`t.Setenv` and the shared `ui.Out` rule out `t.Parallel()` in those tests. They are fast enough to run serially. Pure tests, such as `ObfuscateToken`, may run in parallel.

## Hermeticity

- **No network.** `ghapi` tests talk to the `ghapitest` fake on loopback through the `baseURL` seam. A test that forgets the seam would reach `api.github.com`, so every test goes through the `newAPI` helper, which sets it.
- **No real home directory.** Only `config.Load` reads it, and the seams above redirect it.
- **`git` on `PATH`.** No test in `config`, `ghapi`, `manifest` or `ui` runs `git`. The `cloner` tests do, against local bare repos made in `t.TempDir()` and cloned by path; every CI runner has `git`.

## Coverage

The target is 80% of statements per package. It is a review signal, not a CI gate: no build fails on a percentage.

| Package | Status | Reason |
|---|---|---|
| `internal/config` | At target | The uncovered lines are `os.Stat` and `os.Rename` failures other than "not found" (permission errors) in `MigrateLegacyFile`, and `Load` passing them on, which can't be provoked portably |
| `internal/manifest` | At target | The uncovered lines are a legacy-migration stat error and a `json.MarshalIndent` error that can't happen for this struct |
| `internal/ui` | At target | |
| `internal/ghapi` | At target (100%) | |
| `internal/cloner` | At target (about 96%) | The uncovered lines are `os.Stat` failures other than "not found" in `MoveRepo` and `DeleteRepo`, which can't be provoked portably, and `gitResult.Message`'s fallback to the process error when git printed nothing |
| `internal/cli` | At target (about 92%; the target here is 70%) | `Execute` is at 0%: it wires `signal.NotifyContext` and `os.Stderr`, and the tests drive `newRootCmd` instead. Also uncovered in `run`: the error paths after a good token (a failed `ListOrgRepos` after `ValidateToken`, a failed `manifest.Read` or `manifest.Write`, a failed `CloneRepos`) and the `Failed` results `handleRemovedAndArchived` appends when a move or delete errors, none of which can be provoked portably against a real directory tree |
| `internal/ghapi/ghapitest` | **Exempt** | Test helper: the fake GitHub API the `ghapi` and `cli` tests use. Reports 0% for the same reason as `gittest` |
| `internal/gittest` | **Exempt** | Test helper: builds the bare repos the `cloner` and `cli` tests use. It is exercised by those tests but reports 0% because coverage counts only a package's own tests |
| `cmd/ghx` | **Exempt** | Entry point: `main` only calls `cli.Execute` and exits with its code |
