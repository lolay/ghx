# Repo layout

Where ghx keeps each repository under the output directory, and the rules that put
a clone there. Users: the Archived repos bullet in [`README.md`](../README.md). Test seams: [`testing.md`](./testing.md).

## Where a repo lives

`<out>` is the output directory (`PATH`). `x` is the repository's name,
`<archived-dir>` is `--archived-dir` (`ARCHIVED` by default) and `<deleted-dir>` is
`--deleted-dir` (`DELETED`). Both must be relative paths inside `<out>`, and
neither may be inside the other, compared without case (`checkApart`): a repo
named like the inner directory would collide with it.

They may be the same directory (`sameDir`, also without case). Then every
removal takes a stamped name, `x.<stamp>`, even when `x` is free, and a removed
archived repo's clone is renamed in place from `x` to `x.<stamp>`. So in the
shared directory a plain `x` is only ever an archived clone and `x.<stamp>`
only ever a removal's backup: the archive pass, which looks at plain names,
never moves a backup out as an unarchived clone, and a removal, which looks at
`<archived-dir>/x`, never retires or deletes one.

A ghx before this rule left removals in a shared directory under their plain
name. So before anything else, a run with a previous manifest (even one with no
repos) renames each unstamped clone there that the manifest doesn't list as
archived to `x.<stamp>` (`x.<stamp>.wiki` for a wiki, kept as is when its repo
was archived), with the same origin check and the result `moved (deleted)`
(`stampLegacy`). It is only renamed beside itself, never deleted. A name shaped
like `x.<stamp>` counts as a backup unless a repo of that whole name is listed
or in the manifest and the clone's origin names it, and a name shaped like
`x.wiki` is `x`'s wiki unless a repo of that whole name is known, in which case
it follows its own archived state. If any rename fails, or the directory can't
be listed (anything but not found), the run stops there with an error: nothing is reconciled or synced and the manifest
isn't rewritten, since every later step would read the backup as a clone, and
the next run tries again. Without a previous manifest an archived clone can't
be told from a backup, so nothing is renamed and a plain `x` counts as an
archived clone. The scan reads `<archived-dir>`, where the later steps look
for plain names: since `sameDir` ignores case, `OLD` and `old` count as one
directory even on a case-sensitive file system, where they are two.

Known limit: a repo whose own name has the stamp's shape (`x.<stamp>`) isn't
fully protected. Its wiki's unstamped backup is skipped as if it were stamped,
and a newly listed archived repo of that exact name would be synced into a
removal's backup of `x` that already has the name. Telling those apart would
take the backup paths into the sync itself; a repo named like a UTC timestamp
isn't worth that.

A dry run shares one plan across these steps (`plan`): the names handed out,
and the paths a planned move or delete has emptied, so a later step sees the
disk as the earlier ones would have left it. One cost: if a run moved a
clone into the shared directory as archived but failed before writing its
manifest, the next run stamps that clone as a backup and clones the repo
afresh beside it; nothing is lost.

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
  onto one. Under `--delete` the conflict deletes neither copy, and the
  archived one isn't synced, so the detail says `deleting neither`.
- The repo is archived and `--delete` is set: `other` is deleted, and then
  `other.wiki` the same way. An archived repo isn't synced under `--delete`.
- Otherwise `other` is moved to `want`'s parent (`moved (archived)` or
  `moved (unarchived)`).
- The wiki, `other.wiki`, then goes through the same rules, with the same
  checks, whenever the repo is no longer at `other`: moved or deleted with it
  this run, or moved by an earlier run that left the wiki behind (a ghx older
  than v0.3.0, or `--clone-wiki` turned on after the move). A repo still at
  `other` after a conflict or an origin mismatch keeps its wiki beside it. A
  wiki is never moved when the unfiltered listing has a repo named `x.wiki`
  (ignoring case): that repo's clone has the same directory name and an origin
  the check can't tell from the wiki's.
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
same check. Both places a clone can be are looked at, `<out>/x` and
`<out>/<archived-dir>/x` (a repo archived and then removed), and each clone found
is retired; when both exist, the second takes a stamped name as below. A removed
directory with no `.git` is reported `skipped (origin mismatch)`, not moved. A
wiki whose repo is no longer in that place (it went on an earlier run and the
wiki failed to follow) is retired on its own, under its own name.

The manifest is the only record of a removal, since a removed repo isn't in the
listing. So when a removed repo's clone or wiki `failed` to move or delete, the
new manifest keeps that repo's previous entry, unchanged, after the listing's
repos, and the next run finds it removed again and retries until it succeeds.
An origin mismatch isn't kept: that directory isn't a clone of the repo, so it is
reported on the run that finds the repo gone and left alone after that. If the
org creates a repo of that name again, the listing's entry replaces the kept one.
A place that can't be checked (a `stat` error other than not found, such as a
directory the user can't traverse, or a `<deleted-dir>` that is a file) counts as
`failed`, so the removal is kept until the path can be checked. A repo counts as
removed only when no listed repo has its name ignoring case: GitHub names are
case-insensitive, so a repo renamed only in case is still the org's, and on macOS
or Windows its old directory is its live clone. The wiki rule above holds here too: a listed repo
named `x.wiki` is never retired as `x`'s wiki.

`--delete` applies to the repos the run finds removed or archived, nothing
earlier: the run that retires a removed repo writes a manifest without it, so no
later run sees it again (a failed retire is kept and retried, as above), and no
earlier removal's clone in `<deleted-dir>` is ever deleted. With a shared
directory, a plain `x` there is an archived clone, not a removal, so `--delete`
deletes it when `x` is removed, as it would in a separate `<archived-dir>`. A clone
already in `<archived-dir>` when `--delete` is first used stays there, unsynced.

Nothing already in `<deleted-dir>` is replaced. When `<deleted-dir>/x` exists
(a repo was removed, a new one was created with the same name, synced, and
removed in turn), the clone moves
to `<deleted-dir>/x.<stamp>`, where `<stamp>` is the run's UTC start time as
`20261014T013134Z` (no colons, so the name is valid on Windows), with `-2`, `-3`
and so on appended if that is taken too. The result is `moved (deleted)` with a
detail naming both paths. The wiki takes its repo's final name plus `.wiki`, so
the pair stays together. With a shared archived and deleted directory, the stamp
is always added (see above); a wiki retired on its own there is
`x.<stamp>.wiki`.

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
