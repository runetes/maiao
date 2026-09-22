# Updating and withdrawing a change

## What a review comment is, and is not

A commit under review has not shipped. Whatever a reviewer finds in it is not a
bug in the product — it is the commit not yet being right — so the answer is to
amend that commit, never to add a second one describing what the first got wrong.

That makes the commit type the tell:

| The change | The commit |
|---|---|
| Answering review feedback on a commit in the stack | `git commit --fixup <sha>` — no type, no new pull request |
| A malfunction already on the target branch | `fix:`, its own commit and its own pull request |
| Anything else not yet merged | amend the commit that should have carried it |

A `fix:` commit answering a reviewer costs twice. It opens a pull request nobody
asked for, and — where the changelog is generated from commit types — it announces
a bug that never reached a user, in the same release as the change that was said to
contain it. The reviewer then reads the flaw and the repair as two changes and has
to reconstruct which lines survived.

Amending keeps the pull request showing the change as it should have been written,
which is the only version anyone will read after it merges.

## Update: `--fixup`, not an interactive rebase

```bash
git commit --fixup abc1111    # fixes the bottom of the stack
git commit --fixup ghi3333    # fixes the top of the stack
git review                    # one run updates both pull requests
```

`git review` matches each `fixup!` commit to its target **by title**, squashes it
in during its own rebase, and force-pushes every affected branch. So fixups reach
any commit in the stack, in any order, with one non-interactive command each.

`git rebase -i` with `edit` works too, but it cannot be driven non-interactively
and a conflict leaves you stranded mid-rebase. Prefer `--fixup`.

`git commit --amend` is fine, but only reaches `HEAD`. It keeps the `Change-Id`:
the hook does nothing when the trailer is already present.

## Rewriting a message by hand: the trailer trap

Git treats **only the last paragraph** as trailers, so a `Change-Id:` with a blank
line under it is prose as far as the hook is concerned — it adds a second one, and
the next `git review` opens a second pull request against it. Keep it in one
unbroken block with the other trailers:

```
Change-Id: I40d238...
Co-Authored-By: ...
```

Check before pushing, and trust the parser rather than the file:

```bash
git log -1 --format=%B | git -c trailer.separators=:= interpret-trailers --no-divider --parse
```

## Withdraw: maiao will not close anything

**maiao only creates and updates pull requests. It has no close, abandon or delete
path.** Dropping a commit makes maiao stop touching that pull request — nothing
more. The PR stays open and `maiao.<Change-Id>` stays on the remote, pointing at a
commit that is now in no branch.

Read the Change-Id off the commit before it is gone:

```bash
git show -s --format=%B def2222 | grep Change-Id   # -> I222...
git rebase --onto abc1111 def2222                  # drop it, replaying what follows
git review                                         # restack what is left
```

Then, on the forge:

```bash
gh pr close 102                                    # or your provider's equivalent
git push origin --delete maiao.I222...             # if closing the PR did not delete it
```

A change leaves the stack by itself **only** when its `Change-Id` shows up on the
target branch — that is how a merged pull request disappears. A dropped commit
never merges, so nothing does this for it.
