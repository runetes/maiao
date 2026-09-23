package cmd

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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

// skillDirAuto is the value of a bare --skill. pflag only treats the flag as
// optional when NoOptDefVal is set, and that value has to be distinguishable
// from a directory the user actually typed: "auto" is not a path anyone would
// pass, and a directory of that name can still be given as ./auto.
const skillDirAuto = "auto"

// skillHarness is one coding agent and the directories it reads skills from.
//
// global is the parent of the personal skills directory. project is that parent
// inside a repository, relative to the repository root. The install always
// appends skills/<name>, which is the layout every one of these harnesses scans.
//
// Paths follow the directory each harness documents, which is also where the
// Agent Skills installer writes. A harness missing from this list is still
// reachable: --skill takes the directory itself.
type skillHarness struct {
	name    string
	project string
	global  func() string
	detect  func() (bool, error)
}

// skillHarnesses is kept alphabetical. The help text and the unknown-harness
// error list names in this order, and a bare install prints the harnesses it
// detected in it too. --harness prints them in the order they were named.
var skillHarnesses = []skillHarness{
	{name: "agents", project: ".agents", global: func() string { return underHome(".agents") }, detect: exists(underHome, ".agents")},
	{name: "claude", project: ".claude", global: claudeSkillRoot, detect: existsFunc(claudeSkillRoot)},
	{name: "cline", project: ".agents", global: func() string { return underHome(".agents") }, detect: exists(underHome, ".cline")},
	{name: "codex", project: ".agents", global: codexSkillRoot, detect: existsFunc(codexSkillRoot)},
	{name: "copilot", project: ".agents", global: func() string { return underHome(".copilot") }, detect: exists(underHome, ".copilot")},
	{name: "cursor", project: ".agents", global: func() string { return underHome(".cursor") }, detect: exists(underHome, ".cursor")},
	// Droid reads the shared agents directory. ~/.factory is only where older
	// installs were written, so detecting it still installs into .agents.
	{name: "droid", project: ".agents", global: func() string { return underHome(".agents") }, detect: exists(underHome, ".factory")},
	{name: "gemini", project: ".agents", global: func() string { return underHome(".gemini") }, detect: exists(underHome, ".gemini")},
	{name: "goose", project: ".goose", global: func() string { return configSkillRoot("goose") }, detect: existsFunc(func() string { return configSkillRoot("goose") })},
	{name: "kilo", project: ".agents", global: func() string { return underHome(".kilo") }, detect: existsAny(underHome, ".kilo", ".kilocode")},
	{name: "opencode", project: ".agents", global: func() string { return configSkillRoot("opencode") }, detect: existsFunc(func() string { return configSkillRoot("opencode") })},
	{name: "pi", project: ".pi", global: func() string { return underHome(filepath.Join(".pi", "agent")) }, detect: exists(underHome, filepath.Join(".pi", "agent"))},
	{name: "windsurf", project: ".windsurf", global: func() string { return underHome(filepath.Join(".codeium", "windsurf")) }, detect: exists(underHome, filepath.Join(".codeium", "windsurf"))},
}

// underHome joins rel onto the user's home directory.
//
// A home directory that cannot be resolved falls back to the relative path rather
// than failing: an install into ./.claude is visibly wrong and easy to move, where
// a hard failure on a machine with no HOME leaves the user with nothing.
func underHome(rel string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return rel
	}
	return filepath.Join(home, rel)
}

// claudeSkillRoot is Claude Code's config directory. CLAUDE_CONFIG_DIR relocates
// it; installing into ~/.claude anyway would write a skill Claude is no longer
// reading.
func claudeSkillRoot() string {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		return dir
	}
	return underHome(".claude")
}

// codexSkillRoot is Codex's home. CODEX_HOME is the documented override, and the
// skills directory lives inside it.
func codexSkillRoot() string {
	if dir := strings.TrimSpace(os.Getenv("CODEX_HOME")); dir != "" {
		return dir
	}
	return underHome(".codex")
}

// configSkillRoot is a directory under XDG_CONFIG_HOME, which OpenCode and Goose
// both honor. Unset, that is ~/.config.
func configSkillRoot(name string) string {
	base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if base == "" {
		base = underHome(".config")
	}
	return filepath.Join(base, name)
}

func exists(root func(string) string, rel string) func() (bool, error) {
	return func() (bool, error) {
		return dirExists(root(rel))
	}
}

func existsAny(root func(string) string, rels ...string) func() (bool, error) {
	return func() (bool, error) {
		for _, rel := range rels {
			ok, err := dirExists(root(rel))
			if err != nil || ok {
				return ok, err
			}
		}
		return false, nil
	}
}

func existsFunc(dir func() string) func() (bool, error) {
	return func() (bool, error) {
		return dirExists(dir())
	}
}

func dirExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func harnessNames() []string {
	names := make([]string, len(skillHarnesses))
	for i, h := range skillHarnesses {
		names[i] = h.name
	}
	return names
}

func harnessByName(name string) (skillHarness, bool) {
	for _, h := range skillHarnesses {
		if h.name == name {
			return h, true
		}
	}
	return skillHarness{}, false
}

// resolveHarnesses turns the names given to --harness into harnesses.
//
// "all" means every harness, not every one detected: a script that names it
// wants the skill in place before the harness is installed, and detection would
// quietly skip a machine that does not have it yet.
func resolveHarnesses(names []string) ([]skillHarness, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("install: --harness needs a name. Known harnesses: %s, or all", strings.Join(harnessNames(), ", "))
	}
	seen := map[string]bool{}
	var out []skillHarness
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			return nil, fmt.Errorf("install: --harness needs a name. Known harnesses: %s, or all", strings.Join(harnessNames(), ", "))
		}
		if name == "all" {
			return append([]skillHarness(nil), skillHarnesses...), nil
		}
		if seen[name] {
			continue
		}
		h, ok := harnessByName(name)
		if !ok {
			return nil, fmt.Errorf("install: unknown harness %q. Known harnesses: %s, or all", name, strings.Join(harnessNames(), ", "))
		}
		seen[name] = true
		out = append(out, h)
	}
	return out, nil
}

// detectedHarnesses is the set a bare --skill installs for.
//
// Nothing detected falls back to the shared agents directory rather than to
// Claude's. ~/.agents/skills is the directory Cursor, Codex, Copilot, OpenCode
// and the other shared harnesses have in common; a Claude install is still
// selected when Claude's own directory is present.
func detectedHarnesses() ([]skillHarness, error) {
	var out []skillHarness
	for _, h := range skillHarnesses {
		ok, err := h.detect()
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, h)
		}
	}
	if len(out) == 0 {
		h, _ := harnessByName("agents")
		out = append(out, h)
	}
	return out, nil
}

// skillDest is one directory to write, and every harness that reads it. Harnesses
// that share a directory are installed once: a second copy would be a second
// skill of the same name, which some harnesses then offer twice.
type skillDest struct {
	root  string
	names []string
}

func destinations(harnesses []skillHarness, projectBase string) []skillDest {
	order := make([]string, 0, len(harnesses))
	byRoot := map[string]*skillDest{}
	for _, h := range harnesses {
		root := h.global()
		if projectBase != "" {
			root = filepath.Join(projectBase, h.project)
		}
		root = filepath.Clean(root)
		dest, ok := byRoot[root]
		if !ok {
			dest = &skillDest{root: root}
			byRoot[root] = dest
			order = append(order, root)
		}
		dest.names = append(dest.names, h.name)
	}
	out := make([]skillDest, 0, len(order))
	for _, root := range order {
		out = append(out, *byRoot[root])
	}
	return out
}

// skillDirectory is the directory after --skill, when the user gave one.
//
// Both forms are read because pflag only reads one. A flag carrying NoOptDefVal ?
// which is what makes a bare --skill mean the detected harnesses ? never consumes
// the next argument, so `--skill .claude` set the flag to the default and left
// `.claude` as a positional nobody looked at. The install then reported success
// somewhere the user had not named.
func skillDirectory(cmd *cobra.Command, args []string) (string, bool, error) {
	root := cmd.Flag("skill").Value.String()
	explicit := root != skillDirAuto
	if len(args) > 0 {
		if explicit {
			return "", false, fmt.Errorf("install: two directories given, %q and %q: name the directory once", root, args[0])
		}
		return args[0], true, nil
	}
	if explicit {
		return root, true, nil
	}
	return "", false, nil
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
//
// names, when set, are the harnesses this directory serves. They are printed so a
// multi-harness install says which agent each path is for; a directory the user
// named has no harness, and the line stays as it was.
func installSkillAt(out io.Writer, root, version string, names []string) (string, error) {
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
	// rather than quietly left out ? a link into a directory that was never written
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
	if len(names) == 0 {
		fmt.Fprintf(out, "Installed the %s skill at %s, with %d files\n", skillName, installed, files)
	} else {
		fmt.Fprintf(out, "Installed the %s skill for %s at %s, with %d files\n", skillName, strings.Join(names, ", "), installed, files)
	}
	return installed, nil
}

// installSkill backs install --skill.
//
// A directory on its own is that directory: the user named a place, and guessing
// a harness on top of it would write somewhere else as well. A harness with a
// directory installs into the project layout of those harnesses, under that
// directory. Neither is the personal install, which is what a bare --skill does,
// for every harness detected on the machine or the ones --harness names.
//
// A separate --path was not available: the root command already has one naming the
// repository, and install resolves the commit message hook through it. A second
// meaning for the same word would have made install --path ambiguous.
func installSkill(cmd *cobra.Command, args []string) error {
	dir, hasDir, err := skillDirectory(cmd, args)
	if err != nil {
		return err
	}
	var harnesses []skillHarness
	if cmd.Flags().Changed("harness") {
		names, err := cmd.Flags().GetStringSlice("harness")
		if err != nil {
			return err
		}
		harnesses, err = resolveHarnesses(names)
		if err != nil {
			return err
		}
	}
	if harnesses == nil && !hasDir {
		harnesses, err = detectedHarnesses()
		if err != nil {
			return err
		}
	}
	if harnesses == nil {
		_, err = installSkillAt(cmd.OutOrStdout(), dir, version.Version, nil)
		return err
	}
	projectBase := ""
	if hasDir {
		projectBase = dir
	}
	for _, dest := range destinations(harnesses, projectBase) {
		if _, err := installSkillAt(cmd.OutOrStdout(), dest.root, version.Version, dest.names); err != nil {
			return err
		}
	}
	return nil
}
