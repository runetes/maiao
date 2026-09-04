package cmd

import (
	"context"
	"fmt"
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
	if !gerrit.Installed(gitDir) {
		if prompt.YesNo(hookMissing) {
			err = gerrit.Install(gitDir)
			if err != nil {
				return err
			}
		} else {
			hookPath := lgit.HookPath(gitDir, lgit.CommitMsgHook)
			fmt.Printf(noAutoInstallHookFmt+"\n", filepath.Dir(hookPath), hookPath, gerrit.HookURL())
			return nil
		}
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
