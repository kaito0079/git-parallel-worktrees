# pwt - Git Parallel Worktrees

A thin wrapper around `git worktree`. The argument syntax matches `git worktree add`, with two conveniences on top: bare `<path>` names are placed in a shared location, and shared assets are synced into each worktree through `.worktreelinks`.

日本語版は [docs/README.ja.md](docs/README.ja.md) にあります。

## Design

pwt stays a **pure wrapper around git worktree**. Its CLI syntax and behavior follow `git worktree`; it does not invent its own argument scheme (deriving a directory name from a branch name, for example). Everything pwt adds is limited to these three things:

- placing a bare `<path>` under a shared work base
- moving between worktrees (`pwt switch` / `pwt path`)
- syncing symlinks and copies described by `.worktreelinks` (`pwt sync` / `pwt unsync`)

## Install

### Homebrew

```bash
brew tap kaito0079/tap
brew install pwt
```

### go install

```bash
go install github.com/kaito0079/git-parallel-worktrees/cmd/pwt@latest
```

### From source

```bash
git clone https://github.com/kaito0079/git-parallel-worktrees
cd git-parallel-worktrees
go build -o pwt ./cmd/pwt
```

## Shell integration (optional)

**Every feature works without this.** Setting it up only adds the ability for `pwt switch` to change your current directory.

A child process cannot change its parent shell's working directory, so moving with `cd` needs a shell function. `shell/pwt.sh` is that function; add one line to `~/.zshrc` (or `~/.bashrc`) to enable it.

```bash
# Homebrew
echo '. "$(brew --prefix)/share/pwt/pwt.sh"' >> ~/.zshrc

# Manual install
echo '. /path/to/shell/pwt.sh' >> ~/.zshrc
```

`.` is the POSIX form; `source` is equivalent in bash and zsh.

Without the shell function, move with `pwt path`:

```bash
cd "$(pwt path 2)"
```

### Completion

```bash
pwt completion zsh  > "${fpath[1]}/_pwt"
pwt completion bash > /usr/local/etc/bash_completion.d/pwt
```

## Getting started

```bash
cd /path/to/your-project

# 1. Generate .worktreelinks (seeded from your .gitignore)
pwt init

# 2. Uncomment the patterns you want linked
vim .worktreelinks

# 3. Create your first worktree and move into it
#    -b creates a branch, my-task is the directory name, main is the base
pwt switch -c -b feature/my-task my-task main
```

## Sharing files (`.worktreelinks`)

Files and directories matching the patterns in `.worktreelinks` are symlinked or copied from the main repository into the worktree.

```
# .worktreelinks example
# Symlinks are the default (good for things you want to keep in one place)
.env
.env.*
docker-compose.override.yml
.claude/settings.local.json

# Entries after [copy] are copied instead (for cases where symlinks
# do not work, such as inside Docker)
[copy]
vendor/
node_modules/

# [link] switches back to symlink mode
[link]
.docker/
```

Pattern matching is delegated to `git ls-files --exclude-from`. The format is exactly `.gitignore`'s, and pwt does not reimplement any of that matching itself.

### Symlink vs copy

| | Symlink (default) | Copy (`[copy]`) |
|---|---|---|
| Use for | things kept in one place (`.env`) | things that must stand alone (`vendor/`) |
| Speed | instant | proportional to file count |
| Sees upstream edits | immediately (single real file) | no (independent copy) |
| Docker | target breaks outside the container | fine |

### Rules

- `pwt add` sets up the links and copies automatically
- **each worktree gets its own copy of `.worktreelinks`**, so it can be configured individually
- committing `.worktreelinks` shares the configuration with your team

### What `pwt sync` does

`pwt sync` removes the existing symlinks that point into the main repository and recreates them. Symlinks that stay inside the worktree are left alone, so symlinks tracked by the repository survive even when the worktree lives inside the repository itself.

`[copy]` entries are skipped when a real file or directory is already at the destination. Use `-f` to overwrite:

```bash
pwt sync -f        # remove the real file/directory and copy again
```

### Updating libraries in one worktree

To update a library in a single worktree only:

```bash
# 1. Edit that worktree's .worktreelinks
vim .worktreelinks      # comment out node_modules

# 2. Recreate the symlinks (the node_modules link goes away)
pwt sync

# 3. Install a real copy for this worktree only
npm install

# Other worktrees are untouched (still symlinked)
```

To drop every symlink at once:

```bash
pwt unsync
```

## Commands

`pwt add` takes the same arguments as `git worktree add`.

| Command | Description |
|---------|-------------|
| `pwt` | list worktrees (numbered, current one marked) |
| `pwt list` | list worktrees (explicit) |
| `pwt path <index\|name>` | print a worktree's absolute path and nothing else |
| `pwt switch <index\|name>` | move to a worktree (needs shell integration) |
| `pwt switch -c [-b <branch>] [-B <branch>] [--detach] <path> [<commit-ish>]` | create a worktree and move into it |
| `pwt add [-b <branch>] [-B <branch>] [--detach] <path> [<commit-ish>]` | create a worktree (without moving) |
| `pwt remove <branch\|name\|.>` | remove a worktree (branch name, directory name, or `.` for the current one) |
| `pwt init` | generate `.worktreelinks` |
| `pwt sync [-f]` | re-sync the current worktree's links and copies |
| `pwt unsync` | remove the current worktree's symlinks into the main repository |
| `pwt completion <shell>` | print a completion script |

`pwt remove` asks for confirmation when the target was resolved by a partial match, and again when the worktree has uncommitted changes.

Exit codes: `0` success, `1` runtime error, `2` bad arguments.

### Examples

```bash
# New branch with a directory name of your choosing
# (useful when the branch is named after a ticket)
pwt add -b feature/PROJ-123 review_1 main

# Check out an existing branch into a directory you name
pwt add review_1 feature/PROJ-123

# Create a branch named after the directory (git worktree's default)
pwt add hotfix

# Handle a branch containing `/` with a single argument (auto branch mode);
# both the directory and the branch become feature/PROJ-123
pwt switch -c feature/PROJ-123
```

### How `<path>` is interpreted

- **bare name** (no slash, e.g. `review_1`) → placed under the work base, honoring `pwt.worktreePrefix`
- **contains a slash, or is absolute** → passed to `git worktree add` as-is

### Auto branch mode

When `<path>` is neither absolute nor an explicit relative path (`./foo`, `../foo`), and none of `-b` / `-B` / `--detach` / `<commit-ish>` is given, `<path>` is also used as the branch name. This works for both bare names and names containing `/`.

| State | Behavior |
|-------|----------|
| `refs/heads/<path>` exists | check that branch out |
| only `refs/remotes/origin/<path>` exists | create a local branch tracking origin |
| neither exists | create a new branch from HEAD |

pwt prints one line saying which case applied. The worktree directory uses `<path>` as-is (`work_base/<path>`), matching the branch name.

Use `-b` when the directory and the branch should differ:

```bash
pwt switch -c -b feature/PROJ-123 review_1 main   # branch feature/PROJ-123, directory review_1
```

## Navigation

```bash
pwt                      # list worktrees (numbered, current marked with >)
pwt switch 2             # move by index
pwt switch feature       # move by partial branch-name match
```

Example output:

```
=== myapp ===
  > 0  /repos/myapp                  (main)
    1  /repos/myapp--review_1        (feature/PROJ-123)
    2  /repos/myapp--hotfix          (hotfix)
```

## Directory layout

When `<path>` is a bare name, pwt places the worktree under the work base. By default that is next to the main repository:

```
/repos/myapp/                      ← main repository
/repos/myapp--review_1/            ← worktree (path: review_1)
/repos/myapp--hotfix/              ← worktree (path: hotfix)
```

### Changing where worktrees go

`pwt.worktreeDir` controls both the location and the directory naming:

| Setting | Result for `pwt add review_1` | prefix |
|---|---|---|
| unset | `/repos/myapp--review_1/` | `<repo>--<path>` |
| `.worktrees` | `/repos/.worktrees/myapp--review_1/` | `<repo>--<path>` |
| `./.worktrees` | `/repos/myapp/.worktrees/review_1/` | `<path>` only |

```bash
# Collect worktrees in a dedicated sibling directory
git config pwt.worktreeDir .worktrees

# Keep them inside the main repository (self-contained per project)
git config pwt.worktreeDir ./.worktrees
```

### Forcing the prefix

`pwt.worktreePrefix` overrides whether the `<repo>--` prefix is applied:

| Value | Behavior |
|---|---|
| `auto` (default) | inferred from the location (no prefix for `./X`, prefix otherwise) |
| `repo` | always `<repo>--<slug>` |
| `none` | always just `<slug>` |

```bash
# Keep worktrees inside the main repo but still prefix them
git config pwt.worktreeDir ./.worktrees
git config pwt.worktreePrefix repo
# → /repos/myapp/.worktrees/myapp--feature-auth/
```

### Environment variable override

```bash
export GIT_PARALLEL_WORKTREES_BASE=/path/to/worktrees
```

It must be an absolute path to an existing directory.

## Requirements

- **OS**: macOS / Linux
- **Requires**: git
- **Shell integration**: any POSIX-compatible shell. `shell/pwt.sh` uses no bash- or zsh-specific syntax and is tested against bash, zsh and dash.

## Development

```bash
go test ./...
go vet ./...
gofmt -l .
```

Release:

```bash
goreleaser release --clean     # build binaries and create the GitHub Release
go run ./tools/formula         # generate the Homebrew formula (dist/Formula/pwt.rb)
```

The tests in `internal/links`, `internal/cli` and `shell` are integration tests that invoke real `git` and real shells.

## License

MIT
