package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInstallSkillWritesSkillWhereClaudeLooks prevents installing the skill
// somewhere no assistant reads. A skill is only found at
// <root>/skills/<name>/SKILL.md, so a file written anywhere else is installed in
// name only.
func TestInstallSkillWritesSkillWhereClaudeLooks(t *testing.T) {
	root := t.TempDir()

	path, err := installSkillAt(io.Discard, root, "1.2.3")
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

	path, err := installSkillAt(io.Discard, root, "1.2.3")
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

	path, err := installSkillAt(io.Discard, root, "1.0.0")
	require.NoError(t, err)
	stale := filepath.Join(filepath.Dir(path), "gone-in-the-next-version.md")
	require.NoError(t, os.WriteFile(stale, []byte("# from an older maiao\n"), 0o644))

	_, err = installSkillAt(io.Discard, root, "2.0.0")
	require.NoError(t, err)

	assert.NoFileExists(t, stale)
}

// TestInstallSkillStampsTheVersionItCameFrom prevents a skill that silently
// describes a maiao other than the one installed. The binary embeds the skill it
// shipped with, and the stamp is the only way to tell afterwards which that was.
func TestInstallSkillStampsTheVersionItCameFrom(t *testing.T) {
	root := t.TempDir()

	path, err := installSkillAt(io.Discard, root, "1.2.3")
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

	_, err := installSkillAt(io.Discard, root, "1.0.0")
	require.NoError(t, err)
	path, err := installSkillAt(io.Discard, root, "2.0.0")
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

	_, err := installSkillAt(io.Discard, root, "1.2.3")
	require.NoError(t, err)
}

// TestInstallSkillReportsWhereItWent prevents a silent install: the one thing the
// caller needs afterwards is the path, so it is printed rather than only returned.
func TestInstallSkillReportsWhereItWent(t *testing.T) {
	root := t.TempDir()
	out := &bytes.Buffer{}

	path, err := installSkillAt(out, root, "1.2.3")
	require.NoError(t, err)

	assert.Contains(t, out.String(), path)
}

// TestDefaultSkillRootIsClaudeHome prevents the default landing somewhere the
// user's assistant does not read, which would make `install --skill` with no
// --path appear to do nothing.
func TestDefaultSkillRootIsClaudeHome(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(home, ".claude"), defaultSkillRoot())
}

// TestInstallSkillTakesTheDirectoryAfterTheFlag prevents the destination being
// silently dropped. --skill carries an optional value, which pflag only reads
// from --skill=dir; written as --skill dir the directory is left as a positional
// argument, and the install goes to the default root while reporting success.
func TestInstallSkillTakesTheDirectoryAfterTheFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()

	cmd := NewCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"install", "--skill", root})
	require.NoError(t, cmd.Execute())

	assert.FileExists(t, filepath.Join(root, "skills", "git-review", "SKILL.md"))
	assert.NoFileExists(t, filepath.Join(home, ".claude", "skills", "git-review", "SKILL.md"),
		"the default root must be left alone when a directory was named")
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

// TestInstallSkillDefaultsToClaudeHome prevents a bare --skill losing its default
// once an argument is allowed after it.
func TestInstallSkillDefaultsToClaudeHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := NewCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"install", "--skill"})
	require.NoError(t, cmd.Execute())

	assert.FileExists(t, filepath.Join(home, ".claude", "skills", "git-review", "SKILL.md"))
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
