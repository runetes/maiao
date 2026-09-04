package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/adevinta/maiao/pkg/gerrit"
	lgit "github.com/adevinta/maiao/pkg/git"
	"github.com/adevinta/maiao/pkg/prompt"
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
	force := cmd.Flag("force").Value.String() == "true"
	if cmd.Flag("global").Value.String() == "true" {
		return installGlobally(cmd.OutOrStdout(), force)
	}
	gitDir, err := lgit.FindGitDir(cmd.Flag("path").Value.String())
	if err != nil {
		return err
	}
	_, err = ensureHook(cmd.OutOrStdout(), lgit.HookPath(gitDir, lgit.CommitMsgHook), force)
	return err
}

// ensureHook installs the commit message hook at path, and returns where it ended
// up, which is empty when it was not installed at all.
//
// A hook maiao did not write is never replaced. Hook managers such as husky,
// lefthook and pre-commit install their own commit message hook, and maiao
// resolves to the same path they do, so overwriting would silently take away
// whatever the repository's own hook did — commit linting, most often. Both hooks
// are wanted, so maiao's is written beside theirs and theirs is made to call it.
//
// Editing a file maiao did not write is asked about even when the user has opted
// in to installing without being asked: opting into an install is not opting into
// having another tool's hook rewritten. With nobody to ask, the line to add is
// printed instead, which is the same bargain batch mode makes everywhere else.
//
// force replaces the foreign hook instead, for the user who wants maiao's and only
// maiao's.
func ensureHook(out io.Writer, path string, force bool) (string, error) {
	if force || gerrit.StateAt(path) != gerrit.ForeignHook {
		if err := installHook(path); err != nil {
			return "", err
		}
		return path, nil
	}
	// Written first, whatever is decided next: it adds a file rather than changing
	// one, and every route from here — automatic or by hand — needs the hook to be
	// there in order to be called.
	beside := filepath.Join(filepath.Dir(path), gerrit.ChainedHookName)
	if err := installHook(beside); err != nil {
		return "", err
	}
	call := gerrit.ChainCall(gerrit.ChainedHookName)
	switch {
	case !gerrit.Chainable(path):
		return "", fmt.Errorf(`%s was installed by something else and is not a shell script, so maiao cannot extend it.
Maiao's hook is at %s. Make %s run it with the message file as its argument, or
replace %s with `+"`git review install --force`", path, beside, path, path)
	case prompt.Batch():
		return "", fmt.Errorf(`%w: %s was installed by something else.
Maiao's hook is at %s. Add this line to %s:
%s
or replace %s with `+"`git review install --force`", prompt.ErrNoInput, path, beside, path, call, path)
	case !confirm(fmt.Sprintf("%s was installed by something else. Add a line to it that also runs maiao's hook?", path)):
		fmt.Fprintf(out, "Left %s as it was. Maiao's hook is at %s; add this line to run it:\n%s\n", path, beside, call)
		return "", nil
	}
	if err := gerrit.ChainTo(path, gerrit.ChainedHookName); err != nil {
		return "", err
	}
	fmt.Fprintf(out, "Installed the commit message hook at %s, and added a call to it to %s\n", beside, path)
	return beside, nil
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
func installGlobally(out io.Writer, force bool) error {
	dir, maiaoOwned, err := templateDir()
	if err != nil {
		return err
	}
	hookPath := filepath.Join(dir, "hooks", string(lgit.CommitMsgHook))
	// A template directory of the user's own may already contain a commit message
	// hook, and it reaches every repository they create from now on, so replacing
	// it is the same mistake as replacing a repository's own — multiplied.
	installedAt, err := ensureHook(out, hookPath, force)
	if err != nil {
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

	// Chaining already said where the hook went, and to what.
	if installedAt == hookPath {
		fmt.Fprintf(out, "Installed the commit message hook at %s\n", hookPath)
	}
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
