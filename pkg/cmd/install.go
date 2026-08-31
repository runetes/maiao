package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/adevinta/maiao/pkg/gerrit"
	lgit "github.com/adevinta/maiao/pkg/git"
	"github.com/spf13/cobra"
)

const (
	// autoInstallHookOption, when true, makes review install the commit message
	// hook itself rather than asking first.
	autoInstallHookOption = "maiao.autoInstallHook"
	templateDirOption     = "init.templateDir"
)

// installHook downloads and writes the commit message hook at a given path. It
// is a variable so that tests can install a local hook rather than reaching out
// to the network.
var installHook = gerrit.InstallAt

func install(cmd *cobra.Command, args []string) error {
	if cmd.Flag("global").Value.String() == "true" {
		return installGlobally(cmd.OutOrStdout())
	}
	gitDir, err := lgit.FindGitDir(cmd.Flag("path").Value.String())
	if err != nil {
		return err
	}
	return installHook(lgit.HookPath(gitDir, lgit.CommitMsgHook))
}

// installGlobally makes the commit message hook apply to repositories the user
// never runs `git review install` in.
//
// Two settings are needed because they cover different repositories:
//
//   - init.templateDir points git at a directory whose contents git copies into
//     every repository it creates or clones from then on. This is additive: it
//     does not redirect the hooks directory, so repository local hooks and tools
//     such as husky keep working. It has no effect on repositories that already
//     exist.
//   - maiao.autoInstallHook tells review to install the hook itself instead of
//     asking, which covers those existing repositories.
//
// Setting core.hooksPath globally would be shorter and is deliberately not done:
// it replaces every repository's hooks directory, so a repository that ships its
// own hooks would silently stop running them, machine wide.
func installGlobally(out io.Writer) error {
	dir, maiaoOwned, err := templateDir()
	if err != nil {
		return err
	}
	hookPath := filepath.Join(dir, "hooks", string(lgit.CommitMsgHook))
	if err := installHook(hookPath); err != nil {
		return err
	}
	if maiaoOwned {
		if err := lgit.ConfigSetGlobal(templateDirOption, dir); err != nil {
			return err
		}
	}
	if err := lgit.ConfigSetGlobal(autoInstallHookOption, "true"); err != nil {
		return err
	}

	fmt.Fprintf(out, "Installed the commit message hook at %s\n", hookPath)
	if maiaoOwned {
		fmt.Fprintf(out, "Set %s so that new and cloned repositories get it from git.\n", templateDirOption)
	} else {
		fmt.Fprintf(out, "Added it to your existing %s, which is left pointing at %s.\n", templateDirOption, dir)
	}
	fmt.Fprintf(out, "Set %s so that existing repositories get it without asking.\n", autoInstallHookOption)
	return nil
}

// templateDir returns the directory to install the template hook into, and
// whether maiao owns that directory.
//
// A template directory the user already configured is reused rather than
// replaced. Pointing init.templateDir somewhere else would silently stop their
// own template from being applied to new repositories.
func templateDir() (string, bool, error) {
	if existing, ok := lgit.ConfigPath("", templateDirOption); ok && existing != "" {
		return existing, false, nil
	}
	config, err := configHome()
	if err != nil {
		return "", false, err
	}
	return filepath.Join(config, "maiao", "template"), true, nil
}

// configHome returns the base directory for maiao's own files.
//
// It follows git's convention rather than the platform's: git reads its user
// configuration from $XDG_CONFIG_HOME/git, or ~/.config/git, on every platform
// including macOS. A git template directory belongs next to that rather than
// under ~/Library/Application Support, which os.UserConfigDir would return and
// which puts a space in a path that ends up in git configuration.
func configHome() (string, error) {
	// The XDG specification requires an absolute path and says to ignore the
	// variable otherwise, which is also what git does.
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}
