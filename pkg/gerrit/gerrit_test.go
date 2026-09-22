package gerrit

import (
	"testing"

	"github.com/adevinta/maiao/pkg/git"
	"github.com/adevinta/maiao/pkg/log"
	"github.com/adevinta/maiao/pkg/system"
	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestHookPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	system.DefaultFileSystem = fs
	t.Cleanup(system.Reset)

	system.EnsureTestFileContent(t, fs, "/src/.git/hooks/commit-msg", "some-content")

	assert.Equal(t, "/src/.git/hooks/commit-msg", git.HookPath("/src/.git/", git.CommitMsgHook))
}

func TestIsInstalled(t *testing.T) {
	fs := afero.NewMemMapFs()
	system.DefaultFileSystem = fs
	t.Cleanup(system.Reset)

	system.EnsureTestFileContent(t, fs, "/src/.git/hooks/commit-msg", changeIDHook)
	system.EnsureTestFileContent(t, fs, "/src/.git/worktrees/some-name/commondir", "../..")
	t.Run("when the hook is installed, installed returns True", func(t *testing.T) {
		assert.True(t, Installed("/src/.git/"))
	})
	t.Run("when running in a worktree, installed returns True", func(t *testing.T) {
		assert.True(t, Installed("/src/.git/worktrees/some-name"))
	})
	t.Run("when the hook is not installed, installed returns False", func(t *testing.T) {
		assert.False(t, Installed("/src/"))
	})
	// A hook manager puts its own commit message hook exactly where maiao's
	// belongs. Counting it as maiao's is what makes the failure silent: commits
	// get no Change-Id and nothing ever says why.
	t.Run("when the hook belongs to something else, installed returns False", func(t *testing.T) {
		system.EnsureTestFileContent(t, fs, "/other/.git/hooks/commit-msg", foreignHook)
		assert.False(t, Installed("/other/.git/"))
	})
	t.Run("when the hook chains to maiao's, installed returns True", func(t *testing.T) {
		system.EnsureTestFileContent(t, fs, "/chained/.git/hooks/commit-msg", foreignHook+"\n"+ChainCall(ChainedHookName)+"\n")
		assert.True(t, Installed("/chained/.git/"))
	})
}

func TestInstall(t *testing.T) {
	t.Cleanup(system.Reset)

	t.Run("when the hooks directory does not exist", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		system.DefaultFileSystem = fs
		testHookInstalled(t, fs, "/src/some-repo/.git")
	})
	t.Run("when the hooks directory already exists", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		system.DefaultFileSystem = fs
		system.EnsureTestFileContent(t, fs, git.HookPath("/src/some-repo/.git", git.CommitMsgHook), "#!/bin/bash\necho hello world")

		testHookInstalled(t, fs, "/src/some-repo/.git")
	})
}

func testHookInstalled(t *testing.T, fs afero.Fs, path string) {
	t.Helper()
	t.Run("hook installation succeed", func(t *testing.T) {
		assert.NoError(t, Install(path))
		system.AssertPathExists(t, fs, git.HookPath(path, git.CommitMsgHook))
		system.AssertFileContents(t, fs, git.HookPath(path, git.CommitMsgHook), string(commitMsgHook))
		system.AssertModePerm(t, fs, git.HookPath(path, git.CommitMsgHook), "-rwxr-xr-x")
	})
}

// get all logs when running tests
func init() {
	log.Logger.SetLevel(logrus.DebugLevel)
}
