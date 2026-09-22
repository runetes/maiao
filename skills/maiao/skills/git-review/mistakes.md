# Common mistakes

| Mistake | What actually happens |
|---|---|
| Assuming a dropped commit closes its pull request | It does not. An open PR and an orphan branch are left behind, silently |
| A `fix:` commit answering review feedback | The reviewed change had not shipped, so nothing was broken. A pull request nobody asked for opens, and the changelog announces a bug no user ever saw. `--fixup` the commit that earned the comment |
| `git rebase -i` + `edit` to fix a lower commit | Works, but needs a terminal and strands you on conflict. `--fixup` is one command |
| Editing or removing a `Change-Id` | The commit loses its identity; the next run opens a **second** pull request |
| A `Change-Id` left above a blank line when rewriting a message | Git reads only the **last** paragraph as trailers, so the hook does not see it and adds another. Two `Change-Id:` lines, and a second pull request against the new one |
| Pushing to a `maiao.*` branch by hand | Overwritten by the next force-push |
| Re-running `git review` after exit status 3 | Finish the rebase instead — its last step is what creates the reviews |
| Squashing several concerns into one commit | One pull request nobody can review |
| `git add .` to build a commit | Sweeps in unrelated work. `git-surgeon commit <hunk-ids>` takes only what you name |
| `git-surgeon split` on a reviewed commit | Its `Change-Id` is dropped; the hook mints new ones. The old pull request is orphaned and two new ones open |
| Reinstalling this skill without reinstalling maiao | The stamp in SKILL.md's last line and `git review version` disagree; the skill describes flags the binary may not have |
