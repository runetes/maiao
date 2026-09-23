package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolateSkillHome points HOME at an empty directory and clears the variables
// that relocate a harness, so a test sees only the harnesses it creates.
func isolateSkillHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	return home
}

// TestInstallSkillWritesSkillWhereClaudeLooks prevents installing the skill
// somewhere no assistant reads. A skill is only found at
// <root>/skills/<name>/SKILL.md, so a file written anywhere else is installed in
// name only.
func TestInstallSkillWritesSkillWhereClaudeLooks(t *testing.T) {
	root := t.TempDir()

	path, err := installSkillAt(io.Discard, root, "1.2.3", nil)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(root, "skills", "git-review", "SKILL.md"), path)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(content), "---\n"),
		"the frontmatter must survive: without it the file is not a skill at all")
	assert.Contains(t, string(content), "name: git-review")
}

// TestInstallSkillWritesTheFilesItLinksTo prevents installing an entry point
// whose links all dangle. SKILL.md holds the model and the four operations and
// points at companion files for the detail, so a reader who follows a link and
// finds nothing cannot tell the detail is missing rather than absent.
func TestInstallSkillWritesTheFilesItLinksTo(t *testing.T) {
	root := t.TempDir()

	path, err := installSkillAt(io.Discard, root, "1.2.3", nil)
	require.NoError(t, err)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	links := regexp.MustCompile(`\]\(([^)]+\.md)\)`).FindAllStringSubmatch(string(content), -1)
	require.NotEmpty(t, links, "SKILL.md must point at its companion files")
	for _, link := range links {
		assert.FileExists(t, filepath.Join(filepath.Dir(path), link[1]))
	}
}

// TestInstallSkillLeavesNothingBehindFromAnOlderInstall prevents a companion file
// that a later version renamed or dropped surviving beside the new skill, where it
// is read as part of it.
func TestInstallSkillLeavesNothingBehindFromAnOlderInstall(t *testing.T) {
	root := t.TempDir()

	path, err := installSkillAt(io.Discard, root, "1.0.0", nil)
	require.NoError(t, err)
	stale := filepath.Join(filepath.Dir(path), "gone-in-the-next-version.md")
	require.NoError(t, os.WriteFile(stale, []byte("# from an older maiao\n"), 0o644))

	_, err = installSkillAt(io.Discard, root, "2.0.0", nil)
	require.NoError(t, err)

	assert.NoFileExists(t, stale)
}

// TestInstallSkillStampsTheVersionItCameFrom prevents a skill that silently
// describes a maiao other than the one installed. The binary embeds the skill it
// shipped with, and the stamp is the only way to tell afterwards which that was.
func TestInstallSkillStampsTheVersionItCameFrom(t *testing.T) {
	root := t.TempDir()

	path, err := installSkillAt(io.Discard, root, "1.2.3", nil)
	require.NoError(t, err)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "maiao 1.2.3")
}

// TestInstallSkillReplacesAnOlderInstall prevents an upgrade leaving the previous
// version's skill in place, which would keep describing behaviour the binary no
// longer has.
func TestInstallSkillReplacesAnOlderInstall(t *testing.T) {
	root := t.TempDir()

	_, err := installSkillAt(io.Discard, root, "1.0.0", nil)
	require.NoError(t, err)
	path, err := installSkillAt(io.Discard, root, "2.0.0", nil)
	require.NoError(t, err)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "maiao 2.0.0")
	assert.NotContains(t, string(content), "maiao 1.0.0")
}

// TestInstallSkillCreatesMissingParents prevents failing on a fresh machine,
// where neither ~/.claude nor its skills directory exists yet.
func TestInstallSkillCreatesMissingParents(t *testing.T) {
	root := filepath.Join(t.TempDir(), "never", "created")

	_, err := installSkillAt(io.Discard, root, "1.2.3", nil)
	require.NoError(t, err)
}

// TestInstallSkillReportsWhereItWent prevents a silent install: the one thing the
// caller needs afterwards is the path, so it is printed rather than only returned.
func TestInstallSkillReportsWhereItWent(t *testing.T) {
	root := t.TempDir()
	out := &bytes.Buffer{}

	path, err := installSkillAt(out, root, "1.2.3", nil)
	require.NoError(t, err)

	assert.Contains(t, out.String(), path)
}

// TestSkillHarnessNamesAreSorted prevents the help text and the unknown-harness
// error drifting apart from the order installs print in.
func TestSkillHarnessNamesAreSorted(t *testing.T) {
	assert.True(t, slices.IsSorted(harnessNames()))
	assert.Equal(t, []string{
		"agents", "claude", "cline", "codex", "copilot", "cursor", "droid",
		"gemini", "goose", "kilo", "opencode", "pi", "windsurf",
	}, harnessNames())
}

// TestSkillHarnessesInstallWhereEachOneReads prevents a personal install landing
// in a directory that harness does not scan. The relative paths are the ones
// each harness documents.
func TestSkillHarnessesInstallWhereEachOneReads(t *testing.T) {
	home := isolateSkillHome(t)
	want := map[string]struct{ global, project string }{
		"agents":   {".agents", ".agents"},
		"claude":   {".claude", ".claude"},
		"cline":    {".agents", ".agents"},
		"codex":    {".codex", ".agents"},
		"copilot":  {".copilot", ".agents"},
		"cursor":   {".cursor", ".agents"},
		"droid":    {".agents", ".agents"},
		"gemini":   {".gemini", ".agents"},
		"goose":    {filepath.Join(".config", "goose"), ".goose"},
		"kilo":     {".kilo", ".agents"},
		"opencode": {filepath.Join(".config", "opencode"), ".agents"},
		"pi":       {filepath.Join(".pi", "agent"), ".pi"},
		"windsurf": {filepath.Join(".codeium", "windsurf"), ".windsurf"},
	}

	require.Equal(t, len(want), len(skillHarnesses))
	for _, h := range skillHarnesses {
		got, ok := want[h.name]
		require.True(t, ok, h.name)
		assert.Equal(t, filepath.Join(home, got.global), h.global(), h.name)
		assert.Equal(t, got.project, h.project, h.name)
	}
}

// TestInstallSkillTakesTheDirectoryAfterTheFlag prevents the destination being
// silently dropped. --skill carries an optional value, which pflag only reads
// from --skill=dir; written as --skill dir the directory is left as a positional
// argument, and the install goes to the default root while reporting success.
func TestInstallSkillTakesTheDirectoryAfterTheFlag(t *testing.T) {
	home := isolateSkillHome(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cursor"), 0o755))
	root := t.TempDir()

	cmd := NewCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"install", "--skill", root})
	require.NoError(t, cmd.Execute())

	assert.FileExists(t, filepath.Join(root, "skills", "git-review", "SKILL.md"))
	assert.NoFileExists(t, filepath.Join(home, ".claude", "skills", "git-review", "SKILL.md"),
		"the default root must be left alone when a directory was named")
	assert.NoFileExists(t, filepath.Join(home, ".cursor", "skills", "git-review", "SKILL.md"),
		"a named directory is that directory, even when a harness is installed")
}

// TestInstallSkillKeepsTheAssignedForm prevents the fix for the argument form
// taking away --skill=dir, which has always worked and is what a script written
// against an earlier maiao uses.
func TestInstallSkillKeepsTheAssignedForm(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()

	cmd := NewCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"install", "--skill=" + root})
	require.NoError(t, cmd.Execute())

	assert.FileExists(t, filepath.Join(root, "skills", "git-review", "SKILL.md"))
}

// TestInstallSkillDefaultsToDetectedHarnesses prevents a bare --skill writing
// only for Claude when another harness is the one installed. Each detected
// harness gets the directory it reads, and one that is not installed is left
// untouched.
func TestInstallSkillDefaultsToDetectedHarnesses(t *testing.T) {
	home := isolateSkillHome(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cursor"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o755))

	cmd := NewCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"install", "--skill"})
	require.NoError(t, cmd.Execute())

	assert.FileExists(t, filepath.Join(home, ".cursor", "skills", "git-review", "SKILL.md"))
	assert.FileExists(t, filepath.Join(home, ".claude", "skills", "git-review", "SKILL.md"))
	assert.NoFileExists(t, filepath.Join(home, ".codex", "skills", "git-review", "SKILL.md"))
}

// TestInstallSkillFallsBackToTheSharedAgentsDirectory prevents a machine with
// no harness yet getting a skill only Claude would read. ~/.agents/skills is
// the directory the shared harnesses have in common.
func TestInstallSkillFallsBackToTheSharedAgentsDirectory(t *testing.T) {
	home := isolateSkillHome(t)

	cmd := NewCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"install", "--skill"})
	require.NoError(t, cmd.Execute())

	assert.FileExists(t, filepath.Join(home, ".agents", "skills", "git-review", "SKILL.md"))
	assert.NoFileExists(t, filepath.Join(home, ".claude", "skills", "git-review", "SKILL.md"))
}

// TestInstallSkillNamesAHarness prevents --harness cursor writing Claude's
// directory, and prevents requiring that harness to be installed already: the
// flag is how a skill is put in place before the harness exists.
func TestInstallSkillNamesAHarness(t *testing.T) {
	home := isolateSkillHome(t)
	out := &bytes.Buffer{}

	cmd := NewCommand()
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"install", "--skill", "--harness", "cursor"})
	require.NoError(t, cmd.Execute())

	path := filepath.Join(home, ".cursor", "skills", "git-review", "SKILL.md")
	assert.FileExists(t, path)
	assert.Contains(t, out.String(), "for cursor")
	assert.NoFileExists(t, filepath.Join(home, ".claude", "skills", "git-review", "SKILL.md"))
}

// TestInstallSkillHonorsARelocatedHarnessHome prevents CLAUDE_CONFIG_DIR and
// CODEX_HOME being ignored. Those variables are how the harnesses move their
// config, and a skill written to the default home is one they do not load.
func TestInstallSkillHonorsARelocatedHarnessHome(t *testing.T) {
	home := isolateSkillHome(t)
	claude := t.TempDir()
	codex := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	t.Setenv("CODEX_HOME", codex)
	require.NoError(t, os.MkdirAll(claude, 0o755))
	require.NoError(t, os.MkdirAll(codex, 0o755))

	cmd := NewCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"install", "--skill"})
	require.NoError(t, cmd.Execute())

	assert.FileExists(t, filepath.Join(claude, "skills", "git-review", "SKILL.md"))
	assert.FileExists(t, filepath.Join(codex, "skills", "git-review", "SKILL.md"))
	assert.NoFileExists(t, filepath.Join(home, ".claude", "skills", "git-review", "SKILL.md"))
	assert.NoFileExists(t, filepath.Join(home, ".codex", "skills", "git-review", "SKILL.md"))
}

// TestInstallSkillSharesOneProjectDirectory prevents naming cursor and codex
// for one repository writing the skill twice. Both read .agents/skills, and a
// second copy is a second skill of the same name.
func TestInstallSkillSharesOneProjectDirectory(t *testing.T) {
	isolateSkillHome(t)
	project := t.TempDir()
	out := &bytes.Buffer{}

	cmd := NewCommand()
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"install", "--skill", "--harness", "cursor", "--harness", "codex", "--harness", "claude", project})
	require.NoError(t, cmd.Execute())

	assert.FileExists(t, filepath.Join(project, ".agents", "skills", "git-review", "SKILL.md"))
	assert.FileExists(t, filepath.Join(project, ".claude", "skills", "git-review", "SKILL.md"))
	assert.Contains(t, out.String(), "for cursor, codex")
	assert.Contains(t, out.String(), "for claude")
	assert.Equal(t, 2, strings.Count(out.String(), "Installed the"))
}

// TestInstallSkillClineUsesTheSharedAgentsDirectory prevents a Cline install
// writing under ~/.cline, which Cline does not scan. Cline reads the shared
// agents directory.
func TestInstallSkillClineUsesTheSharedAgentsDirectory(t *testing.T) {
	home := isolateSkillHome(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cline"), 0o755))

	cmd := NewCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"install", "--skill"})
	require.NoError(t, cmd.Execute())

	assert.FileExists(t, filepath.Join(home, ".agents", "skills", "git-review", "SKILL.md"))
	assert.NoFileExists(t, filepath.Join(home, ".cline", "skills", "git-review", "SKILL.md"))
}

// TestInstallSkillRejectsAnUnknownHarness prevents a misspelled harness being
// treated as success with nothing written.
func TestInstallSkillRejectsAnUnknownHarness(t *testing.T) {
	isolateSkillHome(t)

	cmd := NewCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"install", "--skill", "--harness", "notepad"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notepad")
	assert.Contains(t, err.Error(), "cursor")
}

// TestInstallRejectsHarnessWithoutSkill prevents --harness on a hook install
// being ignored, which would report the hook installed and leave the skill
// unwritten.
func TestInstallRejectsHarnessWithoutSkill(t *testing.T) {
	cmd := NewCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"install", "--harness", "cursor", "--path", t.TempDir()})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--skill")
}

// TestInstallRejectsADirectoryWithoutSkill prevents the other half of the same
// silence: install ignored every positional argument, so a mistyped destination
// was reported as a successful install somewhere else.
func TestInstallRejectsADirectoryWithoutSkill(t *testing.T) {
	cmd := NewCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	// --path keeps the hook install away from this repository's own hooks; the
	// argument must be refused before anything looks for a repository at all.
	cmd.SetArgs([]string{"install", "--path", t.TempDir(), t.TempDir()})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--skill",
		"the message must name the flag that gives a directory a meaning")
}

// TestInstallSkillRejectsTwoDestinations prevents picking one of two directories
// the user named, which is a guess either way.
func TestInstallSkillRejectsTwoDestinations(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := NewCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"install", "--skill=" + t.TempDir(), t.TempDir()})

	assert.Error(t, cmd.Execute())
}
