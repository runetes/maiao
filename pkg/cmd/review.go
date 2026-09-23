package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/spf13/cobra"
	"github.com/runetes/maiao/pkg/gerrit"
	lgit "github.com/runetes/maiao/pkg/git"
	"github.com/runetes/maiao/pkg/maiao"
	"github.com/runetes/maiao/pkg/prompt"
)

const (
	hookMissing      = "commit message hook is missing, do you want to install it automatically?"
	hookOutdated     = "commit message hook is outdated, do you want to update it?"
	noAutoInstallFmt = "You are missing change ids in your commits.\nPlease install the commit hook by running `git review install`"
)

func review(cmd *cobra.Command, args []string) error {
	jsonOutput := cmd.Flag("json").Value.String() == "true"
	// Before anything that can fail, so that every failure is reported the same way,
	// and before Review, which is what starts the rebase whose final step re-runs
	// maiao — that run finds the handoff through the environment.
	handoff, err := newResultHandoff(jsonOutput)
	if err != nil {
		return err
	}
	defer handoff.cleanup()
	result, err := runReview(cmd, args)
	return emitResult(os.Stdout, handoff, jsonOutput, result, err)
}

// runReview does the review itself, leaving its caller to report the outcome.
//
// Split out so that reporting is on one path rather than at every return: a failure
// that goes unreported is one a caller reading stdout cannot see at all.
func runReview(cmd *cobra.Command, args []string) (*maiao.Result, error) {
	path := cmd.Flag("path").Value.String()
	repo, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{
		DetectDotGit: true,
		// A linked worktree's .git points at .git/worktrees/<name>, which holds
		// HEAD but keeps refs and config in the common dir named beside it.
		// Without this, resolving what HEAD points at fails with
		// "reference not found".
		EnableDotGitCommonDir: true,
	})
	if err != nil {
		return nil, err
	}
	branch := ""
	if len(args) > 0 {
		branch = args[0]
	}
	gitDir, err := lgit.FindGitDir(path)
	if err != nil {
		return nil, err
	}
	proceed, err := ensureCommitMsgHook(path, gitDir)
	if err != nil {
		return nil, err
	}
	// A declined install is a decision, not a failure, so it still reports a
	// result: an empty one, rather than nothing at all for a caller to parse.
	if !proceed {
		return &maiao.Result{}, nil
	}
	return maiao.Review(context.Background(), repo, maiao.ReviewOptions{
		RepoPath:       path,
		Remote:         cmd.Flag("remote").Value.String(),
		SkipRebase:     cmd.Flag("no-rebase").Value.String() != "false",
		Topic:          cmd.Flag("topic").Value.String(),
		Branch:         branch,
		WorkInProgress: cmd.Flag("work-in-progress").Value.String() != "false",
		Ready:          cmd.Flag("ready").Value.String() != "false",
		Stack:          stackOption(repo),
	})
}

// confirm is a seam so that tests do not need a terminal.
var confirm = prompt.YesNo

// ensureCommitMsgHook makes sure the commit message hook is installed, and
// reports whether the review should go ahead.
//
// Without the hook, commits get no Change-Id and maiao has nothing to track
// reviews by, so declining to install it stops the review rather than failing
// later with something harder to act on.
func ensureCommitMsgHook(repoPath, gitDir string) (bool, error) {
	hookPath := lgit.HookPath(gitDir, lgit.CommitMsgHook)
	switch gerrit.StateAt(hookPath) {
	case gerrit.MaiaoHook:
		offerHookUpdate(hookPath)
		return true, nil
	case gerrit.ForeignHook:
		// Not the same question as a missing hook, and not one the auto install
		// option answers: the hook is there, it belongs to something else, and
		// keeping it working means editing it rather than installing over it.
		installedAt, err := ensureHook(os.Stderr, hookPath, false)
		return installedAt != "", err
	}
	// Users who opted in globally are not asked again, which is what makes maiao
	// usable from a script or an agent working across many repositories.
	if !lgit.ConfigBool(repoPath, autoInstallHookOption) {
		// With nobody to answer, the question cannot be treated as a "no". Doing so
		// would end the run reporting success while no review had been created,
		// which is indistinguishable from having nothing to review.
		if prompt.Batch() {
			return false, fmt.Errorf(`%w: the commit message hook is not installed.
Run `+"`git review install --global`"+` to install it here and in every repository
from now on, or `+"`git review install`"+` for this one only.
Expected at %s`, prompt.ErrNoInput, hookPath)
		}
		if !confirm(hookMissing) {
			fmt.Fprintln(os.Stderr, noAutoInstallFmt)
			return false, nil
		}
	}
	if err := installHook(hookPath); err != nil {
		return false, err
	}
	return true, nil
}

// offerHookUpdate silently checks whether the installed hook matches the
// version embedded in this binary and offers to update it when it does not.
//
// A stale hook still works — it just misses fixes and improvements from
// upstream. Declining is fine, so the review always proceeds.
func offerHookUpdate(hookPath string) {
	// In a chained setup the actual maiao hook lives beside the foreign one.
	target := hookPath
	chainedPath := filepath.Join(filepath.Dir(hookPath), gerrit.ChainedHookName)
	if gerrit.CurrentAt(chainedPath) {
		return
	}
	if !gerrit.CurrentAt(hookPath) {
		// Decide which file to update: if the chained hook exists, that is ours.
		if gerrit.StateAt(chainedPath) == gerrit.MaiaoHook {
			target = chainedPath
		}
		if prompt.Batch() || !confirm(hookOutdated) {
			return
		}
		_ = installHook(target)
	}
}

func stackOption(repo *git.Repository) string {
	cfg, err := repo.Config()
	if err != nil || cfg.Raw == nil {
		return "auto"
	}
	v := cfg.Raw.Section("maiao").Option("useNativeStack")
	switch v {
	case "true", "false":
		return v
	default:
		return "auto"
	}
}
