# Repo layout

Where ghx keeps each repository under the output directory, and the rules that put
a clone there. Users: the Archived repos bullet in [`README.md`](../README.md). Test seams: [`testing.md`](./testing.md).

## Where a repo lives

`<out>` is the output directory (`PATH`). `x` is the repository's name,
`<archived-dir>` is `--archived-dir` (`ARCHIVED` by default) and `<deleted-dir>` is
`--deleted-dir` (`DELETED`). Both must be relative paths inside `<out>`.

| Repo state | Clone | Wiki (`--clone-wiki`) |
|---|---|---|
| Active | `<out>/x` | `<out>/x.wiki` |
| Archived | `<out>/<archived-dir>/x` | `<out>/<archived-dir>/x.wiki` |
| Removed (in the last manifest, gone from the org) | `<out>/<deleted-dir>/x`, or `x.<stamp>` when that name is taken | beside its repo: `x.wiki`, or `x.<stamp>.wiki` |

Where a clone sits is decided by the repo's archived state in the listing the run
just fetched, not by the previous `.ghx.json`: the manifest names the repos that
went missing from the org (a removed repo isn't in the listing), and nothing
else. The `.ghx.json` schema does not carry a location.

## Reconcile

Before any clone or pull, every run puts each synced clone where its state says it
belongs. The repos it looks at are the filtered list (`--include`, `--exclude`,
`--type`), so a repo the run doesn't sync is never moved somewhere no sync will
reach it. For each such repo `x`:

- `want` is `<out>/<archived-dir>/x` if the repo is archived, else `<out>/x`;
  `other` is the opposite place.
- `other` has no `.git` directory: nothing to do.
- `other`'s origin doesn't name `x` (see below): `other` is left alone and
  reported `skipped (origin mismatch)` with the URL found.
- Something already exists at `want`: nothing is overwritten. The run reports a
  `conflict` naming both paths, leaves both copies, and syncs `want` as usual.
  `MoveRepo` replaces an existing destination, so the reconcile never calls it
  onto one.
- The repo is archived and `--delete` is set: `other` is deleted, and then
  `other.wiki` the same way. An archived repo isn't synced under `--delete`.
- Otherwise `other` is moved to `want`'s parent (`moved (archived)` or
  `moved (unarchived)`), and then `other.wiki` is moved the same way when it
  exists and `want.wiki` doesn't. The wiki has the same checks as its repo.
- `--dry-run` reports each planned action with the detail `dry-run` and touches
  nothing.

Because the disk decides, the rules cover a repo archived since the last run, one
unarchived since, a mirror with no `.ghx.json`, and a move an earlier run failed to
make: the next run finds the clone in the wrong place again and retries. A path
that can't be checked counts as taken, so the repo shows as a conflict and stays
put until the path is cleared. Where the check finds nothing but the move still
fails (an unwritable parent, or a dangling symlink where the archive directory
belongs), the repo shows as `failed` and the next run retries.

A removed repo goes through the same origin check, then moves to `<deleted-dir>`
or, under `--delete`, is deleted; its wiki then goes the same way, through the
same check. A removed directory with no `.git` is reported
`skipped (origin mismatch)`, not moved.

Nothing already in `<deleted-dir>` is replaced. When `<deleted-dir>/x` exists
(the repo was removed before, came back and was removed again), the clone moves
to `<deleted-dir>/x.<stamp>`, where `<stamp>` is the run's UTC start time as
`20261014T013134Z` (no colons, so the name is valid on Windows), with `-2`, `-3`
and so on appended if that is taken too. The result is `moved (deleted)` with a
detail naming both paths. The wiki takes its repo's final name plus `.wiki`, so
the pair stays together.

## Origin check

The directory's name matches the repo by construction, so it proves nothing. The
origin URL does, so ghx moves or deletes a clone only when its origin names the
repo.

- `cloner.ReadOrigin` reads the raw `remote.origin.url` with
  `git --git-dir=<dir>/.git config --local --get remote.origin.url`. Not
  `git remote get-url`, which expands `url.<base>.insteadOf` and so reports where
  git fetches from, not what the clone says it is (and would break under the test
  harness's URL rewrite). Pointing git at `<dir>/.git` keeps a directory with a
  broken `.git` from borrowing the origin of a repo around it.
- `cloner.OriginRepoName` takes the last non-empty path segment, splitting on `/`,
  `\` and `:`, trims a trailing `.git`, and the check compares it with the repo's
  name ignoring case. `https://github.com/org/x.git`,
  `https://user:tok@github.com/org/x`, `git@github.com:org/x.git`,
  `ssh://git@github.com/org/x/` and a local path all reduce to `x`.
- Host and owner are ignored on purpose: an org rename, a GitHub Enterprise host
  or an SSH host alias doesn't cause a false mismatch.
- A missing or unreadable origin counts as a mismatch. A mismatch only ever leaves
  the directory alone and reports, so the check fails safe.
