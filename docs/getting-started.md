# Getting Started

Welcome to Maiao! This guide will help you set up and start using stacked pull requests in your GitHub workflow.

## 📋 Prerequisites

Before installing Maiao, ensure you have:
- Git installed (version 2.0+)
- GitHub account with repository access
- GitHub Personal Access Token with `repo` scope ([create one here](https://github.com/settings/tokens))

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

### GitHub Authentication

Create a `~/.netrc` file with your GitHub credentials:

```
machine github.com
  login your.username@example.com
  password <your-personal-access-token>
```

**For GitHub Enterprise:**
```
machine github.company.example.com
  login firstname.lastname@company.example.com
  password <your-token>
```

### 🔐 Experimental: System Keychain (Optional)

Use your OS keychain (macOS Keychain, pass, etc.) instead of `.netrc`:

```bash
export MAIAO_EXPERIMENTAL_CREDENTIALS=true
git review
```

Supported keychains: [99designs/keyring](https://pkg.go.dev/github.com/99designs/keyring)

## 🚀 First Time Setup

### 1. Initialize Repository

In your repository, install the Gerrit commit-msg hook:

```bash
cd /path/to/your/repo
git review install
```

This installs a Git hook that automatically adds a unique `Change-Id` to every commit message.

### 2. Verify Installation

Check that the hook is installed:

```bash
ls -la .git/hooks/commit-msg
# Should show the commit-msg hook file
```

## 📖 The Maiao Workflow

Maiao aims to solve the classic code review problem:

![](img/code_reviews_tweet.jpeg)

**Philosophy**: Create small, self-contained commits. Each commit becomes a reviewable PR, stacked on the previous one.

**Benefits**:
- ✅ Faster reviews (smaller changesets)
- ✅ Better feedback (focused on one change)
- ✅ Cleaner history (atomic commits)
- ✅ Easier debugging (bisectable changes)

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

## Installation

### Homebrew users

To install maiao with homebrew, you need to tap the repo before installing it:

```bash
brew tap runetes/maiao https://github.com/runetes/maiao.git
brew install maiao
```

### Unix users

The simplest way to install maiao is to download the binary from github. To do so, visit
the [releases](https://github.com/runetes/maiao/releases) page and download the version you want to install. Then run

```
mv <downloadsDir>/git-review-`uname -s`-`uname -m` /usr/local/bin/git-review
chmod +x /usr/local/bin/git-review
```

!!! warning "MacOS users"
To make sure the binary is not quarantined run: `xattr -d com.apple.quarantine /usr/local/bin/git-review`

To build a standalone binary, you will need to run `go build -o /usr/local/bin/git-review ./cmd/maiao`

### Windows users

The relevant Windows binary can be downloaded in the [releases](https://github.com/runetes/maiao/releases) page

Download the `git-review-windows-<arch>` where `<arch>` is the architecture of your machine. If you are unsure, usually,
amd64 is the default windows architecture.

## Configuration

If you have ever installed and configured `adv` command line, you should already be good to go. If not, you will need to
configure a github token in your netrc file. To do so, follow these steps:

Create your personal [access token](https://github.company.example.com/settings/tokens) with `repo` privileges and
create a file `~/.netrc` having the following content:

```
machine github.company.example.com
  login <firstname>.<lastname>@company.example.com
  password <your token>
```

### Experimental system keychain

You may now use all the default keyrings supported by [`99-designs/keyring`](https://pkg.go.dev/github.com/99designs/keyring@v1.2.2#section-readme).

To enable it, ensure you export the `MAIAO_EXPERIMENTAL_CREDENTIALS=true` environment variable before running `git review`.
Maiao will then use your system keyring like [macOS keychain](https://support.apple.com/en-au/guide/keychain-access/welcome/mac) or [pass](https://www.passwordstore.org/)
to store GitHub API keys.

We will be happy to receive your feedback about this feature in [maiao's issues](https://github.com/runetes/maiao/issues) 


Your environment is then ready to run maiao.

There will be some other configuration required for each repository you need to run maiao on. Don't worry, maiao will
prompt and suggest you to perform the setup if the configuration is missing.


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
- Maps commits to GitHub branches

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
# Check ~/.netrc has correct token
# Or verify GitHub token has 'repo' scope
# Recreate token: https://github.com/settings/tokens
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
