package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/spf13/cobra"
	"github.com/adevinta/maiao/pkg/gerrit"
	lgit "github.com/adevinta/maiao/pkg/git"
	"github.com/adevinta/maiao/pkg/maiao"
	"github.com/adevinta/maiao/pkg/prompt"
)

const (
	hookMissing = "commit message hook is missing, do you want to install it automatically?"
	// noAutoInstallHookFmt takes the hooks directory, the resolved hook path and the
	// hook download URL. The path is resolved through git.HookPath so the command
	// works in worktrees, where .git is a file pointing at the common git dir.
	noAutoInstallHookFmt = "You are missing change ids in your commits. \nPlease install the commit hook by running\n`mkdir -p %[1]s && curl -o %[2]s %[3]s && chmod +x %[2]s`"
)

func review(cmd *cobra.Command, args []string) error {
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
		return err
	}
	branch := ""
	if len(args) > 0 {
		branch = args[0]
	}
	gitDir, err := lgit.FindGitDir(path)
	if err != nil {
		return err
	}
	proceed, err := ensureCommitMsgHook(path, gitDir)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
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
	if gerrit.Installed(gitDir) {
		return true, nil
	}
	hookPath := lgit.HookPath(gitDir, lgit.CommitMsgHook)
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
			fmt.Fprintf(os.Stderr, noAutoInstallHookFmt+"\n", filepath.Dir(hookPath), hookPath, gerrit.HookURL())
			return false, nil
		}
	}
	if err := installHook(hookPath); err != nil {
		return false, err
	}
	return true, nil
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
