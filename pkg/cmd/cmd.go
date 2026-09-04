package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/99designs/keyring"
	"github.com/adevinta/maiao/pkg/git"
	"github.com/adevinta/maiao/pkg/log"
	"github.com/adevinta/maiao/pkg/prompt"
	mssh "github.com/adevinta/maiao/pkg/ssh"
	"github.com/adevinta/maiao/pkg/version"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	changeIDEditorHelp = `Reproduces git interactions a human should do to add Change-Ids to commits
	Used as 'env GIT_EDITOR="git review add-change-id-editor" git rebase -i origin/master'
	This command selects changes all pickups to rewords and keeps the message intact for the
	Change-Id hook to be ran
	`
)

// NewCommand implements a new cobra command to run git-review
func NewCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use: "git-review [<targetBranch>]",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			switch cmd.Flag("verbose").Value.String() {
			case "0":
				log.Logger.SetLevel(logrus.FatalLevel)
			case "1":
				log.Logger.SetLevel(logrus.ErrorLevel)
			case "2":
				log.Logger.SetLevel(logrus.WarnLevel)
			case "3":
				log.Logger.SetLevel(logrus.InfoLevel)
			case "4":
				keyring.Debug = true
				log.Logger.SetLevel(logrus.DebugLevel)
			case "5":
				keyring.Debug = true
				log.Logger.SetLevel(logrus.TraceLevel)
			default:
				return fmt.Errorf("unexpected log level %s expecting 0-5", cmd.Flag("verbose").Value.String())
			}
			prompt.SetBatch(cmd.Flag("batch").Value.String() == "true")
			mssh.SetTrustNewHosts(cmd.Flag("trust-new-ssh-hosts").Value.String() == "true")
			// From here on a failure is a runtime one, and the flag list says nothing
			// useful about it. Printing it buried the message that did, which matters
			// most where that message is the entire diagnostic: batch mode.
			//
			// Set here rather than on the command, so a genuine misuse — an unknown flag,
			// too many arguments, a bad verbosity — is still answered with usage. Cobra
			// validates arguments before this runs.
			cmd.Root().SilenceUsage = true
			return nil
		},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return errors.New("too many arguments provided")
			}
			return nil
		},
		Version: version.Version,
	}

	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		maiaoArgs := os.Getenv(git.RebaseArgsEnvVar)
		if maiaoArgs != "" {
			os.Unsetenv(git.RebaseArgsEnvVar)
			originalArgs := []string{}
			err := json.Unmarshal([]byte(maiaoArgs), &originalArgs)
			if err != nil {
				return err
			}
			err = rootCmd.ParseFlags(originalArgs)
			if err != nil {
				return err
			}
			err = rootCmd.Execute()
			if err != nil {
				return err
			}
			return nil
		}
		return review(cmd, args)
	}

	rootCmd.PersistentFlags().IntP("verbose", "v", 0, "the logging verbosity (0-5)")
	rootCmd.PersistentFlags().StringP("path", "C", ".", "Path of the repository to push reviews")
	rootCmd.PersistentFlags().BoolP("no-rebase", "R", false, "Don't rebase changes before submitting")
	rootCmd.PersistentFlags().StringP("topic", "t", "", "Topic to submit branch to")
	rootCmd.PersistentFlags().Bool("debug", false, "Run the command in debug mode")
	rootCmd.PersistentFlags().String("remote", "", "Specifies the remote the review should be done on. By default the tracking remote of the target branch is used")
	rootCmd.PersistentFlags().BoolP("work-in-progress", "w", false, "Mark the review as work in progress, or draft in compatible remotes. This flag is exclusively effective when creating Pull Requests")
	rootCmd.PersistentFlags().BoolP("ready", "W", false, "Mark the review as ready in compatible remotes (i.e. removing the work in progress or draft flag)")
	rootCmd.PersistentFlags().Bool("batch", prompt.Batch(), "Never prompt, and fail with what to configure instead. Defaults to true when stdin is not a terminal")
	rootCmd.PersistentFlags().Bool("trust-new-ssh-hosts", mssh.TrustNewHosts(), "Accept the SSH key of a host missing from known_hosts without asking. Never applies to a key mismatch. Also settable with "+mssh.TrustNewHostsEnvVar)
	rootCmd.PersistentFlags().Bool("json", false, `Describe the reviewed changes as JSON on stdout. Diagnostics stay on stderr. A failed review still reports the changes it submitted, plus an "error" object naming the failure`)
	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Installs commit message hook to the repository",
		Long: `Installs commit message hook to the repository.

With --global, installs it for every repository instead: new and cloned
repositories get it from git through init.templateDir, and existing ones get it
the first time you run git review in them, without being asked.

A commit message hook installed by something else, such as husky or lefthook, is
never replaced. Maiao installs its own beside it and offers to add one line to
the existing hook so that both run.`,
		RunE: install,
	}
	installCmd.Flags().Bool("global", false, "Install the hook for every repository rather than only this one")
	installCmd.Flags().Bool("force", false, "Replace a commit message hook installed by something else, rather than keeping it and running maiao's as well")
	rootCmd.AddCommand(
		installCmd,
		&cobra.Command{
			Use:   "version",
			Short: "Installs commit message hook to the repository",
			Long:  `Installs commit message hook to the repository`,
			Run: func(cmd *cobra.Command, args []string) {
				fmt.Println(version.Version)
			},
		},
		&cobra.Command{
			Use:    "add-change-id-editor",
			Short:  "Handles rebase interactive file edition",
			Long:   changeIDEditorHelp,
			RunE:   rebaseEditor,
			Hidden: true,
		},
	)
	rootCmd.AddCommand()
	return rootCmd
}
