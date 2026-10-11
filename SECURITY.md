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
  Windows) or `<PATH>/.ghx.toml`. ghx never writes a token to a config file, and
  it sends the token only to GitHub: to the API and, over HTTPS, to `git`.
- **Display.** The settings table printed at the start of a run shows the token
  obfuscated, never in full.
- **HTTPS clones embed the token.** With the default `https` protocol, ghx
  passes `git` a URL of the form `https://<token>@github.com/...`. The token is
  therefore visible in the process list while `git` runs, and `git` records the
  URL, token included, as the clone's `origin` remote in `.git/config`. Anyone
  who can read your clones can read the token. Treat the output directory like a
  credential store, and revoke the token if it is shared.
- **SSH avoids that.** With `--protocol ssh` (or `--ssh`) the clone URL carries
  no token and `git` authenticates with your SSH key. ghx still uses the token
  for the API calls that list repositories.
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
