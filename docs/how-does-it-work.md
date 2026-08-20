# How Does Maiao Work?

## 🎯 Overview

Maiao implements a **stacked diffs** workflow for GitHub, inspired by Gerrit's code review model. It transforms your linear commit history into individual, stacked pull requests where each commit is independently reviewable while maintaining proper dependencies.

## 📚 Background: Stacked Diffs

**Stacked diffs** (also called stacked PRs) is a code review methodology where large features are broken into small, sequential changes. Each change is reviewed independently while maintaining logical dependencies.

**Learn more about stacked diffs:**
- [Stacked Diffs Versus Pull Requests](https://jg.gg/2018/09/29/stacked-diffs-versus-pull-requests/) - Jackson Gabbard's foundational article
- [Graphite's Guide to Stacked Diffs](https://graphite.dev/guides/stacked-diffs) - Comprehensive guide to the methodology
- [The Pragmatic Engineer: Stacked Diffs](https://newsletter.pragmaticengineer.com/p/stacked-diffs) - Analysis of stacked diffs at scale (paywalled)

## 🔑 Core Mechanism: Change-IDs

### What is a Change-ID?

Each commit gets a **unique, persistent identifier** added to its commit message by the Gerrit commit-msg hook:

```
Add user authentication

Implements JWT-based authentication for API endpoints

Change-Id: I8f3c2a1b5e9d7f6a4c3b2a1d0e9f8c7b6a5d4e3f
```

### Purpose of Change-IDs

1. **Track commits across rebases**: Change-ID remains constant even when commit SHA changes
2. **Enable fixup matching**: `git commit --fixup` matches commits by title, Change-ID confirms the match
3. **Map to GitHub branches**: Each Change-ID generates a unique branch name (`maiao.<Change-ID>`)
4. **Detect merged changes**: Identify which commits already exist in the target branch

### How Change-IDs are Generated

The Gerrit commit-msg hook (`pkg/gerrit/gerrit.go:19-25`):
1. Downloads from the [official Gerrit repository](https://github.com/GerritCodeReview/gerrit)
2. Installs to `.git/hooks/commit-msg`
3. Executes before every commit
4. Generates a unique `I<40-char-hex>` identifier based on commit content
5. Appends `Change-Id: I...` to the commit message

## 🌿 Branch Naming Convention

Each commit creates an **ephemeral remote branch**: `maiao.<Change-ID>`

**Example:**
```
Commit SHA:    abc1234567890def
Change-ID:     I8f3c2a1b5e9d7f6a4c3b2a1d0e9f8c7b6a5d4e3f
GitHub Branch: maiao.I8f3c2a1b5e9d7f6a4c3b2a1d0e9f8c7b6a5d4e3f
```

**Key Properties:**
- **Ephemeral**: Recreated on every `git review` run
- **Force-pushed**: Always overwritten (safe because Change-ID is stable)
- **Deterministic**: Same Change-ID always produces same branch name

## 🔄 The Review Workflow

When you run `git review`, Maiao executes a multi-phase process:

### Phase 1: Fetch and Analysis

**Code:** `pkg/maiao/review.go:91-110`

```go
// 1. Fetch latest changes from origin
remote.Fetch(&git.FetchOptions{RemoteName: options.Remote})

// 2. Find merge-base (common ancestor)
mergeBase := lgit.MergeBase(ctx, repo, remoteRef, headRef)

// 3. Determine if rebase needed
needRebase := remoteCommit.String() != mergeBase.String()
```

**Example repository state:**
```
                 A---B---C topic (your branch)
                /
           D---E---F---G origin/main
```

### Phase 2: Rebase (if needed)

**Triggers:**
- `origin/main` has moved ahead
- Commits missing Change-IDs
- Parent-child relationships broken

**Code:** `pkg/maiao/review.go:195-217`

```go
// 1. Extract changes from local branch
changes := extractChanges(ctx, repo, base, head)

// 2. Identify already-merged Change-IDs
knownChangeIDs := extractChangeIDs(ctx, repo, base, remoteHead)

// 3. Filter out merged changes
changes = removeMergedChangeIDs(changes, knownChangeIDs)

// 4. Execute interactive rebase
lgit.RebaseCommits(ctx, repo, base, remoteHead, rebaseTODO(changes))
```

**Example:** If origin/main updated while you were working:

**Before rebase:**
```
                 A---B---C topic
                /
           D---E---F---G---H---I origin/main (updated)
```

**After rebase:**
```
           D---E---F---G---H---I origin/main
                                \
                                 A'---B'---C' topic (rebased)
```

### Phase 3: Extract Changes

**Code:** `pkg/maiao/review.go:382-436`

```go
func extractChanges(ctx, repo, base, head) []*change {
    // Walk commits from HEAD to merge-base
    for commit := head; commit != base; commit = commit.Parent() {
        message := lgit.Parse(commit.Message)

        if message.IsFixup() {
            // Group fixup with its parent by title matching
            fixupCommits[message.GetTitle()] = append(fixups, commit)
        } else {
            // Create new change with any pending fixups
            changeID, _ := message.GetChangeID()
            change := &change{
                changeID: changeID,
                branch:   "maiao." + changeID,
                commits:  [commit] + fixupCommits[message.GetTitle()],
                head:     lastCommit,
            }
            changes = append(changes, change)
        }
    }
    return changes
}
```

**Result:** Structured changes with fixups grouped

```go
[]*change{
    {changeID: "I111", branch: "maiao.I111", commits: [A], head: A},
    {changeID: "I222", branch: "maiao.I222", commits: [B], head: B},
    {changeID: "I333", branch: "maiao.I333", commits: [C], head: C},
}
```

### Phase 4: Push Branches

**Code:** `pkg/maiao/review.go:231-257`

```go
// Build refspecs for each change
refspecs := []config.RefSpec{}
for _, change := range changes {
    // Push commit SHA to its maiao.* branch
    refspecs = append(refspecs,
        config.RefSpec(change.head.Hash + ":refs/heads/" + change.branch))
}

// Force-push all branches at once
repo.Push(&git.PushOptions{
    RefSpecs: refspecs,
    Force:    true,
})
```

**Result on GitHub:**
```
origin/main  → I (commit I's SHA)
maiao.I111   → A' (commit A's rebased SHA)
maiao.I222   → B' (commit B's rebased SHA)
maiao.I333   → C' (commit C's rebased SHA)
```

### Phase 5: Create/Update Pull Requests

**Code:** `pkg/maiao/review.go:264-291`

```go
var parent *change
for i, change := range changes {
    change.parent = parent

    // Build PR options with stacking
    opts := prOptions(repo, prAPI, options, change, changes[:i], changes[i+1:])

    // Create or find existing PR
    pr, created, err := prAPI.Ensure(ctx, opts)

    change.pr = pr
    parent = change  // Next PR stacks on this one
}
```

**PR Structure Created:**
```
PR #1: maiao.I111 → origin/main
       Title: "Add user authentication"
       Base: origin/main
       Head: maiao.I111

PR #2: maiao.I222 → maiao.I111
       Title: "Add authorization middleware"
       Base: maiao.I111  ← Stacked on PR #1
       Head: maiao.I222

PR #3: maiao.I333 → maiao.I222
       Title: "Add admin endpoints"
       Base: maiao.I222  ← Stacked on PR #2
       Head: maiao.I333
```

## 📊 Visual Workflow Example

**Initial State:**
```
           D---E---F---G origin/main
                        \
                         A---B---C topic
```

**After `git review`:**
```
           D---E---F---G origin/main
                        \
                         A PR #1 (maiao.I111)
                          \
                           B PR #2 (maiao.I222)
                            \
                             C PR #3 (maiao.I333)

## 🔧 The Fixup Workflow

### Creating Fixups

When you receive review feedback, create fixup commits:

```bash
# Fix commit A
git commit --fixup <SHA-of-A>
# Creates: "fixup! Original commit title"

# Fix commit B
git commit --fixup <SHA-of-B>
```

**Your branch now:**
```
           D---E---F---G origin/main
                        \
                         A---B---C---fixup(A)---fixup(B) topic
```

### Fixup Detection and Grouping

**Code:** `pkg/git/message.go:74-80, pkg/maiao/review.go:409-416`

```go
// Detect fixup commits
func (m *Message) IsFixup() bool {
    return strings.HasPrefix(strings.ToLower(m.Title), "fixup! ")
}

// Group fixups by original commit title
if message.IsFixup() {
    fixupCommits[message.GetTitle()] = append([]*object.Commit{c}, fixupCommits[message.GetTitle()]...)
} else {
    changeCommits := []*object.Commit{c}
    if fixups, ok := fixupCommits[message.GetTitle()]; ok {
        changeCommits = append(changeCommits, fixups...)  // Attach fixups
    }
}
```

### Rebase with Fixups

**Code:** `pkg/maiao/review.go:335-348`

When `git review` runs, it generates a rebase TODO:

```
pick abc1234 Add user authentication
pick def5678 Add authorization middleware
pick ghi9012 Add admin endpoints
pick jkl3456 fixup! Add user authentication
pick mno7890 fixup! Add authorization middleware
```

Then executes interactive rebase to squash fixups into their parents.

### Result After `git review`

**Rebased and force-pushed:**
```
           D---E---F---G origin/main (updated)
                        \
                         A'---B'---C' topic (includes fixups)

PR #1: maiao.I111 (updated with fixup(A))
PR #2: maiao.I222 (updated with fixup(B))
PR #3: maiao.I333 (unchanged)
```

## ✅ Merge Detection and Stack Collapse

### When a PR Merges

When reviewers approve and merge PR #1:

**Before merge:**
```
           D---E---F---G origin/main
                        \
                         A PR #1 (maiao.I111)
                          \
                           B PR #2 (maiao.I222)
                            \
                             C PR #3 (maiao.I333)
```

**After merge:**
```
           D---E---F---G---A origin/main (A merged)

                         B PR #2 (still exists, now stale)
                          \
                           C PR #3 (still exists)

topic still has:  A---B---C
```

### Automatic Stack Update

**Code:** `pkg/maiao/review.go:200-205, 360-380`

Next `git review` execution:

```go
// 1. Extract Change-IDs from origin/main
knownChangeIDs := extractChangeIDs(ctx, repo, base, remoteHead)
// Result: {"I111": struct{}{}}  ← A's Change-ID found in main

// 2. Filter out merged changes
changes = removeMergedChangeIDs(changes, knownChangeIDs)
// Result: Only B and C remain

// 3. Rebase remaining commits onto new main
lgit.RebaseCommits(ctx, repo, oldBase, newRemoteHead, rebaseTODO(changes))
```

**Result:**
```
           D---E---F---G---A origin/main
                            \
                             B' PR #2 (updated)
                               \
                                C' PR #3 (updated)

PR #2 base changed: maiao.I111 → origin/main
PR #3 base unchanged: maiao.I222 (rebased)
```

## 🔍 Technical Deep Dive

### Commit Message Parsing

**Code:** `pkg/git/message.go:26-52`

```go
func Parse(message string) *Message {
    scanner := bufio.NewScanner(strings.NewReader(message))
    scanner.Scan()
    m := &Message{
        Title:   scanner.Text(),
        Headers: map[string]string{},
    }

    // Parse headers (Change-Id, Signed-off-by, etc.)
    for scanner.Scan() {
        line := scanner.Text()
        parts := headerRe.FindStringSubmatch(line)  // Match "Key: Value"
        if len(parts) == 3 {
            m.Headers[parts[1]] = parts[2]
        } else {
            m.Body += line
        }
    }
    return m
}
```

### Change-ID Extraction

**Code:** `pkg/git/message.go:94-104`

```go
func (m *Message) GetChangeID() (changeID string, ok bool) {
    changeID, ok = m.Headers["Change-Id"]
    return
}
```

### GitHub API Integration

**Code:** `pkg/api/github.go:48-96`

```go
func (g *GitHub) Ensure(ctx, options) (*PullRequest, bool, error) {
    // 1. List existing PRs for this head branch
    prs := g.PullRequests.List(ctx, g.Owner, g.Repository, &github.PullRequestListOptions{
        Head: g.Owner + ":" + options.Head,
    })

    switch len(prs) {
    case 0:
        // Create new PR
        pr := g.PullRequests.Create(ctx, g.Owner, g.Repository, &github.NewPullRequest{
            Title: options.Title,
            Body:  options.Body,
            Base:  options.Base,
            Head:  options.Head,
        })
        return pr, true, nil  // Created
    case 1:
        return prs[0], false, nil  // Already exists
    }
}
```

### Force Push Strategy

**Code:** `pkg/maiao/review.go:249-257`

```go
err = repo.Push(&git.PushOptions{
    RefSpecs:   refspecs,        // All maiao.* branches
    Force:      true,            // Force-push (safe: ephemeral branches)
    Auth:       gitAuth,
    RemoteName: options.Remote,
})
```

**Why force-push is safe:**
- `maiao.*` branches are ephemeral (recreated each run)
- Change-IDs provide commit identity persistence
- No one should work directly on `maiao.*` branches (they're PR branches)

## 🧩 Key Algorithms

### Merge-Base Detection

**Purpose:** Find the common ancestor between your branch and origin/main

**Code:** Uses git's merge-base algorithm to find the first commit that exists in both branches

### Parent-Child Validation

**Code:** `pkg/maiao/review.go:155-193`

```go
func changesNeedRebase(ctx, changes) bool {
    var parent *object.Commit
    for _, change := range changes {
        for _, commit := range change.commits {
            if parent != nil {
                commitParent := commit.Parent(0)
                if commitParent.Hash != parent.Hash {
                    return true  // Parent mismatch, need rebase
                }
            }
            parent = commit
        }
    }
    return false
}
```

**Purpose:** Ensure commits are properly stacked (each commit's parent is the previous commit)

### Fixup Title Matching

**Code:** `pkg/git/message.go:82-92`

```go
func (m *Message) GetTitle() string {
    t := m.Title
    for isFixupTitle(t) {
        t = t[len("fixup! "):]  // Strip "fixup! " prefix(es)
    }
    return t  // Returns original commit title
}
```

**Purpose:** Extract the original commit title from nested fixups (`fixup! fixup! Original`)

## 🎓 Advanced Concepts

### Idempotency

`git review` is **idempotent**: Running it multiple times produces the same result.

**Guarantees:**
- Same Change-IDs → same branch names → same PRs
- Force-push ensures branches always reflect current commit state
- PR Ensure() checks for existing PRs before creating

### Change-ID Persistence

Change-IDs persist across:
- ✅ Interactive rebases
- ✅ Cherry-picks
- ✅ Commit message edits (if Change-Id line preserved)
- ❌ Squash merges (destroys individual commits)
- ❌ Commit --amend without preserve (loses Change-Id)

### Race Conditions

**Protected against:**
- Multiple `git review` runs (last write wins, deterministic)
- Simultaneous PR creation (GitHub API handles duplicates)

**Not protected against:**
- Manual edits to `maiao.*` branches (will be overwritten)
- Direct pushes to PR branches (changes lost on next `git review`)

## 📚 References and Further Reading

- **[Gerrit Code Review](https://gerrit-review.googlesource.com/)** - Original inspiration for Change-IDs
- **[Stacked Diffs vs Pull Requests](https://jg.gg/2018/09/29/stacked-diffs-versus-pull-requests/)** - Philosophy and benefits
- **[Graphite Dev Guide](https://graphite.dev/guides/stacked-diffs)** - Modern stacked diff workflows
- **[The Pragmatic Engineer on Stacked Diffs](https://newsletter.pragmaticengineer.com/p/stacked-diffs)** - Industry adoption analysis

## 🔗 Related Documentation

- **[Getting Started](getting-started.md)** - Installation and basic usage
- **[Source Code](../pkg/maiao/review.go)** - Core implementation
- **[GitHub Issues](https://github.com/runetes/maiao/issues)** - Report bugs or request features
