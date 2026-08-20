# Maiao

![](https://github.com/runetes/maiao/actions/workflows/go.yml/badge.svg)
![License](https://img.shields.io/github/license/runetes/maiao)
![GitHub all releases downloads](https://img.shields.io/github/downloads/runetes/maiao/total)

> **Note:** This is a community fork of [adevinta/maiao](https://github.com/adevinta/maiao). The original maintainers are no longer at Adevinta and the upstream repository is no longer actively maintained. This fork continues development under [runetes/maiao](https://github.com/runetes/maiao).

**Gerrit-style code review workflow for GitHub**

Maiao brings the power of **stacked pull requests** to GitHub, enabling you to break large features into small, reviewable commits where each commit becomes its own PR.

## What is Maiao?

Maiao provides the `git review` command that:

- **Creates one PR per commit** in your branch
- **Stacks PRs automatically** with proper parent-child dependencies
- **Registers native GitHub Stacks** when available, for first-class UI support
- **Manages fixups elegantly** using `git commit --fixup`
- **Tracks commits via Change-IDs** (using the Gerrit commit-msg hook)
- **Auto-rebases your stack** when PRs get merged

## Quick Example

```bash
# Make multiple commits
git commit -m "Add user authentication"
git commit -m "Add authorization middleware"
git commit -m "Add admin endpoints"

# Create stacked PRs for all commits
git review
```

**Result**: Three GitHub PRs created and registered as a native stack:

- PR #1: `Add user authentication` → `main`
- PR #2: `Add authorization middleware` → PR #1
- PR #3: `Add admin endpoints` → PR #2

## Key Benefits

- **Granular Reviews**: Each commit reviewed independently for faster, focused feedback
- **Clear History**: One logical change per PR maintains clean git history
- **Easy Fixups**: Address review feedback with `git commit --fixup <sha>`
- **Automatic Stacking**: Tool manages PR dependencies automatically
- **Native GitHub Stacks**: Integrates with GitHub's stack UI for unified review and merge controls
- **Merge Detection**: Stack updates automatically when PRs merge
- **Rebase Integration**: Handles upstream changes gracefully

## Native GitHub Stacks

Maiao treats [GitHub native stacks](https://github.blog/changelog/2024-10-25-github-stacked-pull-requests/) as a progressive enhancement. When two or more PRs are pushed, Maiao probes the Stacks API (cached for 24 hours) and registers the PRs as a stack if supported. On older GitHub Enterprise instances the feature is silently skipped — branch-based stacking still works as before.

```bash
# Control via git config (default: auto)
git config maiao.useNativeStack auto   # use when available, skip otherwise
git config maiao.useNativeStack true   # always register; warn if unavailable
git config maiao.useNativeStack false  # disable entirely
```

## Documentation

- **[Getting Started](getting-started.md)** - Installation and workflow guide
- **[How Does It Work](how-does-it-work.md)** - Technical details and architecture
- **[Pricing](pricing.md)** - Free and open source

## Learn About Stacked Diffs

Maiao implements the **stacked diffs** methodology. Learn more:

- **[Stacked Diffs Versus Pull Requests](https://jg.gg/2018/09/29/stacked-diffs-versus-pull-requests/)** - Jackson Gabbard's foundational article on the philosophy
- **[Graphite's Guide to Stacked Diffs](https://graphite.dev/guides/stacked-diffs)** - Comprehensive guide to the workflow and best practices
- **[The Pragmatic Engineer: Stacked Diffs](https://newsletter.pragmaticengineer.com/p/stacked-diffs)** - Industry analysis and adoption patterns (paywalled)

## Why "Maiao"?

As Maiao encourages users to create smaller and nicer commits in their pull requests, it has been given the name of a tiny island:

![](img/inspired.jpg)

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](../CONTRIBUTING.md) for details.

## License

MIT License - see [DISCLAIMER.md](../DISCLAIMER.md) for details.
