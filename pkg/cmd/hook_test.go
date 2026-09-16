package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/adevinta/maiao/pkg/gerrit"
	lgit "github.com/adevinta/maiao/pkg/git"
	"github.com/adevinta/maiao/pkg/prompt"
	"github.com/adevinta/maiao/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// foreignHook stands in for the commit message hook husky, lefthook or
// pre-commit installs. It lints and says so, and it ends in `exit 0`, which is
// what makes appending to such a hook the wrong way to extend it.
const foreignHook = `#!/bin/sh
echo commitlint-ran >&2
exit 0
`

// installForeignHook puts a hook belonging to something else exactly where maiao
// resolves its own to, which is what a hook manager does.
func installForeignHook(t testing.TB, gitDir string) string {
	t.Helper()
	path := lgit.HookPath(gitDir, lgit.CommitMsgHook)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(foreignHook), 0755))
	return path
}

// assertBothHooksRan commits and checks that the repository's own hook still runs
// and that the commit came out with a Change-Id. Nothing short of this proves the
// two coexist.
func assertBothHooksRan(t testing.TB, repo string) {
	t.Helper()
	out, err := testutil.CmdCombinedOutput("git", "-C", repo, "commit", "--allow-empty", "-m", "x")
	require.NoError(t, err, out)
	assert.Contains(t, out, "commitlint-ran", "the repository's own hook must still run")
	assert.Contains(t, changeIDs(t, repo), "Change-Id: I0000", "maiao's hook must run as well")
}

func TestInstallOverForeignHook(t *testing.T) {
	t.Run("accepted, both hooks run", func(t *testing.T) {
		testutil.IsolateHome(t)
		setBatch(t, false)
		stubHookInstall(t)
		repo := testutil.InitRepo(t)
		gitDir, err := lgit.FindGitDir(repo)
		require.NoError(t, err)
		path := installForeignHook(t, gitDir)
		asked := false
		stubConfirm(t, func(string) bool { asked = true; return true })

		out, err := runInstall(t, "-C", repo)
		require.NoError(t, err)
		assert.True(t, asked, "editing a file maiao did not write must be asked about")
		assert.Contains(t, out, gerrit.ChainedHookName, "the output must say where maiao's hook went")
		assert.FileExists(t, filepath.Join(filepath.Dir(path), gerrit.ChainedHookName))
		assertBothHooksRan(t, repo)
	})

	t.Run("declined, the hook is left exactly as it was", func(t *testing.T) {
		testutil.IsolateHome(t)
		setBatch(t, false)
		stubHookInstall(t)
		repo := testutil.InitRepo(t)
		gitDir, err := lgit.FindGitDir(repo)
		require.NoError(t, err)
		path := installForeignHook(t, gitDir)
		stubConfirm(t, func(string) bool { return false })

		out, err := runInstall(t, "-C", repo)
		require.NoError(t, err)
		assertUntouched(t, path)
		assert.Contains(t, out, gerrit.ChainCall(gerrit.ChainedHookName),
			"declining must still leave the user the line to add")
	})

	t.Run("in batch mode, it fails with the line to add", func(t *testing.T) {
		testutil.IsolateHome(t)
		setBatch(t, true)
		stubHookInstall(t)
		repo := testutil.InitRepo(t)
		gitDir, err := lgit.FindGitDir(repo)
		require.NoError(t, err)
		path := installForeignHook(t, gitDir)
		stubConfirm(t, func(string) bool {
			t.Error("must not prompt with nobody to answer")
			return true
		})

		_, err = runInstall(t, "-C", repo)
		require.Error(t, err)
		assert.True(t, errors.Is(err, prompt.ErrNoInput),
			"an unanswerable question is what exit status %d is for, got %v", ExitInputRequired, err)
		assert.Equal(t, ExitInputRequired, ExitCode(err))
		assert.Contains(t, err.Error(), gerrit.ChainCall(gerrit.ChainedHookName))
		assertUntouched(t, path)
	})

	t.Run("with --force, it replaces the hook", func(t *testing.T) {
		testutil.IsolateHome(t)
		setBatch(t, true)
		stubHookInstall(t)
		repo := testutil.InitRepo(t)
		gitDir, err := lgit.FindGitDir(repo)
		require.NoError(t, err)
		path := installForeignHook(t, gitDir)
		stubConfirm(t, func(string) bool {
			t.Error("--force is the answer, so there is nothing to ask")
			return true
		})

		_, err = runInstall(t, "-C", repo, "--force")
		require.NoError(t, err)
		assert.Equal(t, gerrit.MaiaoHook, gerrit.StateAt(path))
		assert.NotContains(t, contents(t, path), "commitlint-ran",
			"--force is the one way to lose the existing hook, and it was asked for")
	})

	// A hook written in something else cannot have a shell call inserted into it
	// without breaking it, so maiao explains instead of guessing.
	t.Run("a hook that is not a shell script is described, not edited", func(t *testing.T) {
		testutil.IsolateHome(t)
		setBatch(t, false)
		stubHookInstall(t)
		repo := testutil.InitRepo(t)
		gitDir, err := lgit.FindGitDir(repo)
		require.NoError(t, err)
		path := lgit.HookPath(gitDir, lgit.CommitMsgHook)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, []byte("#!/usr/bin/env python3\nprint('lint')\n"), 0755))
		stubConfirm(t, func(string) bool {
			t.Error("must not ask to do something maiao cannot do")
			return true
		})

		_, err = runInstall(t, "-C", repo)
		require.Error(t, err)
		assert.Contains(t, err.Error(), gerrit.ChainedHookName, "the message must say where maiao's hook is")
		assert.Equal(t, "#!/usr/bin/env python3\nprint('lint')\n", contents(t, path))
	})
}

// TestInstallGlobalOverForeignTemplateHook covers the same mistake one level up: a
// commit message hook in the user's own template directory reaches every
// repository they create from then on, so replacing it is worse, not better.
func TestInstallGlobalOverForeignTemplateHook(t *testing.T) {
	home := testutil.IsolateHome(t)
	setBatch(t, false)
	stubHookInstall(t)

	existing := filepath.Join(home, "my-template")
	require.NoError(t, os.MkdirAll(filepath.Join(existing, "hooks"), 0700))
	hook := filepath.Join(existing, "hooks", "commit-msg")
	require.NoError(t, os.WriteFile(hook, []byte(foreignHook), 0755))
	testutil.Cmd(t, "git", "config", "--global", templateDirOption, existing)
	stubConfirm(t, func(string) bool { return true })

	_, err := runInstall(t, "--global")
	require.NoError(t, err)

	assert.Contains(t, contents(t, hook), "commitlint-ran", "the user's own template hook must survive")
	assert.FileExists(t, filepath.Join(existing, "hooks", gerrit.ChainedHookName))

	// The chained call resolves itself from $0, so it keeps working once git has
	// copied the whole hooks directory into a repository that did not exist yet.
	t.Run("a repository created afterwards runs both", func(t *testing.T) {
		assertBothHooksRan(t, testutil.InitRepo(t))
	})
}

func TestEnsureCommitMsgHookOverForeignHook(t *testing.T) {
	t.Run("it asks even when auto install is opted in", func(t *testing.T) {
		testutil.IsolateHome(t)
		setBatch(t, false)
		stubHookInstall(t)
		repo := testutil.InitRepo(t)
		testutil.Cmd(t, "git", "config", "--global", autoInstallHookOption, "true")
		gitDir, err := lgit.FindGitDir(repo)
		require.NoError(t, err)
		installForeignHook(t, gitDir)
		asked := false
		stubConfirm(t, func(string) bool { asked = true; return true })

		proceed, err := ensureCommitMsgHook(repo, gitDir)
		require.NoError(t, err)
		assert.True(t, proceed)
		assert.True(t, asked,
			"opting in to installing a hook is not opting in to rewriting another tool's")
		assertBothHooksRan(t, repo)
	})

	t.Run("declined, the review stops rather than going ahead without Change-Ids", func(t *testing.T) {
		testutil.IsolateHome(t)
		setBatch(t, false)
		stubHookInstall(t)
		repo := testutil.InitRepo(t)
		gitDir, err := lgit.FindGitDir(repo)
		require.NoError(t, err)
		path := installForeignHook(t, gitDir)
		stubConfirm(t, func(string) bool { return false })

		proceed, err := ensureCommitMsgHook(repo, gitDir)
		require.NoError(t, err)
		assert.False(t, proceed)
		assertUntouched(t, path)
	})

	// Before the hook was recognised by content, any file at the path counted as
	// maiao's, so a review ran on to fail somewhere else entirely while every
	// commit it made carried no Change-Id.
	t.Run("a foreign hook does not pass for maiao's", func(t *testing.T) {
		testutil.IsolateHome(t)
		setBatch(t, true)
		stubHookInstall(t)
		repo := testutil.InitRepo(t)
		gitDir, err := lgit.FindGitDir(repo)
		require.NoError(t, err)
		path := installForeignHook(t, gitDir)

		proceed, err := ensureCommitMsgHook(repo, gitDir)
		require.Error(t, err, "the review must not proceed as though the hook were maiao's")
		assert.False(t, proceed)
		assert.Equal(t, ExitInputRequired, ExitCode(err))
		assertUntouched(t, path)
	})
}

func assertUntouched(t testing.TB, path string) {
	t.Helper()
	assert.Equal(t, foreignHook, contents(t, path), "a hook maiao did not write must not change")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
}

func contents(t testing.TB, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}
