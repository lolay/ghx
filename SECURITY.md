# Security Policy

## Supported versions

ghx has no tagged release yet, so security fixes land on `main`. Once releases
ship, the latest release line receives security fixes and older lines do not.

## Reporting a vulnerability

Please **do not** open a public issue for security problems. Use the private
channel below:

1. **GitHub private vulnerability report.** Open a draft advisory at
   <https://github.com/lolay/ghx/security/advisories/new>. The report stays
   visible only to maintainers until a fix and a disclosure plan are ready.

In your report, please include:

- A description of the issue and the impact you believe it has.
- Steps to reproduce, or the command line and config that trigger it. Replace
  any real token with a placeholder.
- The commit SHA or release you tested against.
- Your operating system and Go version.
- Any suggested mitigation, if you have one.

## What to expect

- We aim to acknowledge receipt within **3 business days**.
- We will keep you informed as we investigate, and will agree the disclosure
  timing with you before publishing a fix or advisory.
- Once a fix lands, we will credit you in the advisory unless you ask us not to.

## How ghx handles your token

ghx needs a GitHub personal access token to list and clone an organization's
repositories. What it does with that token:

- **Sources.** The token comes from `--token`, the `GITHUB_TOKEN` environment
  variable, or a `token` key in `~/.ghx.toml` (`%USERPROFILE%\.ghx.toml` on
  Windows) or `<PATH>/.ghx.toml`. ghx sends the token only to GitHub: to the
  API and, over HTTPS, to `git`.
- **Display.** The settings table printed at the start of a run shows the token
  obfuscated, never in full. Failure messages from `git` and from the GitHub
  API have the token, and the header value that encodes it, masked as `***`.
- **Never written to disk.** ghx writes no credential to any file. It gives
  `git` the token through `git`'s own environment (`GIT_CONFIG_COUNT` with an
  `http.<url>.extraheader` scoped to the clone URL's host), for the length of
  each clone or pull command only. Clone URLs and every clone's `.git/config`
  hold no token, and it is not in the process list, which other users can
  read; a process's environment is readable only by its own user. Because the
  header is sent with the first request, your credential helper (macOS
  Keychain, Git Credential Manager) is never asked for or given the token.
  This needs `git` 2.31 or later.
- **Older clones are cleaned.** Clones made by versions of ghx before this
  hardening stored the token in their `origin` URL. A sync rewrites any HTTPS
  origin with a user name or token in it to the same URL without one: in each
  repository it pulls, and in every clone directly under the output directory,
  `DELETED/` and `ARCHIVED/`. SSH and local-path origins are left alone, and
  `--dry-run` changes nothing. If you used an older version, rotate the token:
  copies of those clones (backups, other machines) may still hold it.
- **SSH needs no token in `git`.** With `--protocol ssh` (or `--ssh`) `git`
  clones with your SSH key, and ghx still uses the token for the API calls that
  list repositories. A clone first made over HTTPS keeps an HTTPS origin, and
  its pulls still get the header, scoped to GitHub's HTTPS host.
- **Reduce the blast radius.** Use a fine-grained token limited to the target
  organization with read-only "Metadata" and "Contents" access, and set an
  expiry.
- **Keep it out of history.** Prefer `GITHUB_TOKEN` or the config file over
  `--token`, which lands in shell history and the process list. Set the config
  file to be readable only by you (`chmod 600 ~/.ghx.toml`).

## Scope

In scope:

- The `ghx` binary: config loading, token handling, GitHub API calls, the
  clone and sync logic, and the `.ghx.json` manifest.
- Anything that lets a crafted config file, manifest, repository name or API
  response write outside the output directory, run unintended commands, or expose
  the token.

Out of scope (please report upstream):

- Vulnerabilities in third-party dependencies, unless ghx exposes them through
  misuse.
- Issues that require an attacker to already have full local-machine access.
- The behavior of tools ghx merely invokes, such as `git` or the GitHub API.
