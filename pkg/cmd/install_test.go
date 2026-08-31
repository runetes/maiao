package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	lgit "github.com/adevinta/maiao/pkg/git"
	"github.com/adevinta/maiao/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHook stands in for the gerrit hook. It appends a fixed Change-Id, which is
// enough to tell whether git ran it, and avoids downloading anything.
const fakeHook = `#!/bin/sh
echo "" >> "$1"
echo "Change-Id: I0000000000000000000000000000000000000000" >> "$1"
`

// stubHookInstall replaces the download with a local write for the duration of
// the test.
func stubHookInstall(t testing.TB) {
	t.Helper()
	original := installHook
	t.Cleanup(func() { installHook = original })
	installHook = func(path string) error {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(fakeHook), 0700)
	}
}

// runInstall runs `git review install` with the given arguments.
func runInstall(t testing.TB, args ...string) (string, error) {
	t.Helper()
	out := bytes.NewBuffer(nil)
	c := NewCommand()
	c.SetOut(out)
	c.SetErr(out)
	c.SetArgs(append([]string{"install"}, args...))
	err := c.Execute()
	return out.String(), err
}

func changeIDs(t testing.TB, repo string) string {
	t.Helper()
	return testutil.CmdOutput(t, "git", "-C", repo, "log", "-1", "--format=%B")
}

func TestInstallGlobal(t *testing.T) {
	home := testutil.IsolateHome(t)
	stubHookInstall(t)

	out, err := runInstall(t, "--global")
	require.NoError(t, err)

	template, ok := lgit.ConfigPath("", templateDirOption)
	require.True(t, ok, "init.templateDir must be set, got output:\n%s", out)
	assert.True(t, lgit.ConfigBool("", autoInstallHookOption), "maiao.autoInstallHook must be set")

	t.Run("the template hook is executable", func(t *testing.T) {
		info, err := os.Stat(filepath.Join(template, "hooks", "commit-msg"))
		require.NoError(t, err)
		assert.NotZero(t, info.Mode().Perm()&0100, "hook must be executable, got %s", info.Mode().Perm())
	})

	t.Run("it stays inside the isolated home", func(t *testing.T) {
		// Guards the test itself: without this the suite would be writing to the
		// developer's real configuration.
		assert.Contains(t, template, home)
	})

	t.Run("it follows git's config convention on every platform", func(t *testing.T) {
		assert.Equal(t, filepath.Join(home, ".config", "maiao", "template"), template)
		assert.NotContains(t, template, " ", "a path with spaces in git config is a needless papercut")
	})

	// The property that matters is not that a file exists, but that a repository
	// created afterwards produces commits with a Change-Id.
	t.Run("a newly created repository gets Change-Ids", func(t *testing.T) {
		repo := testutil.InitRepo(t)
		assert.Contains(t, changeIDs(t, repo), "Change-Id: I0000")
	})

	t.Run("a cloned repository gets Change-Ids", func(t *testing.T) {
		origin := testutil.InitRepo(t)
		clone := filepath.Join(t.TempDir(), "clone")
		testutil.Cmd(t, "git", "clone", origin, clone)
		testutil.Cmd(t, "git", "-C", clone, "config", "user.name", "maiao tests")
		testutil.Cmd(t, "git", "-C", clone, "config", "user.email", "maiao-tests@example.com")
		testutil.Cmd(t, "git", "-C", clone, "commit", "--allow-empty", "-m", "in the clone")
		assert.Contains(t, changeIDs(t, clone), "Change-Id: I0000")
	})

	t.Run("repository local hooks keep running", func(t *testing.T) {
		// The reason core.hooksPath was not used: it would suppress these.
		repo := testutil.InitRepo(t)
		hooks := filepath.Join(repo, ".git", "hooks")
		require.NoError(t, os.MkdirAll(hooks, 0700))
		require.NoError(t, os.WriteFile(filepath.Join(hooks, "pre-commit"),
			[]byte("#!/bin/sh\necho repo-local-hook-ran >&2\n"), 0700))

		out, err := testutil.CmdCombinedOutput("git", "-C", repo, "commit", "--allow-empty", "-m", "x")
		require.NoError(t, err, out)
		assert.Contains(t, out, "repo-local-hook-ran")
	})
}

// TestInstallGlobalKeepsExistingTemplate covers the user who already has a
// template directory: maiao must add to it, not point the setting elsewhere.
func TestInstallGlobalKeepsExistingTemplate(t *testing.T) {
	home := testutil.IsolateHome(t)
	stubHookInstall(t)

	existing := filepath.Join(home, "my-template")
	require.NoError(t, os.MkdirAll(filepath.Join(existing, "hooks"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(existing, "hooks", "pre-commit"),
		[]byte("#!/bin/sh\necho my-own-hook-ran >&2\n"), 0700))
	testutil.Cmd(t, "git", "config", "--global", templateDirOption, existing)

	_, err := runInstall(t, "--global")
	require.NoError(t, err)

	value, _ := lgit.ConfigPath("", templateDirOption)
	assert.Equal(t, existing, value, "the user's template directory must be left in place")
	assert.FileExists(t, filepath.Join(existing, "hooks", "commit-msg"))

	repo := testutil.InitRepo(t)
	out, err := testutil.CmdCombinedOutput("git", "-C", repo, "commit", "--allow-empty", "-m", "x")
	require.NoError(t, err, out)
	assert.Contains(t, out, "my-own-hook-ran", "the user's own template hook must survive")
	assert.Contains(t, changeIDs(t, repo), "Change-Id: I0000")
}

// TestInstallGlobalExpandsTilde covers a template directory configured as ~/...,
// which git expands and a naive read would not.
func TestInstallGlobalExpandsTilde(t *testing.T) {
	home := testutil.IsolateHome(t)
	stubHookInstall(t)
	testutil.Cmd(t, "git", "config", "--global", templateDirOption, "~/tilde-template")

	_, err := runInstall(t, "--global")
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(home, "tilde-template", "hooks", "commit-msg"))
	assert.NoDirExists(t, filepath.Join(home, "~"), "a literal ~ directory means the path was not expanded")
}

func TestEnsureCommitMsgHook(t *testing.T) {
	t.Run("already installed, nothing to do", func(t *testing.T) {
		testutil.IsolateHome(t)
		repo := testutil.InitRepo(t)
		gitDir, err := lgit.FindGitDir(repo)
		require.NoError(t, err)
		installFakeHook(t, gitDir)
		stubConfirm(t, func(string) bool {
			t.Error("must not prompt when the hook is already installed")
			return false
		})
		expectNoInstall(t)

		proceed, err := ensureCommitMsgHook(repo, gitDir)
		require.NoError(t, err)
		assert.True(t, proceed)
	})

	t.Run("auto install opted in, installs without asking", func(t *testing.T) {
		testutil.IsolateHome(t)
		repo := testutil.InitRepo(t)
		gitDir, err := lgit.FindGitDir(repo)
		require.NoError(t, err)
		stubHookInstall(t)
		testutil.Cmd(t, "git", "config", "--global", autoInstallHookOption, "true")
		stubConfirm(t, func(string) bool {
			t.Error("must not prompt once the user has opted in globally")
			return false
		})

		proceed, err := ensureCommitMsgHook(repo, gitDir)
		require.NoError(t, err)
		assert.True(t, proceed)
		assert.FileExists(t, lgit.HookPath(gitDir, lgit.CommitMsgHook))
	})

	t.Run("not opted in and declined, the review stops", func(t *testing.T) {
		testutil.IsolateHome(t)
		repo := testutil.InitRepo(t)
		gitDir, err := lgit.FindGitDir(repo)
		require.NoError(t, err)
		asked := false
		stubConfirm(t, func(string) bool { asked = true; return false })
		expectNoInstall(t)

		proceed, err := ensureCommitMsgHook(repo, gitDir)
		require.NoError(t, err)
		assert.False(t, proceed)
		assert.True(t, asked, "the user must still be asked when they have not opted in")
	})

	t.Run("not opted in and accepted, installs", func(t *testing.T) {
		testutil.IsolateHome(t)
		repo := testutil.InitRepo(t)
		gitDir, err := lgit.FindGitDir(repo)
		require.NoError(t, err)
		stubHookInstall(t)
		stubConfirm(t, func(string) bool { return true })

		proceed, err := ensureCommitMsgHook(repo, gitDir)
		require.NoError(t, err)
		assert.True(t, proceed)
		assert.FileExists(t, lgit.HookPath(gitDir, lgit.CommitMsgHook))
	})

	// The interaction with the core.hooksPath fix: auto installing must write
	// where git will actually run the hook, or it silently does nothing.
	t.Run("auto install honours core.hooksPath", func(t *testing.T) {
		testutil.IsolateHome(t)
		repo := testutil.InitRepo(t)
		testutil.Cmd(t, "git", "-C", repo, "config", "core.hooksPath", ".githooks")
		testutil.Cmd(t, "git", "config", "--global", autoInstallHookOption, "true")
		gitDir, err := lgit.FindGitDir(repo)
		require.NoError(t, err)
		stubHookInstall(t)
		stubConfirm(t, func(string) bool {
			t.Error("must not prompt once the user has opted in globally")
			return false
		})

		proceed, err := ensureCommitMsgHook(repo, gitDir)
		require.NoError(t, err)
		assert.True(t, proceed)
		assert.FileExists(t, filepath.Join(repo, ".githooks", "commit-msg"))

		testutil.Cmd(t, "git", "-C", repo, "commit", "--allow-empty", "-m", "x")
		assert.Contains(t, changeIDs(t, repo), "Change-Id: I0000",
			"the hook must be installed where git runs it")
	})
}

func installFakeHook(t testing.TB, gitDir string) {
	t.Helper()
	path := lgit.HookPath(gitDir, lgit.CommitMsgHook)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(fakeHook), 0700))
}

// expectNoInstall fails the test if the hook gets installed. It also keeps the
// test off the network, since the real installer downloads the hook.
func expectNoInstall(t testing.TB) {
	t.Helper()
	original := installHook
	t.Cleanup(func() { installHook = original })
	installHook = func(path string) error {
		t.Errorf("unexpected hook installation at %s", path)
		return nil
	}
}

func stubConfirm(t testing.TB, f func(string) bool) {
	t.Helper()
	original := confirm
	t.Cleanup(func() { confirm = original })
	confirm = f
}
