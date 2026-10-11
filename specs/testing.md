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

No production code changed to make these packages testable; the tests use what the standard library already reads.

| Dependency | Seam | Why this one |
|---|---|---|
| Home directory in `config.Load` | `os.UserHomeDir` reads `HOME` (Unix, macOS) or `USERPROFILE` (Windows). Tests set both with `t.Setenv` to a `t.TempDir()` (`isolateHome`) | No signature change for `cli`; plan B's run-flow tests drive the cobra command and need the same environment seam |
| Safety net for the home directory | `config`'s `TestMain` points `HOME` and `USERPROFILE` at an empty temp directory before any test runs | A test that forgets `isolateHome` still can't read the developer's `~/.ghx.toml` |
| `GITHUB_TOKEN` in `config.ResolveToken` | `t.Setenv("GITHUB_TOKEN", ...)` per case; `ResolveToken` treats empty as unset. `config`'s `TestMain` unsets it | A token in the developer's shell or a CI secret can't change a result |
| Terminal output in `ui` | `ui.Out` is a package `io.Writer`; tests swap in a `bytes.Buffer` and restore it with `t.Cleanup` | Already there; no wrapper needed |
| ANSI colors in `ui` | `ui`'s `TestMain` calls go-pretty's `text.DisableColors()` | Output compares as plain text on any terminal or runner |
| Clock in `manifest.Write` | None. Tests check `exported_at` parses as RFC 3339 in UTC within the test's time window, and swap the golden value in before comparing bytes | One field; a clock variable would be production state for no other caller |

`t.Setenv` and the shared `ui.Out` rule out `t.Parallel()` in those tests. They are fast enough to run serially. Pure tests, such as `ObfuscateToken`, may run in parallel.

## Hermeticity

- **No network.** `ghapi` tests cover only `FilterRepos`. The go-github client is built inside each function, so there is no seam for a test server until plan B adds one.
- **No real home directory.** Only `config.Load` reads it, and the seams above redirect it.
- **`git` on `PATH`.** No test in `config`, `ghapi`, `manifest` or `ui` runs `git`. The `cloner` tests that plan B adds will, against local bare repos made in `t.TempDir()`; every CI runner has `git`.

## Coverage

The target is 80% of statements per package. It is a review signal, not a CI gate: no build fails on a percentage.

| Package | Status | Reason |
|---|---|---|
| `internal/config` | At target | The uncovered lines are `os.Stat` and `os.Rename` failures other than "not found" (permission errors) in `MigrateLegacyFile`, and `Load` passing them on, which can't be provoked portably |
| `internal/manifest` | At target | The uncovered lines are a legacy-migration stat error and a `json.MarshalIndent` error that can't happen for this struct |
| `internal/ui` | At target | |
| `internal/ghapi` | **Exempt until plan B** (about 43%) | `filter.go` is fully covered. `ValidateToken`, `ListOrgRepos`, `toRepoInfo` and `formatAPIError` need the `httptest` seam that plan B adds in its `m1.s2` |
| `internal/cloner` | **Exempt until plan B** (0%) | Shells out to `git`; plan B's `m1.s1` tests it against local bare repos |
| `internal/cli` | **Exempt until plan B** (0%) | The run flow needs both seams above; plan B's `m1.s3` drives it end to end |
| `cmd/ghx` | **Exempt** | Entry point: `main` only calls `cli.Execute` and exits with its code |
