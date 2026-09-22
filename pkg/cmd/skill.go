package cmd

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/adevinta/maiao/pkg/version"
	skill "github.com/adevinta/maiao/skills/maiao"
	"github.com/spf13/cobra"
)

// skillName is the directory the skill is installed under, and so the name it is
// invoked by. It matches the name in the skill's own frontmatter, because an
// assistant reads the two from different places and a mismatch is confusing to
// debug.
const skillName = "git-review"

// skillFile is the entry point an assistant looks for inside that directory. The
// companion files beside it are reached only through its links.
const skillFile = "SKILL.md"

// defaultSkillRoot is where assistants read personal skills from.
//
// A home directory that cannot be resolved falls back to the relative path rather
// than failing: an install into ./.claude is visibly wrong and easy to move, where
// a hard failure on a machine with no HOME leaves the user with nothing.
func defaultSkillRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

// installSkillAt writes the embedded skill under root, and returns the path of its
// SKILL.md.
//
// The skill is a directory: SKILL.md carries the model and the four operations and
// links to companion files for the rest, so all of them are written or none of the
// links resolve.
//
// The version is stamped into SKILL.md because nothing else records which maiao a
// skill came from. Upgrading maiao and forgetting to reinstall leaves an older
// skill describing flags and exit statuses this binary may no longer have, and the
// stamp is what makes that visible instead of silent.
func installSkillAt(out io.Writer, root, version string) (string, error) {
	dir := filepath.Join(root, "skills", skillName)
	// The directory is maiao's own, and is replaced rather than written over: a
	// companion file this version renamed or dropped would otherwise survive beside
	// the new skill and be read as part of it.
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	// Walked rather than listed, so a companion added in a subdirectory is installed
	// rather than quietly left out — a link into a directory that was never written
	// fails the same way as a missing file, but with nothing in the output to say so.
	files := 0
	err := fs.WalkDir(skill.GitReview, skill.GitReviewRoot, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(skill.GitReviewRoot, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := fs.ReadFile(skill.GitReview, p)
		if err != nil {
			return err
		}
		if rel == skillFile {
			content = []byte(fmt.Sprintf("%s\n<!-- installed by maiao %s -->\n", content, version))
		}
		files++
		return os.WriteFile(target, content, 0o644)
	})
	if err != nil {
		return "", err
	}

	installed := filepath.Join(dir, skillFile)
	fmt.Fprintf(out, "Installed the %s skill at %s, with %d files\n", skillName, installed, files)
	return installed, nil
}

// installSkill backs install --skill, whose destination is the argument after the
// flag, or the flag's own assigned value.
//
// Both forms are read because pflag only reads one. A flag carrying NoOptDefVal —
// which is what makes a bare --skill mean the default directory — never consumes
// the next argument, so `--skill .claude` set the flag to the default and left
// `.claude` as a positional nobody looked at. The install then reported success
// somewhere the user had not named.
//
// A separate --path was not available: the root command already has one naming the
// repository, and install resolves the commit message hook through it. A second
// meaning for the same word would have made install --path ambiguous.
func installSkill(cmd *cobra.Command, args []string) error {
	root := cmd.Flag("skill").Value.String()
	if len(args) > 0 {
		// Only the assigned form can disagree with the argument; a bare --skill
		// leaves the flag holding NoOptDefVal, which is this same default.
		if root != defaultSkillRoot() {
			return fmt.Errorf("install: two directories given, %q and %q: name the directory once", root, args[0])
		}
		root = args[0]
	}
	if root == "" {
		root = defaultSkillRoot()
	}
	_, err := installSkillAt(cmd.OutOrStdout(), root, version.Version)
	return err
}
