---
name: git-review
description: Use when pull requests come from `git review` or maiao, commits carry `Change-Id:` trailers, or changes must be stacked.
---

# git-review - Stacked pull requests with maiao

**One commit is one pull request, and the `Change-Id:` trailer — not the SHA — is
which pull request.** The trailer survives rebase, amend and cherry-pick; never
edit or drop one. Your commit order is the stack. Each change is force-pushed to
a branch named `maiao.<Change-Id>` on every run, so never commit on one or push to
it by hand. `git review` is idempotent and needs no feature branch: it reviews
whatever is on `HEAD` and not yet on the remote target branch, so committing
straight on `main` works.

## Triggers
- A repository whose pull requests are opened with `git review`
- Commit messages carrying `Change-Id:`, or `maiao.I…` branches on the remote
- Review feedback that must reach a commit below the top of the stack
- A change that must be withdrawn from a stack
- `git review` run from CI or an agent, with nobody to answer a prompt

## The four operations

**A change under review is amended, never patched.** Review feedback goes into the
commit that earned it, so the pull request always shows the change as it should
have been written. `fix:` is for a malfunction already on the target branch — a new
`fix:` commit answering a reviewer both adds a pull request nobody asked for and
announces, in the changelog, a bug that never shipped.

| Goal | Do this |
|---|---|
| Create | commit, then `git review` |
| Update any change in the stack | `git commit --fixup <sha>`, then `git review` |
| Reorder or restructure | `git-surgeon move` / `fold` / `reword`, or `git rebase -i`, then `git review` |
| Withdraw a change | drop the commit, `git review`, **then close the PR and delete its branch yourself** |

- Shaping commits, and carving one out of a dirty tree → [commits.md](commits.md)
- Updating and withdrawing, and the trailer trap → [operations.md](operations.md)
- Installing maiao, the hook and credentials → [setup.md](setup.md)
- Exit statuses for CI and agents → [unattended.md](unattended.md)
- What goes wrong, and what it costs → [mistakes.md](mistakes.md)

## Red flags — stop

- About to write a `fix:` commit for something a reviewer just pointed at → that change is not on the target branch yet. `git commit --fixup <sha>` it into the commit that introduced it.
- About to `git rebase -i` to answer review feedback → `git commit --fixup <sha>` reaches any commit in the stack, non-interactively.
- About to report a change as *removed* because `git review` succeeded → maiao has no close path. Its pull request is still open.
- About to commit with no `Change-Id:` in the message → install the hook first; do not push it.
- About to hand-edit a `maiao.*` branch → the next run force-pushes over it.
- About to rewrite a message that already has a `Change-Id:` → keep it in the last paragraph with the other trailers, and parse it back before pushing.
