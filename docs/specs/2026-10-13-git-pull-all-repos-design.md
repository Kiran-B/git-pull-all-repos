# git-pull-all-repos — Design

Date: 2026-10-13

## Why

Today the user browses a GitHub org in the browser and clones repos one at a
time. They want a single command that brings every repo in an org they can read
onto disk, and that can be re-run at any time to bring that folder up to date —
without trampling work in progress inside those repos.

Previously they kept clones fresh with this loop:

```bash
for D in */; do
  pushd "${D}" && PWD && git fetch --tags --force --prune origin || true && git pull --rebase || true && popd;
done
```

The tool replaces both the browse-and-clone step and this loop, and fixes the
loop's rough edges (e.g. leaving a repo stuck mid-rebase after a failed pull).

## Goals

- One command clones every repo of a GitHub org (or user) that the
  authenticated `gh` user can read, into a destination folder.
- Re-running against the same folder updates existing clones safely.
- Filtering, sorting and limiting which repos are included.
- Installable via Homebrew as a single, lean binary.
- Code a human can easily maintain.

## Non-goals

- Deleting local repo folders whose repo vanished/renamed on GitHub. Never.
- A config file. Everything is flags.
- A LICENSE file. Do not add or commit one (user decision).
- Windows support. (macOS is primary; Linux should just work; nothing actively
  blocks other platforms.)
- Managing auth. `gh` is assumed installed and authenticated; `git` is assumed
  installed.

## Anti-goals (would be a failure even if it "works")

- Losing or hiding local work: discarding uncommitted changes, force-deleting
  branches, leaving repos mid-rebase, silently switching branches.
- Looking like a bulk scraper: default behaviour must be sequential, one repo
  at a time, so an org with bulk-download alerting isn't tripped.
- Shipping a fat binary / runtime (no bundled Node etc.).
- Requiring anything beyond the binary itself (plus the already-present `gh`
  and `git`).

## Decisions made with the user

### Language & distribution
- **Go**, single static binary.
- **GoReleaser** builds macOS (arm64, amd64) and Linux binaries on each git tag
  via GitHub Actions, and updates a Homebrew tap repo in the user's GitHub
  account, so `brew install <user>/tap/git-pull-all-repos` works.

### Invocation

```
git-pull-all-repos <org> [destination] [flags]
```

`<org>` may be any of:
- `https://github.com/orgs/acme` (also with trailing path like `/repositories`)
- `https://github.com/acme`
- `acme`

A personal user account works the same way (`gh repo list` handles both).

Destination:
- **Omitted** → `./<org-name>/<repo>` (folder created if needed).
- **Given** → `<destination>/<repo>` directly, **no** org subfolder.
  Created if missing.

### Flags

| Flag | Meaning | Default |
|------|---------|---------|
| `--no-forks` | Exclude forks | forks included |
| `--no-archived` | Exclude archived repos | archived included |
| `--language <lang>` | Only repos whose primary language matches (same as GitHub web filter; passed to `gh repo list --language`) | no filter |
| `--sort pushed\|stars` | Order: most recently pushed first, or most stars first | `pushed` |
| `--limit N` | Keep the first N repos after filtering + sorting | all |
| `--jobs N` | Repos processed concurrently | **1** (sequential, deliberately) |
| `--no-submodules` | Don't clone/update submodules | submodules on |
| `--dry-run` | Show what would happen; no clone/fetch/pull | off |
| `--version`, `--help` | | |

Mirrors are **always** excluded.

`gh repo list` has no sort option and defaults to 30 results, so the tool asks
`gh` for the full list (with `pushedAt`, `stargazerCount`, etc. in `--json`),
then filters/sorts/limits in Go.

### Talking to GitHub and git
- `gh` is used only to **list repos** and to **supply credentials**.
- All git work uses plain **`git`** (not `gh repo clone`). Reason: `gh repo
  clone` adds an `upstream` remote and sets the parent as default for forks,
  which is surprising in a bulk mirror; plain git gives just `origin` and keeps
  one code path.
- Clone URL protocol follows the user's `gh` setting
  (`gh config get git_protocol`): `https` → repo `url`, `ssh` → `sshUrl`.
- Every git network call (clone, fetch, submodule update) borrows `gh`'s login
  via per-command config, scoped to the GitHub host, e.g.
  `-c credential.https://github.com.helper= -c credential.https://github.com.helper='!gh auth git-credential'`.
  The user's global git config is never modified. (Verified on the user's
  machine: `git_protocol` is `https` and no global credential helper is set, so
  without this, fetches of private repos would fail or prompt.)
- If the org URL's host isn't `github.com` (GitHub Enterprise), the tool passes
  that host through to `gh` (e.g. via `GH_HOST`) and uses it for the
  credential scope.

### Which repos get touched on a run (user decision)

Filters (`--no-forks`, `--no-archived`, `--language`, `--sort`, `--limit`)
decide **what gets cloned**. They do **not** limit what gets updated:

- Every repo from the filtered GitHub list is cloned or updated.
- **Additionally**, every existing clone in the destination folder that
  belongs to this org is updated, even if the filters excluded it this run.
  "Belongs to this org" = an immediate subfolder whose `origin` URL points at
  `<host>/<org>/*` (normalise ssh/https, `.git` suffix, case).
- Clones from other orgs/users in the same folder are left alone and not
  mentioned.
- Repos renamed on GitHub: the old folder still has the old origin URL, which
  GitHub redirects, so it keeps being updated; the new name is cloned into a
  new folder. No deletion, no renaming of folders. (Builder's call: leave this
  duplicate as-is; optionally note it in output.)
- For a clone not in this run's GitHub list, the default branch comes from
  `origin/HEAD` (set at clone time; refresh with
  `git remote set-head origin --auto` if missing).
- `--dry-run` and the summary include these extra clones.

### Per-repo behaviour

For each repo to clone or update, in order:

1. **Not on disk** → `git clone` (with `--recurse-submodules` unless
   `--no-submodules`). Lands on the default branch. Result: `cloned`.
2. **On disk** → always fetch first:
   `git fetch --prune --tags --force origin` — new commits, new branches,
   new/moved tags, and drops `origin/*` refs for branches deleted on GitHub.
   The user explicitly said the exact command is not sacred; the intent is
   "get all new code, branches and tags, and clean up stale tracking refs".
3. **Clean up local branches whose GitHub branch is gone** (upstream shows as
   gone after prune):
   - delete with `git branch -d` **only**; if git refuses (not confirmed
     merged — e.g. squash-merged PRs), keep it and **warn**;
   - **never** `-D`;
   - never the currently checked-out branch (warn instead).
4. **Update**, depending on state:
   - **On default branch, clean** → `git pull --rebase` equivalent. On
     conflict/failure: `git rebase --abort` so the repo is exactly as before,
     then **warn**. Result: `updated` (or `up to date`).
   - **On default branch, uncommitted changes** → fetch only, **warn**.
   - **On another branch (clean or dirty)** → do **not** switch branches.
     Fast-forward the local default branch to `origin/<default>` without
     checking it out (e.g. `git fetch origin <default>:<default>` or a local
     ancestor check + `update-ref`). This never touches the working tree, so
     it's safe even with uncommitted changes. If it can't fast-forward (local
     default has its own commits), **warn**. Stay on the user's branch.
     Then update the **current feature branch itself** (user decision):
     - clean and its upstream exists → rebase onto its upstream
       (`git pull --rebase` equivalent); on conflict `git rebase --abort`
       and **warn**;
     - uncommitted changes → leave it, **warn** (default branch was still
       fast-forwarded above);
     - no upstream (local-only branch) or detached HEAD → nothing to do for
       it, no warning;
     - upstream gone on GitHub → **warn** (it can't be deleted while checked
       out, see step 3).
5. **Submodules** (unless `--no-submodules`): after a successful rebase of the
   checked-out branch (default or feature),
   `git submodule update --init --recursive`. Repos that were only fetched
   leave submodules alone. No-op for repos without submodules.

The tool never checks out a different branch in an existing clone, never
discards changes, never force-deletes.

### Output
- One line per repo as it finishes, e.g.
  `+ repo-a  cloned`, `✓ repo-b  updated`,
  `⚠ repo-c  uncommitted changes, fetched only`,
  `✗ repo-d  failed: <reason>`.
- End-of-run summary: counts (cloned / updated / up to date / warnings /
  failed) and a full list of every warning and failure so none get lost in the
  scroll.
- Exit code non-zero **only** if something failed. Warnings alone → 0.

## Builder's calls (made by me, not confirmed by the user — flag if wrong)

These are my defaults; the user did not weigh in on them.

- **Default `--sort` is `pushed`.** The user agreed this was the main use case.
- **Empty repos** (no commits) are skipped and reported as such; there's no
  default branch to work with.
- **Existing folder that isn't a git repo, or whose `origin` points at a
  different repo** → skip with a warning. Never touch it. Origin comparison
  should normalise ssh/https forms, `.git` suffix and case.
- **Detached HEAD** is treated as "on another branch" (default branch is
  fast-forwarded; nothing else to rebase).
- **Rebase or merge already in progress** in a local repo → treated like
  uncommitted changes: fetch only, warn.
- **"Uncommitted changes"** means changes to tracked files (staged or
  unstaged). Untracked files alone don't block the update; if they would
  collide, the rebase fails and is aborted with a warning per the rule above.
- **Local default branch missing** (e.g. org renamed `master` → `main` since
  the clone) while on another branch → create it from `origin/<default>`.
- The "pull" on the default branch may be implemented as
  `git rebase origin/<default>` after the fetch already done, avoiding a second
  network round-trip; behaviour is the same as `git pull --rebase`.
- `GIT_TERMINAL_PROMPT=0` for all git calls so an auth problem fails fast
  instead of hanging.
- **Submodule update failure** → warning, not failure (the main repo did
  update).
- **Fetch or clone failure** → failure for that repo; carry on with the rest.
- **Setup errors** (gh/git not on PATH, `gh` not authenticated, listing
  fails, bad args) → clear message, exit code 2. Repo failures → exit 1.
- **`--dry-run`** inspects only local state (no network beyond the `gh repo
  list` call) and prints the planned action per repo: would clone / would
  update / would warn (dirty, other branch, not a git repo…).
- **Ctrl-C**: don't start new repos, let git child processes receive the
  signal, print the summary of what completed.
- **Flag parsing** via `spf13/pflag` (tiny dependency) so flags can appear
  before or after the positional args; stdlib `flag` stops at the first
  positional. No cobra.
- **Homebrew cask** doesn't declare `gh`/`git` dependencies (the user said
  they're assumed present); a caveat line mentions them.
- **GitHub identity:** user is `Kiran-B`. Go module path
  `github.com/Kiran-B/git-pull-all-repos`; tap repo `Kiran-B/homebrew-tap`
  (does **not** exist yet — creating it is a public action and needs the
  user's go-ahead at release time). Install line:
  `brew install Kiran-B/tap/git-pull-all-repos`.
- **Formula vs cask:** GoReleaser v2 deprecates `brews` (formulae) in favour
  of `homebrew_casks`. Use a **cask** with a post-install hook that strips the
  quarantine attribute (`xattr -dr com.apple.quarantine`), so the unsigned
  binary isn't blocked by Gatekeeper. No Apple signing/notarisation for now.
  Casks are macOS-only; Linux users download the release tarball. Revisit if
  the user gets a Developer ID or wants `brew` on Linux.
- Because the binary is named `git-pull-all-repos`, `git pull-all-repos …`
  also works as a git subcommand for free.

## Left to the builder

- Package layout. Suggested shape: `main.go` (args/flags, wiring),
  `internal/github` (gh listing, protocol, host), `internal/gitops` (thin git
  runner with credential config), `internal/syncer` (per-repo state machine),
  `internal/report` (per-line output + summary).
- Testing approach. Strong suggestion: unit tests for org-arg parsing and
  filter/sort/limit; integration tests for the per-repo state machine using
  local bare repos as `origin` (no network), covering every state above.
- Exact output formatting/colour (respect `NO_COLOR` and non-TTY).
- README with install, usage and examples.
- GitHub Actions + GoReleaser + tap wiring details (tap repo name, token
  secret).
