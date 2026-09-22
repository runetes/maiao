# Commits are the unit of review

Every property of the stack comes from the commits, so shape them first:

- **One logical change per commit.** A commit doing two things is a review nobody
  can give.
- **Each commit builds and passes its tests alone.** The reviewer sees it alone,
  and so does `git bisect`.
- **The documentation a change makes true ships in the same commit** — the README
  line, the docs page, the changelog. A docs-only follow-up means the commit before
  it shipped a lie.
- **Semantic subject saying *what*; body saying *why*** — the failure observed, the
  evidence, what else was tried. Assume the reader can read the diff.
- **Order by dependency.** The stack is the dependency graph.

## Carving one commit out of a dirty tree

`git add .` sweeps in unrelated work, and `git add -p` needs a terminal. Use
[git-surgeon](https://github.com/raine/git-surgeon), which stages by hunk without
prompting:

```bash
git-surgeon hunks                                  # hunk ids, per file
git-surgeon commit 7468249 25c63c5 -m "fix: ..."   # stage exactly those, and commit
```

It refuses to run `commit` when anything is already staged, so the commit holds
precisely the hunks you named. `git-surgeon fold`, `move` and `reword` restructure
commits already made, without an interactive rebase.

## `git-surgeon split` loses the `Change-Id`

Both halves come out without the original trailer, and the `commit-msg` hook —
which git-surgeon does run — then mints a *new* one for each. The pull request of
the commit you split is left with nothing pointing at it, and two new ones appear.

Either put the original `Change-Id:` line back into whichever half inherits the
review, or treat the old pull request as withdrawn and close it by hand.

Observed with git-surgeon 0.1.17 in a scratch repo carrying a Change-Id hook:

```
before  "feat: two things"  Change-Id: Ihook000...
after   "feat: first"       Change-Id: Ihook000...
        "feat: second"      Change-Id: Ihook000...
```
