# Goal: reordering commits keeps every pull request open

## Objective (verbatim)

> make sure that the fix allows re-ordering commits in a PR without closing any
> PR. We need to keep the same PR opened and keep the conversation

The fix under test is bce5aae, `fix: park reordered pull requests before pushing
their branches`. The human reports it does not work.

## Success is read on a real forge, not on a fake

bce5aae ships 309 lines of new test in `pkg/maiao/push_test.go` and every one of
them passes. So the unit tests already say the fix works and the human says it
does not: whatever is wrong lives in the gap between maiao's fake provider and
github.com. Every KPI below is therefore read from the GitHub API against
`github.com/runetes/maiao-tests`, with the app key at
`~/maiao-tests.2026-09-26.private-key.pem` (installation `165147049`).

## KPIs

Each is read after a stack that already has open pull requests carrying a comment is
disturbed — reordered, or amended with a `--fixup`.

K1–K5 live in `test/e2e`, behind the `e2e` build tag. It reads them from the forge with
its own HTTP client rather than through maiao's providers: a test that asked the code
under test what happened would only prove it self-consistent.

| # | KPI | Target | How it is read |
|---|-----|--------|----------------|
| K1 | Pull request numbers survive | identical before and after, and exactly one per change | `PullRequestsForHead`, through `stack.only` |
| K2 | No pull request is closed | every `state == "open"` | same call, read `state` |
| K3 | No pull request is reported merged | `merged_at` unset — the field a forge sets even when nothing was merged | same call |
| K4 | The conversation survives | the comment posted beforehand still has its id | `CommentIDs` |
| K5 | Each pull request ends on the right base | base of the change at stack position *i* is the branch of position *i-1* | same call, read `base.ref` |
| K6 | The repository's own tests stay green | `go test ./pkg/...` passes | `go test ./pkg/...` |

K1–K5 failing is the bug. K6 failing means the fix broke something else. `go test ./...`
is deliberately **not** the measure: two of its failures predate this work, and the log
below says which.

## Assumptions

| Assumption | State | Cost if wrong |
|---|---|---|
| A1: GitHub exposes a stacked-pull-request REST API at `/repos/{owner}/{repo}/stacks`, with `/unstack`, as `pkg/api/stack_github.go` and bce5aae's message both claim | **validated: retrieved** `docs.github.com/en/rest/pulls/stacks` — the five endpoints exist, the `pull_request=` filter `GetStack` relies on exists, `X-GitHub-Api-Version: 2026-03-10` matches `stackAPIVersion`, and unstack answers 204 when dissolved or 200 with what is left, both of which `Unstack` handles | Would have been the cause. It is not: the previous session's uncited claim turned out right. |
| A2: maiao can authenticate to GitHub with a GitHub App installation token | **validated: run** — `GITHUB_TOKEN` (`pkg/credentials/env.go:30`) feeds the API client, and with an `https://` origin the same token authenticates the push (`pkg/credentials/auth_method.go:23`) | — |
| A3: installation 165147049 grants contents:write and pull_requests:write | **validated: run** — the access_tokens response reports exactly `contents: write`, `metadata: read`, `pull_requests: write` | — |
| A4: the failure is deterministic and reproducible in one reorder of a three-commit stack | **refuted** — all six reorderings of a 2- and 3-commit stack pass on github.com | The scenario had to be widened, and the report still is not reproduced by reordering a healthy repository. |
| A5: a pull request GitHub auto-closed because its head became reachable from its base can be reopened, so a damaged repository can be recovered | **assumed** | Decides whether the answer for an already-damaged repository is "reuse the pull request" or "there is nothing to reuse, so say so". `scripts/e2e/recover-closed-prs.sh` reads it from GitHub. |

## Infrastructure that must exist

- `github.com/runetes/maiao-tests` — exists, given by the human.
- No new repository, no new app, no new organisation is needed. Nothing outward-facing
  beyond pull requests inside that test repository.

## Milestones

Each is a state the system can be left in.

The plan below is as it was written, with what became of each marked. The shape it got
wrong is called out rather than tidied away.

- **M1 — the harness runs.** A script that mints an installation token from the app
  key, clones `maiao-tests` into the scratchpad, builds three commits, runs
  `git review` through `go run ./cmd/maiao`, and prints the KPI table. Committed under
  `scripts/`, so the next session reruns it instead of rebuilding it.
  → **done, then superseded.** "A script under `scripts/`" was the wrong shape: it is a
  test, and it belongs in the test suite. It is `test/e2e` now, behind the `e2e` build
  tag, covering GitHub and Gitea from one test body. See the log for why the shell
  version had to go rather than merely being joined.
- **M2 — the failure is observed.** KPI table from a real reorder with the current
  `main`, quoted verbatim. This is the evidence that the fix does not work, and the
  exact shape of how it fails.
  → **the premise was wrong.** No reorder of a healthy repository reproduces the report:
  nine of them meet K1–K5. What reproduces it is a repository the *pre-fix* maiao had
  already touched, which is a different observation than this milestone anticipated.
- **M3 — the cause is named.** One sentence tying the observed failure to the code
  path, with the API response that proves it.
  → **done**, and the cause is not in the code: the damage predates the fix and GitHub
  refuses to undo it (422, quoted in the log).
- **M4 — the fix lands.** Atomic commit(s), unit test that fails before and passes
  after, and the M1 harness rerun showing K1–K6 met.
  → **done** for the live defect the investigation turned up — a lookup that could not
  see a closed pull request — across five providers, plus three high-severity defects a
  review of that work then found.
- **M5 — closed.** KPI numbers written into this file, including any that missed.
  → **done.** The numbers are in "Outcome" below.

## Outcome

`go test -tags e2e ./test/e2e/` against both forges in one run, 2026-09-26, on the code
as it stands: **16 scenarios, 8 per forge, all passing** in 480s.

| | GitHub | Gitea |
|---|---|---|
| five reorderings (2-, 3- and 4-commit stacks) | pass | pass |
| a closed pull request is reopened, not duplicated | pass | pass |
| `--fixup` on the bottom change, and one above it | pass | pass |

K1 (numbers survive), K2 (nothing closed), K3 (nothing reported merged), K4 (the
conversation survives) and K5 (each pull request ends behind the change below it) hold in
all sixteen. K6: `go test ./pkg/...` passes.

**What missed.** One thing the objective asked for cannot be delivered, and no code
change will: a pull request that a reorder already closed as merged is unrecoverable.
GitHub answers a reopen with 422, Gitea with 412, GitLab has no transition out of
`merged`. For those, the change gets a new pull request and the review now says on
stderr which conversation it is leaving behind — the honest answer rather than a silent
duplicate. Everything from here on is prevented; nothing from before is repaired.

**Published**, 2026-09-26, as pull requests 23–29 on gitea.jamet.me/runetes/maiao,
stacked behind #21. Both pull requests that existed beforehand — #21, and the orphaned
#22 carrying `docs/investigations/rebase-commit-order.md`, which is stacked on #21 and
in no local branch — were still open and unmerged afterwards, checked against a listing
taken before the push.

**CI has not run on any of them, and cannot.** All seven sit `pending` with
`Go / build (pull_request)` and `Go / build-darwin (pull_request)` queued, because
`runetes/maiao` has **no Actions runner registered**: 14 runs are queued, the oldest
from an earlier session (`docs: record what the rebase-order investigation ruled out`).
A queued run is indistinguishable from a passing one on the pull request page, so the
only evidence these changes work is the local runs written above — `go test ./pkg/...`
and the 16 live scenarios. Nothing here has been through CI.

**Not verified.** Bitbucket and Origin have unit tests only — there is no throwaway
repository on either, and Origin's ordering semantics could not be retrieved at all.
Bitbucket's listing reads one page whose default size could not be retrieved either, so
a head branch carrying more pull requests than one page holds would truncate; one change
per head does not produce that. Both gaps are marked in the code where they bite.

## Subtasks

| Subtask | Agent | Returns |
|---|---|---|
| S1: validate A1 — does the stacks API exist? | this thread (one WebFetch + one curl) | the API's real shape, or proof it does not exist |
| S2: read how maiao authenticates and whether an app installation token works | `Explore` | the credential path by file:line, and the env/config that feeds it a token |
| S3: build the harness (M1) | this thread | the script |
| S4: run and diagnose (M2, M3) | this thread | KPI table + cause |
| S5: fix + test (M4) | this thread, delegating any wide reading | commits |

Bulk reading goes to subagents; the diagnosis and the commits stay here.

## Log

- 2026-09-26: goal file written before exploring. Nothing validated yet.
- 2026-09-26: **M1 reached.** `scripts/e2e/mint-installation-token.sh`,
  `scripts/e2e/reorder-keeps-prs-open.sh` and `scripts/e2e/sweep-reorders.sh` run
  against `runetes/maiao-tests`. Two things the harness needed that the plan did
  not anticipate: maiao refuses to review a repository whose commit message hook is
  missing even when every commit already carries a Change-Id, so the harness runs
  `maiao install` first; and running the sweep and the recovery check at the same
  time earned a TCP timeout against api.github.com, so they run one at a time.
- 2026-09-26: **M2 did not reproduce the report.** All six reorderings of a 2- and
  3-commit stack meet K1–K5 on github.com: same pull request numbers, all open,
  none reported merged, comments intact, and every base pointing at the change
  below it. The native stack path was genuinely exercised — stack 13 registered,
  dissolved on the reorder, re-registered as 14 — so the sweep is not passing by
  skipping it.
- 2026-09-26: **the sweep widened to four commits and still passes.** Three commits
  cannot produce the one case where the parking pass deliberately leaves a pull
  request alone: a change whose base branch keeps its *name* while that branch's
  contents change underneath it. `1 0 2 3` is that case — the bottom two swap, so
  `D` still targets `maiao.C`, which now holds `BAC` instead of `ABC`. `0 2 1 3` and
  `3 2 1 0` went with it. All three meet K1–K5, which makes 9 reorderings with no
  pull request closed.
- 2026-09-26: what the sweep cannot reach, and where the report most likely comes
  from. Two findings:
  1. **Every provider's `Find` looks only at open pull requests**, which is the
     exact gap bce5aae's own message blamed for non-recovery and then did not
     close: `pkg/api/github.go:50` sets no `State` (the API defaults to `open`),
     and `pkg/gitea/base.go:157` sends `state=open` outright, with gitlab,
     bitbucket and origin built the same way. So a repository whose pull requests
     the old bug already closed does not get them back: `Find` reports nothing,
     and a second pull request is opened beside the closed one — losing exactly the
     conversation this objective is about. Whether reuse is even possible depends on
     A5.
  2. **The reorder this repository's own workflow produces is not the one the sweep
     tests.** `origin` here is `gitea.jamet.me` with `maiao.provider gitea`, not
     github.com, and `docs/investigations/rebase-commit-order.md` (on `f8bbb43`,
     never merged to main) records that a `--fixup` commit is what "makes a
     `--fixup` workflow reorder the stack on the remote, which is what the pull
     request closing bug fed on". Reading the rebase path: a fixup is grouped with
     its target and picked directly after it, so the order of *changes* is
     preserved and only their commits are rebuilt — which should be safe. Unverified
     either way: no harness has driven the fixup path or Gitea yet.
- 2026-09-26: **M3 — the cause is named, and it is not in the parking pass.**
  `scripts/e2e/recover-closed-prs.sh` reproduced the report by running the maiao that
  predates bce5aae: it closed #46 and opened #48 for the same change in the same run.
  Handed that repository, the fixed maiao behaved correctly — and the repository is
  still wrong, because #46 is gone for good:

  ```
  GET  /repos/runetes/maiao-tests/pulls/46 -> {"state":"closed","merged":true,
                                               "merged_at":"2026-09-26T13:43:35Z"}
  PATCH /repos/runetes/maiao-tests/pulls/46 {"state":"open"}
  422 Validation Failed
    state cannot be changed. The pull request cannot be reopened.
  ```

  **A5 is refuted.** Nothing recovers a pull request closed this way. So a reorder
  run before the fix leaves permanent duplicates and a stranded conversation, and a
  run after it looks broken in exactly the way the report describes while the code is
  doing the only thing left to it. That is the most likely reading of "the current fix
  is not working": the damage predates the fix and cannot be undone.
- 2026-09-26: **M4 — the one live defect found along the way is fixed.** The lookup
  only ever asked for open pull requests, so a closed one was invisible and the change
  got a second pull request beside it. It now asks for every state and reports which,
  a closed-but-not-merged pull request is reopened and reused rather than duplicated,
  and a merged one is named on stderr instead of silently replaced. GitHub is done;
  the other four providers are in progress.
- 2026-09-26: the end-to-end harness earned its keep by breaking the fix it was
  written to check. `scripts/e2e/reopen-closed-pr.sh` closes a pull request by hand —
  the only way to reach *closed but not merged*, which neither the sweep nor the
  recovery check produces — and the reopen failed on it:

  ```
  PATCH /repos/runetes/maiao-tests/pulls/69 -> 422
    base: Cannot change the base branch because the pull request is part of a stack.
  ```

  Reopening moved the base before reopening, unconditionally, and that pull request's
  base was already right: the write was pointless, and inside one of GitHub's native
  stacks a pointless write is a refusal. Now the base is only moved when it is wrong,
  and a base that has to move and will not leaves the pull request closed with a new
  one opened instead — losing a conversation beats losing the conversation and the
  pull request. All four KPIs pass on github.com: #73 reopened, same number, comment
  intact, base pointing at the change below it.
- 2026-09-26: the other four providers converted, and the interesting part is that
  **no two forges agree on how to reopen**. Gitea applies a state change before a base
  change within one request and refuses a base move while the issue is closed, so
  state and base travel together — the opposite of GitHub. GitLab drops
  `target_branch` from the params entirely while the merge request is still closed, so
  the reopen has to go alone and the base follows in a second request. Bitbucket cannot
  reopen at all: only open pull requests can be mutated and no endpoint exists. Origin
  is the one where the ordering claim could not be retrieved, and is marked unverified
  in the code.

  Two of those claims were checked against the forges' own source rather than taken on
  trust — `routers/api/v1/repo/pull.go:768,782` and `services/pull/pull.go:238` for
  Gitea, `update_service.rb:128` for GitLab — and both held. The Origin ordering quote
  did not: two retrievals of `cursor.com/docs/api/origin` truncated before the Update
  Pull Request section, so the comments that asserted it now say so instead, and every
  provider sends the base only when it actually moved, which keeps the common case off
  the unverified path entirely.
- 2026-09-26: **the shell harness is gone, replaced by `test/e2e` behind the `e2e`
  build tag.** The scripts named earlier in this log did their job — they found the
  report, and they found two bugs in the fix itself — but they are not what this
  belongs in, and a review found their failure reporting untrustworthy: `run_review`
  re-armed `set -e` inside itself, which defeated the caller's `set +e`, so a review
  that failed killed the script before it recorded anything, printed its KPI table, or
  cleaned up. The sweep then reported that permutation as `exit=1 failures=0` — a run
  where nothing was measured, reading as a run with nothing wrong. It had already
  happened once, in the reopen check that caught the base-edit bug. The nine passing
  reorderings are still good evidence (all reported `exit=0`, which only a completed
  KPI pass produces), but a harness that cannot be trusted when it fails is not one to
  keep. The Go tests also drop the two leaks the same review found: the installation
  token in `argv` and in the clone's `.git/config`.
- 2026-09-26: the integration test found its own bug before it found anything else.
  `TestMain` lived in `stack.go`, and `go test` only honours `TestMain` in a `_test.go`
  file — so it was an ordinary function nobody called, the binary path stayed empty, and
  every run died on `install: exec: no command`. The package's files are all `_test.go`
  now. Second thing it taught: the credential. maiao's `GITEA_TOKEN` path sends the
  username `x-token`, and Gitea answers **401** to that whoever the token belongs to —
  only a real account name works, confirmed by probing three usernames against
  gitea.jamet.me. So the tests write a `.netrc` into a HOME of their own instead, which
  also makes a run independent of the machine. **That looks like a live maiao bug in its
  own right** — `GITEA_TOKEN` cannot authenticate a push to Gitea — and is not part of
  this goal.
- 2026-09-26: **the `--fixup` path is verified, and it was the one real gap left.**
  `docs/investigations/rebase-commit-order.md` flagged it as what "makes a `--fixup`
  workflow reorder the stack on the remote, which is what the pull request closing bug
  fed on", and no harness had ever driven it: the others hand maiao an already-reordered
  stack that needs no rebase, so the pull requests are sent from the same process. A
  fixup instead makes maiao drive a real `git rebase -i` whose todo squashes the fixup
  into its target, rebuilding every change above it, and the pull requests are created by
  the maiao that the rebase execs as its last todo step. Reading the code said the order
  of *changes* is preserved so it should be safe; the test now says it is, on both forges,
  for a fixup to the bottom change and to one above it.
- 2026-09-26: **pre-existing, not touched:** `pkg/github`'s `ExampleNewClient` makes a
  live `api.github.com` call for `runetes/maiao` and panics on the nil repository when
  there is no working credential, so `go test ./...` is red in any environment without
  one. `TestLicenses` at the root fails on `pkg.go.dev` rate limits and an MPL-2.0
  dependency. Neither is related to this work.
