// Package testutil provides helpers for tests that need a real git repository.
//
// Helpers live in a regular file rather than a _test.go one so that several
// packages can share them, following the convention of pkg/system.
package testutil

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Cmd runs a command and fails the test if it does not succeed.
func Cmd(t testing.TB, cmd string, args ...string) {
	t.Helper()
	c := exec.Command(cmd, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		t.Fatalf("failed to run command %s: %s", strings.Join(append([]string{cmd}, args...), " "), err.Error())
	}
}

// CmdOutput runs a command and returns its trimmed standard output, failing the
// test if it does not succeed.
func CmdOutput(t testing.TB, cmd string, args ...string) string {
	t.Helper()
	c := exec.Command(cmd, args...)
	b := bytes.NewBuffer(nil)
	c.Stdout = b
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		t.Fatalf("failed to run command %s: %s", strings.Join(append([]string{cmd}, args...), " "), err.Error())
	}
	return strings.Trim(b.String(), " \n")
}

// CmdCombinedOutput runs a command and returns its output and error, for tests
// that assert on what a command printed even when it may fail. Hook output lands
// on standard error, so both streams are captured.
func CmdCombinedOutput(cmd string, args ...string) (string, error) {
	out, err := exec.Command(cmd, args...).CombinedOutput()
	return string(out), err
}

// CommitFile writes content to path inside dir and commits it, returning the
// resulting commit hash.
func CommitFile(t testing.TB, dir, path, content, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0600); err != nil {
		t.Fatalf("failed to write %s: %s", path, err.Error())
	}
	Cmd(t, "git", "-C", dir, "add", path)
	Cmd(t, "git", "-C", dir, "commit", "-m", message)
	return CmdOutput(t, "git", "-C", dir, "rev-parse", "HEAD")
}

// IsolateHome points every location git and go-git read user level
// configuration from at a temporary directory, and returns it.
//
// Tests that write global git configuration must call this first. Without it
// they edit the developer's own ~/.gitconfig, and a test setting something like
// init.templateDir would do so machine wide. It also works the other way: a
// developer who has core.hooksPath set globally would otherwise see unrelated
// tests fail.
func IsolateHome(t testing.TB) string {
	t.Helper()
	home := t.TempDir()
	// GIT_CONFIG_GLOBAL is what git itself honours. HOME and XDG_CONFIG_HOME
	// cover go-git, which builds the paths by hand, and any git subprocess on a
	// version predating GIT_CONFIG_GLOBAL.
	for key, value := range map[string]string{
		"HOME":              home,
		"XDG_CONFIG_HOME":   filepath.Join(home, ".config"),
		"GIT_CONFIG_GLOBAL": filepath.Join(home, ".gitconfig"),
	} {
		t.Setenv(key, value)
	}
	return home
}

// InitRepo creates a repository with one empty commit in a temporary directory
// and returns its top level path.
//
// The path comes from git rather than from os.MkdirTemp, because on macOS
// temporary directories live under a symlinked /tmp while git reports the
// resolved /private/tmp. Returning git's own answer keeps comparisons honest.
func InitRepo(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	Cmd(t, "git", "-c", "init.defaultBranch=main", "init", "-q", dir)
	// Do not depend on whatever the developer or CI has configured.
	Cmd(t, "git", "-C", dir, "config", "user.name", "maiao tests")
	Cmd(t, "git", "-C", dir, "config", "user.email", "maiao-tests@example.com")
	Cmd(t, "git", "-C", dir, "config", "commit.gpgsign", "false")
	Cmd(t, "git", "-C", dir, "commit", "-q", "--allow-empty", "-m", "initial commit")
	return CmdOutput(t, "git", "-C", dir, "rev-parse", "--show-toplevel")
}

// AddWorktree adds a linked worktree on a new branch and returns its top level
// path, as reported by git.
func AddWorktree(t testing.TB, repo, branch string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "worktree")
	Cmd(t, "git", "-C", repo, "worktree", "add", "-q", "-b", branch, dir)
	t.Cleanup(func() {
		// Leave the shared repository consistent for later assertions.
		_ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", dir).Run()
	})
	return CmdOutput(t, "git", "-C", dir, "rev-parse", "--show-toplevel")
}
