# Unattended runs: CI and agents

`git review` never prompts when stdin is not a terminal; force it either way with
`--batch`. `--json` prints the result to stdout and keeps diagnostics on stderr.

| Status | Meaning | What to do |
|---|---|---|
| 0 | The review completed | Nothing. Zero changes is also success |
| 1 | Any other failure | Read stderr |
| 2 | No usable credentials, or the host rejected them | Supply a token. Retrying changes nothing |
| 3 | The rebase stopped before the reviews were created | Resolve, then `git rebase --continue` — **that** creates them. Do not re-run `git review` |
| 4 | An answer was needed and there was no terminal | Apply the setting the message names |
| 5 | The host presented a different SSH key than the one on record | Stop. A changed key is indistinguishable from interception |

A failed run still reports the pull requests it managed to create, so a caller can
tell a resume from a fresh start.

Pair `--batch` with `git review install --global`, a token in the environment and —
on a self-hosted host — `git config --global maiao.provider <forge>`, so no run
ever stops to ask a question the caller cannot answer.
