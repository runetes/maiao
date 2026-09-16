package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adevinta/maiao/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitHookPathOracle is what these tests assert against: whatever git itself
// reports from inside the working tree is by definition the right answer.
func gitHookPathOracle(t testing.TB, workingTree string) string {
	t.Helper()
	path := testutil.CmdOutput(t, "git", "-C", workingTree, "rev-parse", "--git-path", "hooks/commit-msg")
	if !filepath.IsAbs(path) {
		path = filepath.Join(workingTree, path)
	}
	return path
}

func TestHookPath(t *testing.T) {
	testutil.IsolateHome(t)
	repo := testutil.InitRepo(t)
	worktree := testutil.AddWorktree(t, repo, "worktree-branch")

	repoGitDir, err := FindGitDir(repo)
	require.NoError(t, err)
	worktreeGitDir, err := FindGitDir(worktree)
	require.NoError(t, err)

	t.Run("in the main working tree", func(t *testing.T) {
		assert.Equal(t, filepath.Join(repoGitDir, "hooks", "commit-msg"), HookPath(repoGitDir, CommitMsgHook))
	})

	t.Run("in a worktree, hooks live in the common git dir", func(t *testing.T) {
		assert.Equal(t, filepath.Join(repoGitDir, "hooks", "commit-msg"), HookPath(worktreeGitDir, CommitMsgHook))
	})

	t.Run("a trailing separator does not change the answer", func(t *testing.T) {
		assert.Equal(t, filepath.Join(repoGitDir, "hooks", "commit-msg"), HookPath(repoGitDir+string(filepath.Separator), CommitMsgHook))
	})

	t.Run("matches git", func(t *testing.T) {
		assert.Equal(t, gitHookPathOracle(t, repo), HookPath(repoGitDir, CommitMsgHook))
		assert.Equal(t, gitHookPathOracle(t, worktree), HookPath(worktreeGitDir, CommitMsgHook))
	})
}

// TestHookPathHonoursHooksPath is the regression test for hooks being installed
// where git would never run them.
//
// Each case asserts two things: that the resolved path matches git's own
// answer, and that a hook placed there is the one git executes. The second
// assertion is the one that matters, because a plausible looking path that git
// does not execute is precisely the bug.
func TestHookPathHonoursHooksPath(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t testing.TB, home, repo string)
	}{
		{
			name:  "unset",
			setup: func(t testing.TB, home, repo string) {},
		},
		{
			name: "local and relative",
			setup: func(t testing.TB, home, repo string) {
				testutil.Cmd(t, "git", "-C", repo, "config", "core.hooksPath", ".githooks")
			},
		},
		{
			name: "local and absolute",
			setup: func(t testing.TB, home, repo string) {
				testutil.Cmd(t, "git", "-C", repo, "config", "core.hooksPath", filepath.Join(home, "absolute-hooks"))
			},
		},
		{
			name: "global",
			setup: func(t testing.TB, home, repo string) {
				testutil.Cmd(t, "git", "config", "--global", "core.hooksPath", filepath.Join(home, "global-hooks"))
			},
		},
		{
			name: "local overrides global",
			setup: func(t testing.TB, home, repo string) {
				testutil.Cmd(t, "git", "config", "--global", "core.hooksPath", filepath.Join(home, "global-hooks"))
				testutil.Cmd(t, "git", "-C", repo, "config", "core.hooksPath", filepath.Join(home, "local-hooks"))
			},
		},
		{
			name: "pulled in through an include directive",
			setup: func(t testing.TB, home, repo string) {
				included := filepath.Join(home, "included.gitconfig")
				content := "[core]\n\thooksPath = " + filepath.Join(home, "included-hooks") + "\n"
				require.NoError(t, os.WriteFile(included, []byte(content), 0600))
				testutil.Cmd(t, "git", "config", "--global", "include.path", included)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := testutil.IsolateHome(t)
			repo := testutil.InitRepo(t)
			worktree := testutil.AddWorktree(t, repo, "worktree-branch")
			tc.setup(t, home, repo)

			// A relative core.hooksPath resolves against the top of each working
			// tree, so a worktree can legitimately differ from the main checkout.
			// Both are checked for that reason.
			for _, workingTree := range []struct {
				name string
				path string
			}{
				{"main working tree", repo},
				{"worktree", worktree},
			} {
				t.Run(workingTree.name, func(t *testing.T) {
					gitDir, err := FindGitDir(workingTree.path)
					require.NoError(t, err)

					resolved := HookPath(gitDir, CommitMsgHook)
					assert.Equal(t, gitHookPathOracle(t, workingTree.path), resolved, "must match git's own resolution")
					assertGitRunsHook(t, workingTree.path, resolved)
				})
			}
		})
	}
}

// assertGitRunsHook installs a marker hook at path and checks that committing
// from workingTree executes it.
func assertGitRunsHook(t testing.TB, workingTree, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	marker := "maiao-hook-ran-" + strings.ReplaceAll(t.Name(), "/", "-")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\necho "+marker+" >&2\n"), 0700))

	out, err := exec.Command("git", "-C", workingTree, "commit", "--allow-empty", "-m", "trigger the hook").CombinedOutput()
	require.NoError(t, err, string(out))
	assert.Contains(t, string(out), marker, "git did not run the hook maiao resolved")
}

// TestHookPathFallsBackWithoutGit covers the path taken when git cannot be
// consulted, which is why the git directory computation is kept.
func TestHookPathFallsBackWithoutGit(t *testing.T) {
	testutil.IsolateHome(t)
	repo := testutil.InitRepo(t)
	worktree := testutil.AddWorktree(t, repo, "worktree-branch")
	repoGitDir, err := FindGitDir(repo)
	require.NoError(t, err)
	worktreeGitDir, err := FindGitDir(worktree)
	require.NoError(t, err)

	// core.hooksPath is deliberately set. The fallback cannot honour it, and
	// asserting the documented limitation is better than implying otherwise.
	testutil.Cmd(t, "git", "-C", repo, "config", "core.hooksPath", ".githooks")
	t.Setenv("PATH", "")

	assert.Equal(t, filepath.Join(repoGitDir, "hooks", "commit-msg"), HookPath(repoGitDir, CommitMsgHook))
	assert.Equal(t, filepath.Join(repoGitDir, "hooks", "commit-msg"), HookPath(worktreeGitDir, CommitMsgHook),
		"the fallback must still follow commondir out of a worktree")
}
