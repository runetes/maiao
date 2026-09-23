# Installing maiao, the hook and credentials

## If `git review` is missing

Invoked with `--install`, or whenever `git review` is not on `PATH`, install maiao
before anything else:

```bash
brew tap runetes/maiao https://github.com/runetes/maiao.git
brew install maiao
```

Elsewhere, take a binary from the [releases page](https://github.com/runetes/maiao/releases)
and put it on `PATH` as `git-review` — the name is what makes git answer to
`git review`.

Then re-run `git review install --skill` to rewrite this skill from the maiao you
just installed. The stamp in SKILL.md's last line says which version wrote it; if
that disagrees with `git review version`, the skill describes a binary you are not
running.

## Setup, once

```bash
git review install --global                 # commit-msg hook in every repo, never asked again
export GITHUB_TOKEN=...                     # or GITLAB_/GITEA_/FORGEJO_/BITBUCKET_/ORIGIN_TOKEN, or ~/.netrc
git config --global maiao.provider gitea    # self-hosted hosts only; known hosts are detected
```

`--global` sets `init.templateDir`, so repositories you create or clone get the
hook, and `maiao.autoInstallHook`, so existing ones get it on the next `git review`
without asking.

## A commit came out with no `Change-Id:`

The hook is not where git looks for it. Check:

```bash
git rev-parse --git-path hooks/commit-msg
```

Hook managers such as husky and lefthook move that path with `core.hooksPath`.
Maiao never replaces a hook it did not write: it installs its own beside theirs and
offers to add one line so both run.

## Installing this skill elsewhere

```bash
git review install --skill                    # every harness detected on this machine
git review install --skill --harness cursor   # only that harness
git review install --skill .agents            # this project, shared by most harnesses
git review install --skill .claude            # this project, Claude Code
```
