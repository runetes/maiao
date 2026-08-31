# Getting Started

Welcome to Maiao! This guide will help you set up and start using stacked pull requests (or merge requests) in your workflow.

## 📋 Prerequisites

Before installing Maiao, ensure you have:
- Git installed (version 2.0+)
- An account on your git hosting provider (GitHub, GitLab, Gitea, Forgejo, Bitbucket Cloud, or Cursor Origin)
- An API token for your provider (see [Configuration](#-configuration) below)

## 📦 Installation

### Homebrew (macOS/Linux)

```bash
brew tap runetes/maiao https://github.com/runetes/maiao.git
brew install maiao
```

#### Migrating from adevinta/maiao

If you previously installed maiao via `brew tap adevinta/maiao`, run:

```bash
brew untap adevinta/maiao
brew tap runetes/maiao https://github.com/runetes/maiao.git
brew reinstall maiao
```

### Binary Installation (Unix/Linux/macOS)

1. Visit the [releases page](https://github.com/runetes/maiao/releases)
2. Download the appropriate binary for your system
3. Install it:

```bash
# Download and install
mv <downloadsDir>/git-review-`uname -s`-`uname -m` /usr/local/bin/git-review
chmod +x /usr/local/bin/git-review

# macOS only: Remove quarantine flag
xattr -d com.apple.quarantine /usr/local/bin/git-review
```

### Windows

1. Visit the [releases page](https://github.com/runetes/maiao/releases)
2. Download `git-review-windows-<arch>` (usually `amd64`)
3. Add to your PATH

### Build from Source

```bash
go build -o /usr/local/bin/git-review ./cmd/maiao
```

## ⚙️ Configuration

### Provider Detection

Maiao auto-detects your provider from the remote URL for known hosts:
- `github.com` → GitHub
- `gitlab.com` → GitLab
- `codeberg.org` → Forgejo
- `bitbucket.org` → Bitbucket Cloud
- `origin.cursor.com` → Cursor Origin

For self-hosted instances, Maiao prompts you on first use and saves the choice to
the repository:

```bash
git config maiao.provider gitlab  # or: github, gitea, forgejo, bitbucket, origin
```

If you work across several repositories on the same self-hosted host, set it once
globally instead and every repository will pick it up:

```bash
git config --global maiao.provider gitlab
```

### Authentication

Maiao tries credentials in this order: environment variable → `~/.netrc` → git credential helpers → system keychain.

#### GitHub / GitHub Enterprise

```bash
export GITHUB_TOKEN=<your-personal-access-token>
```

Or via `~/.netrc`:
```
machine github.com
  login your.username@example.com
  password <your-personal-access-token>
```

Create a token with `repo` scope at [github.com/settings/tokens](https://github.com/settings/tokens).

#### GitLab

```bash
export GITLAB_TOKEN=<your-personal-access-token>
```

Or via `~/.netrc`:
```
machine gitlab.com
  login your.username@example.com
  password <your-personal-access-token>
```

Create a token with `api` scope at Settings → Access Tokens.

#### Gitea

```bash
export GITEA_TOKEN=<your-access-token>
```

Or via `~/.netrc`:
```
machine your-gitea-instance.com
  login your-username
  password <your-access-token>
```

#### Forgejo / Codeberg

```bash
export FORGEJO_TOKEN=<your-access-token>
```

Or via `~/.netrc`:
```
machine codeberg.org
  login your-username
  password <your-access-token>
```

#### Bitbucket Cloud

Bitbucket Cloud requires basic auth with your Atlassian email and an app password:

```bash
export BITBUCKET_USERNAME=your-email@example.com
export BITBUCKET_TOKEN=<your-app-password>
```

Or via `~/.netrc`:
```
machine bitbucket.org
  login your-email@example.com
  password <your-app-password>
```

Create an app password at [Bitbucket Settings → App passwords](https://bitbucket.org/account/settings/app-passwords/) with `Repositories: Read/Write` and `Pull requests: Read/Write` permissions.

#### Cursor Origin (beta)

> **Note:** Cursor Origin is currently in beta. The API and behavior may change.

Cursor Origin uses a git credential helper installed by the Origin CLI. Maiao picks up the token automatically via `git credential fill`:

```bash
# Authenticate with Origin (one-time setup)
origin auth login
```

Alternatively, you can set the token directly:

```bash
export ORIGIN_TOKEN=<your-origin-token>
```

Cursor Origin supports native stacking via `parentPullNumber` — Maiao uses this automatically when creating stacked PRs to register the parent-child relationship.

### 🔐 Experimental: System Keychain (Optional)

Use your OS keychain (macOS Keychain, pass, etc.) instead of environment variables or `.netrc`:

```bash
export MAIAO_EXPERIMENTAL_CREDENTIALS=true
git review
```

Supported keychains: [99designs/keyring](https://pkg.go.dev/github.com/99designs/keyring)

## 🤖 Non-interactive use

CI jobs, scripts and coding agents run Maiao with nobody available to answer a
question. Maiao detects this — when stdin is not a terminal it enters **batch
mode**, where it never prompts. Instead of asking, it fails and names the setting
that would have made the question unnecessary.

Force it either way with `--batch` / `--batch=false`.

Batch mode only changes what happens *instead of* a prompt. It does not skip any
verification, and it does not make Maiao assume an answer.

### Output streams

Diagnostics — progress, prompts, guidance and errors — go to **stderr**. stdout is
reserved for output meant to be read by a program. Redirecting stderr away is
therefore safe for a script, and `2>&1` is what you want when you are reading it
yourself.

### Exit statuses

Maiao distinguishes failures it expects a caller to handle differently:

| Status | Meaning | What to do |
|---|---|---|
| 0 | The review completed | Nothing. Zero changes is also a success |
| 1 | Any other failure | Read stderr |
| 2 | No usable credentials, or the host rejected them | Provide a token; retrying changes nothing |
| 3 | The rebase stopped before the reviews were created | Resolve the conflict and `git rebase --continue`, which finishes the review |
| 4 | An answer was needed and there was no terminal to ask on | Apply the setting the message names |
| 5 | The host presented a different SSH key than the one on record | Stop. See [SSH host key error](#ssh-host-key-error) |

Non-zero still means failure, so `if ! git review` keeps working.

Status 3 is worth knowing about even interactively: `git review` rebases before
submitting, and if that rebase stops there is nothing on the remote yet. The
reviews are created by the step git runs at the end of the rebase, so finishing the
rebase is what completes them.

### Machine-readable results

`--json` prints what the review did to stdout, and nothing else:

```bash
git review --json
```

```json
{
  "changes": [
    {
      "change_id": "I8f3c2a1b5e9d7f6a4c3b2a1d0e9f8c7b6a5d4e3f",
      "branch": "maiao.I8f3c2a1b5e9d7f6a4c3b2a1d0e9f8c7b6a5d4e3f",
      "url": "https://github.com/org/repo/pull/101",
      "id": "101",
      "status": "created"
    }
  ]
}
```

| Field | Meaning |
|---|---|
| `changes[].change_id` | The `Change-Id` trailer, stable across rebases and amends, so it correlates entries between runs |
| `changes[].branch` | The remote branch the commit was pushed to |
| `changes[].url` | The pull or merge request as a person would open it |
| `changes[].id` | The provider's identifier — the number shown in its web interface. A string, because a provider is not obliged to use integers |
| `changes[].status` | `created` or `updated` |
| `stack_id` | The stack the provider recorded, for providers that model one natively. Absent otherwise |
| `error` | Why the review did not complete. **Absent when it did** |

`changes` is ordered as the pull requests are stacked, so the first entry targets
your branch and each later one targets the entry before it. A run with nothing to
review prints `{"changes": []}`.

#### Failures

A failed review still prints its result, because a review can fail after it has
already created some of the pull requests — those exist, and a caller that cannot
see them has no way to tell whether a retry is resuming or starting over.

```json
{
  "changes": [
    {
      "change_id": "I8f3c2a1b5e9d7f6a4c3b2a1d0e9f8c7b6a5d4e3f",
      "branch": "maiao.I8f3c2a1b5e9d7f6a4c3b2a1d0e9f8c7b6a5d4e3f",
      "url": "https://github.com/org/repo/pull/101",
      "id": "101",
      "status": "created"
    }
  ],
  "error": {
    "kind": "auth",
    "code": 2,
    "message": "unable to find token for api.github.com"
  }
}
```

| Field | Meaning |
|---|---|
| `kind` | The failure class: `error`, `auth`, `rebase_incomplete`, `input_required` or `host_key_mismatch` |
| `code` | The process's [exit status](#exit-statuses). The two are the same by construction, so they cannot disagree |
| `message` | The same diagnostic that appears on stderr |

So a review that ran prints `error` exactly when it exits non-zero, and either is
enough to branch on:

```bash
result=$(git review --json)
echo "$result" | jq -r '.changes[].url'   # whatever was submitted, success or not
echo "$result" | jq -e -r '.error.kind // empty' && exit 1
```

One exception: an invalid command line — an unknown flag, too many arguments — is
answered with usage on stderr and no JSON, because there was no review to report
on. Treat empty stdout with a non-zero status as that case.

### Configuring away the prompts

Set these once and Maiao runs unattended:

```bash
# Install the commit-msg hook everywhere, without ever asking
git review install --global

# Say what a self-hosted host is, for every repository at once
git config --global maiao.provider gitlab

# Provide credentials (see Authentication above)
export GITHUB_TOKEN=...
```

### Host keys

Record the host keys when you build the image or sandbox, not when you run a
review:

```dockerfile
RUN mkdir -p ~/.ssh && ssh-keyscan github.com >> ~/.ssh/known_hosts
```

Doing it at build time means the key is fixed by someone who can verify it, and
every later run compares against it.

If you cannot pre-seed, `--trust-new-ssh-hosts` (or
`MAIAO_TRUST_NEW_SSH_HOSTS=1`) lets Maiao accept the key of a host it has no
record of. This is trust on first use: you are trusting whichever key answers.
It is off by default.

Neither the flag nor the environment variable has any effect when the key
**changed** rather than being unknown. A changed key is indistinguishable from an
interception, and resolving it means discarding a key that was known to be good,
so Maiao always fails and leaves `known_hosts` untouched. Resolve it yourself as
described under [SSH host key error](#ssh-host-key-error).

## 🚀 First Time Setup

### 1. Initialize Repository

In your repository, install the Gerrit commit-msg hook:

```bash
cd /path/to/your/repo
git review install
```

This installs a Git hook that automatically adds a unique `Change-Id` to every commit message.

To install it once for every repository instead, so you are never asked again:

```bash
git review install --global
```

This sets two things in your global Git config:

| Setting | Effect |
|---|---|
| `init.templateDir` | Git copies the hook into every repository you create or clone from now on |
| `maiao.autoInstallHook` | `git review` installs the hook itself, without asking, in repositories that already exist |

Both are additive. `init.templateDir` does not redirect your hooks directory, so
repositories with their own hooks, and tools such as husky, lefthook or
pre-commit, keep working. If you already point `init.templateDir` at your own
template, maiao adds the hook to it rather than replacing the setting.

Worktrees need nothing extra: they share the main repository's hooks.

This is the recommended setup for CI and for AI agents, which create
repositories and worktrees often and cannot answer an interactive prompt.

### 2. Verify Installation

Check that the hook is installed:

```bash
ls -la "$(git rev-parse --git-path hooks/commit-msg)"
# Should show the commit-msg hook file
```

Ask git for the path rather than looking in `.git/hooks` directly. The hooks
directory moves when `core.hooksPath` is configured, and inside a worktree
`.git` is a file pointing at the main repository rather than a directory.

## 📖 The Maiao Workflow

Maiao aims to solve the classic code review problem:

![](img/code_reviews_tweet.jpeg)

**Philosophy**: Create small, self-contained commits. Each commit becomes a reviewable PR/MR, stacked on the previous one.

**Benefits**:
- ✅ Faster reviews (smaller changesets)
- ✅ Better feedback (focused on one change)
- ✅ Cleaner history (atomic commits)
- ✅ Easier debugging (bisectable changes)

This workflow works identically across all supported providers — GitHub PRs, GitLab MRs, etc.

## 🎯 Basic Workflow Example

Let's say you're building a new authentication feature requiring changes to 2 files and 2 test files.

### Step-by-Step

**Commit 1: Database Schema**
```bash
# 1. Make changes to database schema
vim db/schema.sql

# 2. Write tests
vim tests/db_test.go

# 3. Ensure tests pass
go test ./tests/db_test.go

# 4. Commit atomically
git add db/schema.sql tests/db_test.go
git commit -m "Add user authentication schema"
# Hook automatically adds: Change-Id: I111abc...
```

**Commit 2: Authentication Logic**
```bash
# 5. Implement auth logic
vim auth/handler.go

# 6. Write tests
vim tests/auth_test.go

# 7. Ensure tests pass
go test ./tests/auth_test.go

# 8. Commit atomically
git add auth/handler.go tests/auth_test.go
git commit -m "Add JWT authentication handler"
# Hook automatically adds: Change-Id: I222def...
```

**Create Stacked PRs**
```bash
# 9. Submit for review
git review
```

**Result:**
```
Created PR https://github.com/org/repo/pull/101
  Title: Add user authentication schema
  Base: main
  Head: maiao.I111abc...

Created PR https://github.com/org/repo/pull/102
  Title: Add JWT authentication handler
  Base: maiao.I111abc...  ← Stacked on PR #101
  Head: maiao.I222def...
```

**Success!** ✨
- Each PR is self-contained
- All tests pass at each step
- Reviewers can review schema changes separately from logic changes
- PRs are automatically stacked (PR #102 depends on PR #101)

## 🔧 Advanced: Responding to Review Feedback

### Scenario 1: Fix the Most Recent Commit (HEAD)

Reviewer comments on **PR #102** (second commit): "The variable name `asdf` is confusing"

```bash
# 1. Make the fix
vim auth/handler.go  # Rename asdf → authToken

# 2. Create fixup commit
git add auth/handler.go
git commit --fixup HEAD
# Creates: "fixup! Add JWT authentication handler"

# 3. Update PRs
git review
```

**Result**: PR #102 is automatically updated with your fix!

### Scenario 2: Fix an Earlier Commit

Reviewer comments on **PR #101** (first commit): "The database query is inefficient"

```bash
# 1. Find the commit hash
git log --oneline
# cf5fd9a Add JWT authentication handler
# abc1234 Add user authentication schema  ← Fix this one

# 2. Make the fix
vim db/schema.sql  # Optimize the query

# 3. Create targeted fixup
git add db/schema.sql
git commit --fixup abc1234
# Creates: "fixup! Add user authentication schema"

# 4. Update PRs
git review
```

**What happens:**
1. Maiao detects the fixup commit
2. Automatically groups it with the original commit (`abc1234`)
3. Rebases and updates PR #101 with the optimization
4. PR #102 is also rebased on top of the updated PR #101

**Result**: Both PRs updated correctly, maintaining the stack! 🎉

### Scenario 3: Multiple Fixups

```bash
# Fix for first commit
git commit --fixup abc1234

# Fix for second commit
git commit --fixup cf5fd9a

# Another fix for first commit
git commit --fixup abc1234

# Apply all fixups
git review
```

Maiao groups all fixups by their target commit and updates PRs accordingly.

## 🎓 Understanding Key Concepts

### Change-IDs

Every commit gets a unique identifier in its message:

```
Add user authentication

Implements JWT-based authentication

Change-Id: I8f3c2a1b5e9d7f6a4c3b2a1d0e9f8c7b6a5d4e3f
```

**Purpose:**
- Tracks commits across rebases
- Enables fixup commit matching
- Maps commits to remote branches

**Generated by:** Gerrit commit-msg hook (installed via `git review install`)

### Branch Naming

Each commit creates a branch: `maiao.<Change-ID>`

**Example:**
- Change-ID: `I8f3c2a1b5e9d7f6a4c3b2a1d0e9f8c7b6a5d4e3f`
- Branch: `maiao.I8f3c2a1b5e9d7f6a4c3b2a1d0e9f8c7b6a5d4e3f`

These branches are **ephemeral** (recreated on each `git review`) and force-pushed.

### Stacking

PRs depend on each other:

```
main
 └─ PR #1 (maiao.I111)
     └─ PR #2 (maiao.I222)
         └─ PR #3 (maiao.I333)
```

When PR #1 merges, `git review` automatically:
1. Detects the merge
2. Rebases remaining commits
3. Updates PR #2 to target `main` instead of `maiao.I111`

## 🔄 Common Workflows

### Starting Fresh

```bash
git checkout main
git pull origin main
git checkout -b feature/my-feature
# Make commits...
git review
```

### Continuing Work

```bash
# Make more commits
git commit -m "Additional changes"
git review  # Updates existing PRs and creates new ones
```

### After PR Merges

```bash
# Someone merged PR #1
git review  # Automatically rebases remaining PRs
```

### Rebasing on Latest Main

```bash
git fetch origin
git review  # Automatically rebases if needed
```

## ❓ Troubleshooting

### "missing Change-Id in commit message"

**Problem:** Commit-msg hook not installed or not working

**Solution:**
```bash
git review install  # Reinstall hook
# Then amend your commits
git commit --amend --no-edit
```

If the hook is present but commits still get no `Change-Id`, check where git
actually looks for it:

```bash
git rev-parse --git-path hooks/commit-msg
git config --get core.hooksPath
```

Hook managers such as husky, lefthook and pre-commit set `core.hooksPath`, which
moves the hooks directory somewhere else entirely. `git review install` follows
that setting, so reinstalling is enough. Note that a *relative* `core.hooksPath`
is resolved against the top of the working tree, so each worktree has its own
hooks directory and needs the hook installed separately.

### "multiple URLs not supported"

**Problem:** Git remote has multiple URLs configured

**Solution:**
```bash
git remote -v  # Check remotes
git remote set-url origin <single-url>
```

### "merge commits are not supported"

**Problem:** Your branch has merge commits

**Solution:**
```bash
# Use rebase instead of merge
git rebase origin/main
```

### "failed to create pull request"

**Problem:** Authentication issue

**Solution:**
```bash
# Check your token is set (see Configuration section above)
# Verify token has the right scopes for your provider
# For Bitbucket: ensure both BITBUCKET_USERNAME and BITBUCKET_TOKEN are set
```

### "401 Unauthorized" on Bitbucket

**Problem:** Bitbucket Cloud requires basic auth with your Atlassian email

**Solution:**
```bash
export BITBUCKET_USERNAME=your-email@example.com
export BITBUCKET_TOKEN=your-app-password
```

### SSH host key error

**Problem:** SSH host key not found, or the key has changed.

These are two different problems and Maiao treats them differently.

**Key not found** is normal the first time you connect to a host. Maiao offers to
add it with `ssh-keyscan`; accept the prompt. In batch mode it refuses instead,
because fetching a key over the network and trusting it unattended is exactly what
an interception needs — see [Non-interactive use](#-non-interactive-use).

**Key changed** means the host presented a different key than the one on record.
That happens when a server is legitimately rekeyed, and equally when the
connection is being intercepted, and Maiao cannot tell the two apart. It never
resolves this on its own. Verify the new key through a channel you trust — your
provider's documentation or status page — then drop the old one:

```bash
ssh-keygen -R git.example.com -f ~/.ssh/known_hosts
```

Pass `-f` explicitly: `ssh-keygen` locates your home directory through the passwd
database and ignores `$HOME`, so without it you may edit a different file than the
one Maiao reads.

A mismatch exits with status 5, kept separate from every other failure precisely so
an automated caller stops rather than retries.

### "the rebase did not complete"

**Problem:** `git review` rebases your commits before submitting them, and that
rebase stopped — a conflict, usually. Nothing has been pushed.

**Solution:**
```bash
git status              # see what conflicts
# resolve the conflicts, then
git add <files>
git rebase --continue
```

Finishing the rebase creates the reviews: Maiao adds itself as the last step of the
rebase, so it runs once the rebase gets there. You do not need to run `git review`
again. This exits with status 3.

### "reference not found"

**Problem:** Default branch detection failed (e.g., repo uses "main" but Maiao falls back to "master")

**Solution:**
```bash
# Pass the target branch explicitly
git review main

# Or ensure remote HEAD is set
git remote set-head origin --auto
```

### "unmatched fixups"

**Problem:** Fixup commit doesn't match any commit in branch

**Solution:**
```bash
git log --oneline  # Find correct commit hash
# Recreate fixup with correct hash
git commit --fixup <correct-hash>
```

### Hook Not Running

**Problem:** Commits don't get Change-IDs

**Solution:**
```bash
# Check hook permissions
chmod +x .git/hooks/commit-msg

# Verify hook content
cat .git/hooks/commit-msg
```

## 💡 Best Practices

### 1. Atomic Commits
Each commit should be a complete, testable change:
```bash
✅ Good: "Add user authentication schema"
❌ Bad:  "WIP changes"
```

### 2. Logical Order
Commit in dependency order:
```bash
✅ First:  Database schema
✅ Second: Business logic using schema
✅ Third:  API endpoints using logic
```

### 3. Test Each Commit
All tests should pass at each commit:
```bash
git add file.go
go test ./...  # ✅ Pass before committing
git commit -m "Add feature"
```

### 4. Descriptive Messages
Write clear commit messages:
```bash
✅ Good: "Add JWT token validation middleware

         Validates JWT tokens from Authorization header
         and rejects requests with invalid/expired tokens"

❌ Bad:  "fix stuff"
```

### 5. Use Fixups Liberally
Don't amend commits directly; use fixups:
```bash
✅ git commit --fixup <hash>  # Trackable, rebaseable
❌ git commit --amend          # Loses Change-ID, breaks tracking
```

## 📚 Next Steps

- **[How Does It Work](how-does-it-work.md)** - Deep dive into technical details
- **[GitHub Issues](https://github.com/runetes/maiao/issues)** - Report bugs or request features
- **[Contributing](../CONTRIBUTING.md)** - Contribute to Maiao
