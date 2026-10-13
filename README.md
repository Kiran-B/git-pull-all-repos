# git-pull-all-repos

Clones every repo of a GitHub org (or user) that you can read, and on re-runs
safely brings the existing clones up to date. It never deletes a repo folder, never force-deletes a
branch, never discards uncommitted work and never switches your branch in an
existing clone.
It replaces the "browse the org and clone one by one, then loop `git pull`"
routine with a single command.

## Install

This repo is its own Homebrew tap:

```bash
brew tap Kiran-B/git-pull-all-repos https://github.com/Kiran-B/git-pull-all-repos
brew install Kiran-B/git-pull-all-repos/git-pull-all-repos
```

The Homebrew cask is macOS only. On Linux, download the tarball from the
[releases page](https://github.com/Kiran-B/git-pull-all-repos/releases).

Because the binary is called `git-pull-all-repos`, it also works as a git
subcommand: `git pull-all-repos <org>`.

## Requirements

- [`gh`](https://cli.github.com), authenticated (`gh auth login`). It lists repos and supplies git credentials.
- `git`

## Usage

```bash
git-pull-all-repos <org> [destination] [flags]
```

`<org>` can be `acme`, `https://github.com/acme` or `https://github.com/orgs/acme`
(a trailing path such as `/repositories` is fine). A personal user account works too.

```bash
# Clone/update into ./acme/<repo>
git-pull-all-repos acme

# Clone/update into ~/code/acme-repos/<repo> (no org subfolder)
git-pull-all-repos https://github.com/orgs/acme ~/code/acme-repos

# Only the 20 most-starred Go repos, no forks or archived repos
git-pull-all-repos acme --language go --no-forks --no-archived --sort stars --limit 20

# See what would happen without cloning or fetching
git-pull-all-repos acme --dry-run
```

Repos are processed one at a time on purpose, so a run doesn't look like a bulk
scraper. Use `--jobs N` to speed things up.

### Flags

| Flag | Meaning | Default |
|------|---------|---------|
| `--no-forks` | Exclude forks | forks included |
| `--no-archived` | Exclude archived repos | archived included |
| `--language <lang>` | Only repos whose primary language matches | no filter |
| `--sort pushed\|stars` | Most recently pushed first, or most stars first | `pushed` |
| `--limit N` | Keep the first N repos after filtering and sorting | all |
| `--jobs N` | Repos processed concurrently | `1` |
| `--no-submodules` | Don't clone/update submodules | submodules on |
| `--dry-run` | Show what would happen; no clone/fetch/pull | off |
| `--version`, `--help` | | |

Mirrors are always excluded. Filters decide what gets cloned; they do not stop
existing clones of the same org in the destination folder from being updated.
Clones from other orgs in the same folder are ignored.

### Exit codes

- `0`: success, or warnings only
- `1`: at least one repo failed
- `2`: setup or usage error (missing `gh`/`git`, not authenticated, bad arguments)

## What happens to each repo

- **Not on disk**: cloned (with submodules unless `--no-submodules`).
- **On disk**: `git fetch --prune --tags --force origin` first.
  - Local branches whose GitHub branch is gone are removed with `git branch -d` only. If git refuses (e.g. squash-merged), the branch is kept and you get a warning. The checked-out branch is never deleted.
  - **On the default branch, clean**: rebased onto `origin/<default>`. On conflict the rebase is aborted so the repo is exactly as before, and you get a warning.
  - **On the default branch, uncommitted changes**: fetch only, with a warning.
  - **On another branch**: the branch is not switched. The local default branch is fast-forwarded without checking it out (warning if it can't be). The current branch is rebased onto its upstream if it is clean, otherwise left alone with a warning.
  - **Rebase or merge in progress**: treated like uncommitted changes.
  - If the current branch's upstream was deleted on GitHub you get a warning (it is never deleted while checked out); a local-only branch or detached HEAD is left as-is with no warning.
- Submodules are updated after a successful rebase.
- A folder that isn't a git repo, or whose `origin` is a different repo, is skipped with a warning. Empty repos are skipped.

"Uncommitted changes" means changes to tracked files; untracked files alone don't block an update.

Each repo prints one line as it finishes, followed by a summary listing every
warning and failure.

## Releasing

```bash
git tag vX.Y.Z && git push --tags
```

The `release` workflow runs the tests, builds darwin/linux binaries for amd64
and arm64 with GoReleaser, publishes the GitHub release and commits the updated
cask to `Casks/` on `main` (so `git pull` after a release). No extra secrets are
needed. The binary isn't signed or notarized, so the cask removes the macOS
quarantine flag after install to keep Gatekeeper from blocking it.
