# git-pull-all-repos Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go CLI, `git-pull-all-repos <org> [dest]`, that clones every repo of a GitHub org the user can read and safely updates existing clones on re-runs. It's distributed as a Homebrew cask.

**Architecture:** `gh` is used only to list repos, read `git_protocol`, and supply credentials. Plain `git` does all repo work. The pieces are: a thin `gitops` runner, a per-repo state machine in `syncer`, discovery of existing clones on disk, a `report` package for output and the summary, and an `app` package that wires everything together behind `main.go`.

**Tech Stack:** Go 1.25 (via mise), `github.com/spf13/pflag`, stdlib only otherwise. GoReleaser v2 and GitHub Actions for releases.

**Spec:** `docs/specs/2026-10-13-git-pull-all-repos-design.md`

## Global Constraints

- Module path is `github.com/Kiran-B/git-pull-all-repos`. Binary name is `git-pull-all-repos`.
- The only third-party dependency is `github.com/spf13/pflag`. No cobra.
- Do not create or commit a LICENSE file.
- Commit messages must contain no AI/assistant references and no Co-Authored-By lines.
- Never run `git branch -D`, never discard changes, and never `checkout`/`switch` in an existing clone. Never delete a folder.
- Every git call sets `GIT_TERMINAL_PROMPT=0`. Every git network call (clone, fetch, submodule update) adds `-c credential.https://<host>.helper=` and `-c credential.https://<host>.helper=!gh auth git-credential`.
- Mirrors are always excluded. Default `--sort` is `pushed` and default `--jobs` is `1`.
- Exit codes: `0` means success or warnings only, `1` means at least one repo failed, `2` means a setup or usage error.
- Tests must not touch the network. Git tests use local bare repos, and app tests use a fake `gh` script on `PATH`.
- Go toolchain: `.tool-versions` containing `golang 1.25.8` (mise honours it; verified). No global Go is set, so run go via `mise exec -- go ...` if `go` is not on PATH.

## Review Focus

1. **Quoted `~` in the destination** (`"~/my-folder"` reaches the program literally): expand a leading `~/` to `$HOME`. The test goes in Task 1.
2. **Default branch renamed on GitHub** (local clone on `master`, GitHub now `main`): `master` is treated as a feature branch whose upstream is gone. It should produce a warning, local `main` should be created from `origin/main`, and nothing should be deleted. The test goes in Task 5.
3. **Auth or network failure** must fail fast, without hanging on a password prompt. A clone of a nonexistent URL gives a `Failed` result and the run continues. The test goes in Task 4.
4. **Destination contains stray files** (`.DS_Store`, plain files, non-git dirs): discovery ignores them silently, and only a listed repo's own folder that isn't a git repo produces a warning. The test goes in Task 7.
5. **Org arg variants** (trailing slash, `/repositories` suffix, mixed case, `http://`, GHE host): all of these parse to the same `(host, org)`. The test goes in Task 1.

---

## File Structure

```
main.go                         flag parsing → app.Run → os.Exit
internal/cli/args.go            ParseOrg, ResolveDest, Options, Parse
internal/github/list.go         Repo, Filter, ListArgs, ParseList, Select, Lister (exec gh)
internal/gitops/git.go          Git runner + state queries
internal/testgit/testgit.go     test helpers: bare origins, commits, clones (test-only)
internal/syncer/types.go        Target, Result, Action
internal/syncer/sync.go         Syncer.Sync: per-repo state machine
internal/syncer/branches.go     gone-branch cleanup, default fast-forward
internal/syncer/discover.go     NormalizeRemote, Discover existing clones
internal/report/report.go       per-line output, summary, exit code
internal/app/app.go             orchestration, worker pool, signals
.goreleaser.yaml, .github/workflows/release.yml, README.md, .gitignore, .gitattributes, .tool-versions
```

---

### Task 0: Repository bootstrap (user decision: the first commit is `.gitignore` + `.gitattributes`)

**Files:**
- Create: `.gitignore`, `.gitattributes`

- [ ] **Step 1: Run** `git init -b main`.
- [ ] **Step 2: Write `.gitignore`.** It covers:
  - the built binary `/git-pull-all-repos`
  - GoReleaser's `dist/`
  - Go test and coverage artifacts (`*.test`, `*.out`, `coverage.*`)
  - `go.work`, `go.work.sum`
  - macOS junk (`.DS_Store`, `._*`)
  - editor dirs (`.idea/`, `.vscode/`, `*.swp`)
  - `.env*`
  - local Claude settings (`.claude/settings.local.json`)
- [ ] **Step 3: Write `.gitattributes`.** It contains:
  - `* text=auto eol=lf`
  - `*.go diff=golang`
  - `go.sum linguist-generated=true`
  - `*.png binary` and `*.jpg binary`
- [ ] **Step 4: Commit only those two files**: `git add .gitignore .gitattributes && git commit -m "chore: add .gitignore and .gitattributes"`.
- [ ] **Step 5: Commit the docs** (`docs/specs`, `docs/plans`) with the message `docs: design spec and implementation plan`.

---

### Task 1: Scaffold and CLI argument parsing

**Files:**
- Create: `go.mod`, `.tool-versions`, `internal/cli/args.go`
- Test: `internal/cli/args_test.go`

**Interfaces:**
- Produces:
  - `type Options struct { Host, Org, Dest string; NoForks, NoArchived bool; Language, Sort string; Limit, Jobs int; NoSubmodules, DryRun, ShowVersion bool }`
  - `func ParseOrg(arg string) (host, org string, err error)`. Default host is `github.com`.
  - `func ResolveDest(org, destArg, home string) string`
  - `func Parse(args []string, home string) (Options, error)`. This uses a `pflag.FlagSet` with interspersed args. It returns `ErrHelp` (a re-export of `pflag.ErrHelp`) for `-h/--help`.

- [ ] **Step 1: Init.** Write `.tool-versions` with `golang 1.25.8`; run `go mod init github.com/Kiran-B/git-pull-all-repos`, and `go get github.com/spf13/pflag`.
- [ ] **Step 2: Write failing tests.**

```go
func TestParseOrg(t *testing.T) {
	cases := map[string][2]string{
		"acme":                                       {"github.com", "acme"},
		"https://github.com/orgs/acme":               {"github.com", "acme"},
		"https://github.com/orgs/acme/":              {"github.com", "acme"},
		"https://github.com/orgs/acme/repositories":  {"github.com", "acme"},
		"https://github.com/acme":                    {"github.com", "acme"},
		"http://GitHub.com/Acme/":                    {"github.com", "Acme"},
		"https://ghe.corp.example/orgs/platform":     {"ghe.corp.example", "platform"},
	}
	// each: host, org match; err nil
	// errors: "", "https://github.com/", "https://github.com/orgs/"
}
func TestResolveDest(t *testing.T) {
	// ("acme","","/h")            -> "acme"          (relative ./acme)
	// ("acme","/x/my-folder/","/h") -> "/x/my-folder"
	// ("acme","~/my-folder","/h")  -> "/h/my-folder"
	// ("acme","~","/h")            -> "/h"
}
func TestParse(t *testing.T) {
	// ["acme","--limit","5","/tmp/d","--sort","stars"] -> Org acme, Dest /tmp/d, Limit 5, Sort stars, Jobs 1
	// defaults: Sort "pushed", Jobs 1, Limit 0 (0 = all)
	// errors: no args; 3 positionals; --sort name; --jobs 0; --limit -1
	// ["--version"] -> ShowVersion true, no org required
}
```

- [ ] **Step 3: Run** `go test ./internal/cli/`. Expected: FAIL (undefined).
- [ ] **Step 4: Implement** `args.go`. `ParseOrg` lowercases the host only, strips a leading `orgs/` path segment, and takes the next segment as the org.
- [ ] **Step 5: Run** `go test ./internal/cli/`. Expected: PASS.
- [ ] **Step 6: Commit** with the message `feat: CLI argument parsing`.

---

### Task 2: GitHub listing, filter, sort, limit

**Files:**
- Create: `internal/github/list.go`
- Test: `internal/github/list_test.go`, `internal/github/testdata/list.json`

**Interfaces:**
- Produces:
  - `type Repo struct { Name, URL, SSHURL, DefaultBranch, Language string; IsArchived, IsFork, IsMirror, IsEmpty bool; PushedAt time.Time; Stars int }`
  - `type Filter struct { NoForks, NoArchived bool; Language string }`
  - `const ListFields = "name,url,sshUrl,defaultBranchRef,primaryLanguage,isArchived,isFork,isMirror,isEmpty,pushedAt,stargazerCount"`
  - `func ListArgs(org string, f Filter) []string`. This returns `["repo","list",org,"--limit","100000","--json",ListFields]`, plus `--source` if NoForks, `--no-archived` if NoArchived, and `--language X` if set.
  - `func ParseList(data []byte) ([]Repo, error)`. It maps `defaultBranchRef.name`, `primaryLanguage.name`, and `stargazerCount`.
  - `func Select(repos []Repo, sortBy string, limit int) []Repo`. It drops mirrors, sorts (`pushed` means PushedAt desc, `stars` means Stars desc), uses Name asc as a tiebreak, then applies the limit (`0` means all).
  - `type Lister struct { Host string }`. This runs `gh` with `GH_HOST=<host>` in env when the host isn't `github.com`.
  - `func (l Lister) List(ctx context.Context, org string, f Filter) ([]Repo, error)`
  - `func (l Lister) GitProtocol(ctx context.Context) (string, error)`. This runs `gh config get git_protocol -h <host>` and returns `"https"` or `"ssh"`. An empty result means `"https"`.
  - `func (r Repo) CloneURL(protocol string) string`

- [ ] **Step 1: Write failing tests.** The fixture holds 4 repos: one mirror, one fork, one with null `primaryLanguage`, and one empty with null `defaultBranchRef`.
  - `TestParseList`: fields are mapped, and null nested objects give empty strings.
  - `TestSelect`: the mirror is dropped; `pushed` order is newest first; `stars` order is highest first; ties are broken by name; `limit 2` gives 2 items; `limit 0` gives all.
  - `TestListArgs`: `--limit 100000` is always present; each Filter field maps to its exact flag.
  - `TestCloneURL`: `ssh` gives SSHURL and anything else gives URL.
- [ ] **Step 2: Run** `go test ./internal/github/`. Expected: FAIL.
- [ ] **Step 3: Implement.** `Lister.List` uses `exec.CommandContext("gh", ListArgs(...)...)` and wraps stderr into the error.
- [ ] **Step 4: Run** the tests. Expected: PASS.
- [ ] **Step 5: Commit** with the message `feat: list and select org repos via gh`.

---

### Task 3: Git runner, state queries, test helpers

**Files:**
- Create: `internal/gitops/git.go`, `internal/testgit/testgit.go`
- Test: `internal/gitops/git_test.go`

**Interfaces:**
- Produces (`gitops`):
  - `type Git struct { Host string; ExtraConfig []string }`. ExtraConfig entries are `key=value` pairs passed as `-c` on every call. Tests use it for `protocol.file.allow=always`.
  - `func (g *Git) Run(ctx context.Context, dir string, args ...string) (string, error)`. It returns trimmed stdout. On error, it returns an error that includes trimmed stderr.
  - `func (g *Git) RunNet(ctx context.Context, dir string, args ...string) (string, error)`. This is the same as Run but adds the credential `-c` pair from Global Constraints.
  - `func (g *Git) IsRepo(dir string) bool`. This checks that `rev-parse --show-toplevel` equals dir (after resolving symlinks).
  - `func (g *Git) OriginURL(ctx, dir) (string, error)`
  - `func (g *Git) CurrentBranch(ctx, dir) (branch string, detached bool, err error)`
  - `func (g *Git) HasTrackedChanges(ctx, dir) (bool, error)`. This uses `status --porcelain --untracked-files=no`.
  - `func (g *Git) OperationInProgress(dir string) bool`. This checks for `rebase-merge`, `rebase-apply`, `MERGE_HEAD`, or `CHERRY_PICK_HEAD` in `git rev-parse --git-dir`.
  - `func (g *Git) DefaultBranchFromOrigin(ctx, dir) (string, error)`. This reads `refs/remotes/origin/HEAD`. If that's missing, it runs `remote set-head origin --auto` once and retries.
  - `func (g *Git) Upstream(ctx, dir, branch string) (ref string, gone bool, err error)`. It returns `""` when no upstream is configured.
  - `func (g *Git) GoneBranches(ctx, dir) ([]string, error)`. It uses `for-each-ref --format=%(refname:short)%09%(upstream:track) refs/heads` and keeps the entries equal to `[gone]`.
  - `func (g *Git) RevParse(ctx, dir, ref string) (string, error)`
  - `func (g *Git) IsAncestor(ctx, dir, a, b string) (bool, error)`
- Produces (`testgit`, test-only):
  - `func Env(t *testing.T)`. This sets `HOME` to a temp dir, plus `GIT_CONFIG_NOSYSTEM=1` and author/committer name and email.
  - `func NewOrigin(t, branch string) (bareDir string)`. It creates a bare repo with one commit on `branch` and HEAD pointing to it.
  - `func Commit(t, bareDir, branch, file, content string)`. It pushes a new commit via a scratch clone.
  - `func Clone(t, bareDir, dest string)`
  - `func Git(t, dir string, args ...string) string`. This runs plain git and fails the test on error.

- [ ] **Step 1: Write failing tests** for each query against repos built with `testgit`:
  - clean versus modified tracked file versus untracked-only (`false`);
  - branch name versus detached;
  - `DefaultBranchFromOrigin` returns `main`, including after `git remote set-head origin -d`;
  - `GoneBranches` lists a branch whose remote branch was deleted then pruned;
  - `OperationInProgress` is true during a conflicted rebase;
  - `RunNet` against a nonexistent `file:///nope` fails within 10s (Review Focus 3) and its error contains stderr text.
- [ ] **Step 2: Run** `go test ./internal/gitops/`. Expected: FAIL.
- [ ] **Step 3: Implement.** Set env as `os.Environ()` plus `GIT_TERMINAL_PROMPT=0`.
- [ ] **Step 4: Run** the tests. Expected: PASS.
- [ ] **Step 5: Commit** with the message `feat: git runner and repo state queries`.

---

### Task 4: Syncer core: clone and default-branch update

**Files:**
- Create: `internal/syncer/types.go`, `internal/syncer/sync.go`
- Test: `internal/syncer/sync_test.go`

**Interfaces:**
- Consumes: `gitops.Git` and `testgit` from Task 3.
- Produces:
  - `type Action string` with constants `Cloned="cloned"`, `Updated="updated"`, `UpToDate="up to date"`, `FetchedOnly="fetched only"`, `Skipped="skipped"`, `Failed="failed"`, `WouldClone="would clone"`, `WouldUpdate="would update"`.
  - `type Target struct { Name, Dir, CloneURL, DefaultBranch string; IsEmpty bool }`. An empty DefaultBranch means "read from origin" (for discovered clones).
  - `type Result struct { Name string; Action Action; Warnings []string; Err error }`
  - `type Syncer struct { Git *gitops.Git; Submodules, DryRun bool; Org string }`. Org is used to check that origin matches.
  - `func (s *Syncer) Sync(ctx context.Context, t Target) Result`

State machine for this task (Task 5 adds the feature-branch path):
1. `IsEmpty` gives `Skipped` with the warning `"empty repository"`.
2. If the dir is missing, run `RunNet clone [--recurse-submodules] <url> <dir>`. The result is `Cloned`, or `Failed` on error.
3. If the dir exists but isn't a repo, the result is `Skipped` with the warning `"folder exists but is not a git repository"`.
4. If origin doesn't match `Org` and `Name` (use `NormalizeRemote` from Task 7; until Task 7 lands, compare with a stub returning true; Task 7 replaces it), the result is `Skipped` with the warning `"origin points to <url>, not this repo"`.
5. Run `RunNet fetch --prune --tags --force origin`. On error the result is `Failed`.
6. On the default branch:
   - with an operation in progress, `FetchedOnly` with the warning `"rebase/merge in progress, fetched only"`;
   - with tracked changes, `FetchedOnly` with the warning `"uncommitted changes, fetched only"`;
   - otherwise, `rebase origin/<default>`. If it fails, run `rebase --abort` and give `FetchedOnly` with the warning `"rebase conflict, left unchanged"`. If HEAD moved, the result is `Updated`; otherwise `UpToDate`.

- [ ] **Step 1: Write failing tests**, one per row, all built with `testgit`:
  - `TestSync_ClonesMissing` (checks the result is `Cloned` and the clone is on `main`);
  - `TestSync_EmptySkipped`;
  - `TestSync_NotARepo`;
  - `TestSync_CloneFailureContinues` (bad URL gives `Failed` and `Err` is non-nil, Review Focus 3);
  - `TestSync_UpToDate`;
  - `TestSync_PullsNewCommit` (`Updated`, and the new file exists);
  - `TestSync_DirtyFetchesOnly` (`FetchedOnly`, and `origin/main` has advanced but the local working file is unchanged);
  - `TestSync_RebaseConflictAborted` (local commit conflicts with origin, so `FetchedOnly` with a warning, `OperationInProgress` is false, and HEAD is unchanged);
  - `TestSync_LocalCommitsRebased` (a non-conflicting local commit ends up on top of origin).
- [ ] **Step 2: Run** `go test ./internal/syncer/`. Expected: FAIL.
- [ ] **Step 3: Implement** `types.go` and the `Sync` steps above.
- [ ] **Step 4: Run** the tests. Expected: PASS.
- [ ] **Step 5: Commit** with the message `feat: clone and update default branch`.

---

### Task 5: Feature branches, default fast-forward, gone-branch cleanup

**Files:**
- Create: `internal/syncer/branches.go`
- Modify: `internal/syncer/sync.go` (add steps after the fetch)
- Test: `internal/syncer/branches_test.go`

**Interfaces:**
- Consumes: the Task 3 queries and the Task 4 `Syncer`/`Result`.
- Produces (unexported, used only by `Sync`):
  - `func (s *Syncer) cleanupGone(ctx, dir, current string) (warnings []string)`. For each gone branch other than `current`, it runs `git branch -d`. On refusal it adds the warning `"branch <b>: deleted on GitHub but not merged, kept"`. If `current` is gone, it adds `"current branch <b> was deleted on GitHub"`.
  - `func (s *Syncer) fastForwardDefault(ctx, dir, def string) (moved bool, warning string)`. If the local `def` is missing, it runs `branch --track def origin/def`. If local is an ancestor of origin, it runs `update-ref refs/heads/def origin/def`. If origin is an ancestor of local, nothing happens. If they've diverged, it gives the warning `"local <def> has diverged from origin, not updated"`.

The new order in `Sync` after the fetch is: cleanupGone, then the branch-specific update. When not on the default branch (or detached):
1. Run `fastForwardDefault`.
2. If detached, there's no upstream, or the upstream is gone, stop there. The action is `Updated` if the default moved, otherwise `UpToDate`.
3. With an operation in progress or tracked changes, the result is `FetchedOnly` with the matching warning from Task 4.
4. Otherwise, `rebase @{u}`, with abort and a warning on conflict, the same as Task 4.

- [ ] **Step 1: Write failing tests**:
  - `TestFeature_DefaultFastForwardedWithoutSwitch` (still on `feat`, local `main` equals `origin/main`, working tree untouched);
  - `TestFeature_DirtyStillFastForwardsDefault`;
  - `TestFeature_RebasedOntoUpstream` (remote `feat` gets a new commit, and local `feat` gains it);
  - `TestFeature_ConflictAborted`;
  - `TestFeature_LocalOnlyBranchNoWarning`;
  - `TestFeature_DetachedHead`;
  - `TestDefault_Diverged` (on `feat`, local `main` has its own commit, so a warning and `main` is unchanged);
  - `TestGone_MergedDeleted`;
  - `TestGone_UnmergedKeptWithWarning` (use a squash-merge on origin);
  - `TestGone_CurrentBranchWarned`;
  - `TestDefaultRenamed` (Review Focus 2: the clone is on `master`, origin renames it to `main` and deletes `master`, and `origin/HEAD` is updated via `Target.DefaultBranch="main"`. Expect a warning about the current branch, local `main` to exist at `origin/main`, the current branch to still be `master`, and nothing to be deleted).
- [ ] **Step 2: Run** `go test ./internal/syncer/`. Expected: FAIL.
- [ ] **Step 3: Implement** `branches.go` and wire it into `Sync`.
- [ ] **Step 4: Run** `go test ./internal/syncer/`. Expected: all PASS, including Task 4.
- [ ] **Step 5: Commit** with the message `feat: feature-branch updates and gone-branch cleanup`.

---

### Task 6: Submodules and dry-run

**Files:**
- Modify: `internal/syncer/sync.go`
- Test: `internal/syncer/submodule_test.go`, `internal/syncer/dryrun_test.go`

**Interfaces:**
- Consumes: `Syncer.Submodules` and `Syncer.DryRun`.
- Behavior:
  - **Submodules:** after any successful rebase (default or feature), when `Submodules` is set, run `RunNet submodule update --init --recursive`. On failure, add the warning `"submodule update failed: <err>"` and keep the action unchanged.
  - **Dry-run:** do no clone, fetch, rebase, update-ref, or branch delete. A missing dir gives `WouldClone`. Not-a-repo and origin mismatch give the same `Skipped` results as before. Otherwise the result is `WouldUpdate`, with warnings for an operation in progress, tracked changes (`"uncommitted changes, would fetch only"`), or being on a non-default branch (`"on branch <b>; default would be fast-forwarded"`).

- [ ] **Step 1: Write failing tests**:
  - `TestSubmodules_ClonedRecursively` (needs `ExtraConfig: protocol.file.allow=always`);
  - `TestSubmodules_UpdatedAfterPull` (the parent bumps the submodule pointer and the clone's submodule follows);
  - `TestSubmodules_Disabled` (the submodule dir stays empty);
  - `TestDryRun_NoChanges` (snapshot `git for-each-ref` and `status` before and after; they're identical, and the missing repo is not created);
  - `TestDryRun_Actions` (`WouldClone`/`WouldUpdate` plus warnings).
- [ ] **Step 2: Run** `go test ./internal/syncer/`. Expected: FAIL.
- [ ] **Step 3: Implement** both behaviors.
- [ ] **Step 4: Run** the tests. Expected: PASS.
- [ ] **Step 5: Commit** with the message `feat: submodules and dry-run`.

---

### Task 7: Remote normalisation and discovery of existing clones

**Files:**
- Create: `internal/syncer/discover.go`
- Modify: `internal/syncer/sync.go` (replace the Task 4 origin-match stub)
- Test: `internal/syncer/discover_test.go`

**Interfaces:**
- Produces:
  - `func NormalizeRemote(url string) (host, owner, name string, ok bool)`. It handles `https://host/o/n(.git)`, `ssh://git@host/o/n.git`, and `git@host:o/n.git`. Host and owner are lowercased, and the name has `.git` stripped. Local paths give `ok=false`.
  - `func OriginMatches(url, host, org, name string) bool`. It compares case-insensitively. A `file://` or plain local path URL matches when its last two path segments are `<org>/<name>(.git)`, so tests can use local origins laid out as `<tmp>/<org>/<name>.git`.
  - `func Discover(ctx context.Context, g *gitops.Git, dest, host, org string, listed map[string]bool) ([]Target, error)`. It scans immediate subdirectories of `dest`, skips non-dirs, entries in `listed` (lowercased dir names), and non-repos, and keeps repos whose origin owner matches `org`. Each result is `Target{Name: dirName, Dir: path, DefaultBranch: ""}`. A missing `dest` gives nil with no error.

- [ ] **Step 1: Write failing tests**:
  - `TestNormalizeRemote` (the 3 URL forms plus mixed case and a GHE host);
  - `TestOriginMatches` (with the local layout described above);
  - `TestDiscover` (dest contains a listed clone, an unlisted clone of the same org, a clone from another org, a `.DS_Store` file, and a plain non-git dir; only the unlisted same-org clone is returned, with no error and no warning, per Review Focus 4);
  - `TestSync_OriginMismatchSkipped`;
  - `TestSync_DiscoveredUsesOriginHead` (`DefaultBranch` empty, so it resolves to `main` and updates).
- [ ] **Step 2: Run** `go test ./internal/syncer/`. Expected: FAIL.
- [ ] **Step 3: Implement**, and update `Sync` so that an empty `DefaultBranch` falls back to `Git.DefaultBranchFromOrigin`.
- [ ] **Step 4: Run** `go test ./...`. Expected: PASS.
- [ ] **Step 5: Commit** with the message `feat: discover and update existing org clones`.

---

### Task 8: Report output, summary, exit code

**Files:**
- Create: `internal/report/report.go`
- Test: `internal/report/report_test.go`

**Interfaces:**
- Consumes: `syncer.Result` and `syncer.Action`.
- Produces:
  - `type Reporter struct { Out io.Writer; Color bool }`. Color is on only when stdout is a TTY and `NO_COLOR` is unset; `main` decides.
  - `func (r *Reporter) Line(res syncer.Result)`
  - `func (r *Reporter) Summary(results []syncer.Result, interrupted bool)`
  - `func ExitCode(results []syncer.Result) int`. It returns 1 if any `Action == Failed`, else 0.

Line format: `<sym> <name>  <action>[: <first warning or err>]`. The symbol is `✗` when Failed, `⚠` when there are warnings or the action is Skipped, `+` for Cloned or WouldClone, and `✓` otherwise. Extra warnings go on following lines, indented by 4 spaces.

The summary is a blank line, then `Done: N cloned, N updated, N up to date, N fetched only, N skipped, N failed`. Only non-zero counts are listed, and in dry-run the counts are `would clone`/`would update`. Then come `Warnings:` and `Failures:` sections listing `  <name>: <msg>` for every entry. If `interrupted`, a final `Interrupted: remaining repos not processed.` line is added.

- [ ] **Step 1: Write failing tests** for: each symbol; multi-warning indentation; summary counts omitting zeros; the Warnings and Failures sections; the interrupted line; `ExitCode` returning 0 for warnings only and 1 when a failure is present; and no ANSI escapes when `Color` is false.
- [ ] **Step 2: Run** `go test ./internal/report/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the tests. Expected: PASS.
- [ ] **Step 5: Commit** with the message `feat: per-repo output and run summary`.

---

### Task 9: App orchestration and main

**Files:**
- Create: `internal/app/app.go`, `main.go`
- Test: `internal/app/app_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces:
  - `func Run(ctx context.Context, opts cli.Options, stdout, stderr io.Writer) int`
  - `main.go` has `var version = "dev"` (set by ldflags). It calls `cli.Parse(os.Args[1:], home)`. On `--help` it prints usage and exits 0. On a parse error it prints the error and usage to stderr and exits 2. `--version` prints `git-pull-all-repos <version>`. Otherwise it sets up `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` and calls `os.Exit(app.Run(...))`.

`Run` steps:
1. `exec.LookPath` for `git` and `gh`. If either is missing, print the error and return 2.
2. Run `Lister.GitProtocol`, then `Lister.List`, then `github.Select`. If listing fails, print the gh error (which typically indicates an auth problem) and return 2.
3. Create `opts.Dest` with `MkdirAll` (not in dry-run).
4. Build Targets from the selection: `Dir = dest/<Name>` and `CloneURL = repo.CloneURL(protocol)`. Then append the `Discover` results, using `listed` = the lowercased names of every selected repo.
5. Run a worker pool of `opts.Jobs` goroutines feeding results to a single reporter goroutine, which calls `Line` as each repo finishes. Stop dispatching new targets once `ctx` is done.
6. Call `Summary(results, ctx.Err() != nil)`. Return `ExitCode`, or 1 if interrupted with no failures.

- [ ] **Step 1: Write failing tests.** These use a fake `gh` script written to a temp dir and prepended to `PATH`. It echoes a JSON fixture for `repo list` and prints `https` for `config get`. The fixture `url`s point at local bare repos laid out as `<tmp>/acme/<name>.git`. Tests:
  - `TestRun_FirstRunClonesAll` (exit 0, all dirs exist);
  - `TestRun_SecondRunUpdates` (new commit pulled);
  - `TestRun_LimitStillUpdatesExistingClones` (first run all, second `--limit 1`: an unselected clone still receives its new commit);
  - `TestRun_MirrorExcluded`;
  - `TestRun_FailureExitCode1` (one fixture URL points nowhere, others succeed, exit 1);
  - `TestRun_MissingGh` (with `PATH` lacking gh, exit 2);
  - `TestRun_JobsParallelSameResult` (`--jobs 3` gives the same final state);
  - `TestRun_DryRunCreatesNothing` (the dest dir doesn't exist afterwards).
- [ ] **Step 2: Run** `go test ./internal/app/`. Expected: FAIL.
- [ ] **Step 3: Implement** `app.go` and `main.go`.
- [ ] **Step 4: Run** `go test ./... && go vet ./... && go build -o git-pull-all-repos .`. Expected: all pass and the binary builds.
- [ ] **Step 5: Manual smoke test** against a real small org the user names (ask first; this hits the network): `./git-pull-all-repos <org> /tmp/gpar-smoke --limit 3`, then run it again. Expected: 3 `+ cloned` on the first run, then `✓ up to date` lines.
- [ ] **Step 6: Commit** with the message `feat: wire up CLI end to end`.

---

### Task 10: Release pipeline and README

**Files:**
- Create: `.goreleaser.yaml`, `.github/workflows/release.yml`, `.github/workflows/test.yml`, `README.md`

**Interfaces:**
- Consumes: `main.version`.

The `.goreleaser.yaml` (version 2) is configured as follows:
- `builds`: one build with `CGO_ENABLED=0`, `goos [darwin, linux]`, `goarch [amd64, arm64]`, and `ldflags -s -w -X main.version={{.Version}}`.
- `archives`: tar.gz.
- `homebrew_casks`: `name: git-pull-all-repos`, `repository: {owner: Kiran-B, name: homebrew-tap, token: "{{ .Env.HOMEBREW_TAP_GITHUB_TOKEN }}"}`, `homepage: https://github.com/Kiran-B/git-pull-all-repos`, a description matching the repo description, and a `caveats` line saying it requires `gh` (authenticated) and `git`. It also has `hooks.post.install` that runs `xattr -dr com.apple.quarantine` on the staged binary when on macOS. Use the GoReleaser docs' documented snippet for this.
- No license field.

The `release.yml` workflow runs on tags `v*`: checkout with `fetch-depth: 0`, `setup-go` 1.25, `go test ./...`, then `goreleaser/goreleaser-action@v6` with `release --clean`. The env includes `GITHUB_TOKEN` and `HOMEBREW_TAP_GITHUB_TOKEN`.

The `test.yml` workflow runs on push and PR, on `macos-latest` and `ubuntu-latest`, and runs `go test ./...`.

The README covers: what the tool does (one paragraph); install with `brew install Kiran-B/tap/git-pull-all-repos`; requirements (`gh` authenticated, `git`); the usage examples from the spec; a flags table copied from the spec; a "What happens to each repo" section (a condensed version of the spec's per-repo rules, including never deleting anything or switching branches); and a "Releasing" section (create the `Kiran-B/homebrew-tap` repo, add the `HOMEBREW_TAP_GITHUB_TOKEN` secret, then `git tag vX.Y.Z && git push --tags`).

- [ ] **Step 1: Validate** with `goreleaser check`. Expected: no errors (deprecation notices are acceptable only if not about `homebrew_casks`).
- [ ] **Step 2: Snapshot build** with `goreleaser release --snapshot --clean --skip=publish`. Expected: 4 archives in `dist/` and a generated cask file.
- [ ] **Step 3: Check binary size** with `ls -lh dist/*/git-pull-all-repos`. Expected: each under about 8 MB.
- [ ] **Step 4: Commit** with the message `build: goreleaser, CI and README`. Do not push, create the tap repo, or tag. Those are public actions for the user to approve.
