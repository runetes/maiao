# Goal: all open Dependabot PRs green and merged

## Objective (verbatim)

Bring agents to rebase, fix and merge all dependabot PRs. All opened PRs have green CI and are fixed. Update their head branch and rebase as required.

## Starting state (2026-10-31)

`origin/main` is 706a771. Nine open PRs, all based on f1d7fac (one commit behind main):

| PR | Bump | Kind |
|----|------|------|
| 86 | golang.org/x/mod 0.41.0 to 0.42.0 | go.mod |
| 85 | github.com/spf13/afero 1.14.0 to 1.15.0 | go.mod |
| 83 | golang.org/x/crypto 0.53.0 to 0.58.0 | go.mod |
| 78 | github.com/sirupsen/logrus 1.9.3 to 1.10.2 | go.mod |
| 84 | actions/download-artifact 4 to 8 | workflow |
| 82 | softprops/action-gh-release 2 to 3 | workflow |
| 76 | actions/upload-artifact 4 to 7 | workflow |
| 73 | googleapis/release-please-action 4 to 5 | workflow |
| 72 | peter-evans/create-pull-request 7 to 8 | workflow |

## Sub-goals and KPIs

| Sub-goal | KPI (read with) |
|----------|-----------------|
| Every PR is on top of current main | `get` on each PR: `mergeable_state` not `behind`/`dirty` |
| Every PR's CI is green | `get_check_runs` on the head SHA: all conclusions `success` |
| Every PR is merged | `list_pull_requests state=open` returns no dependabot PRs |

## Subtasks

1. **Go modules agent**: PRs 86, 85, 83, 78, one at a time (they all edit go.mod/go.sum, so each merge makes the next one behind). Per PR: update branch, wait for CI, fix build/test breakage in the bump's own commit scope, merge.
2. **Actions agent**: PRs 84, 82, 76, 73, 72. The same loop. A major-version bump of an action may need input/output renames in the workflow; read that action's release notes, do not guess.

The two groups touch disjoint files, so they run in parallel.

## Assumptions

- Merge method squash is allowed on the repo: assumed.
- Pushing fixes to `dependabot/*` branches is permitted: assumed (the user asked for it). Dependabot stops rebasing a branch once someone else pushes to it, which is fine since we merge it ourselves.
- Failing CI on a PR may be unrelated to the bump (flaky or already red on main): assumed, to be checked against main's last run.

## Log

(Updated as milestones land.)
