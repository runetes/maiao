package git

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/adevinta/maiao/pkg/system"
	"github.com/spf13/afero"
)

const (
	CommitMsgHook Hook = "commit-msg"
)

type Hook string

const (
	ApplyPatchMsgHook    Hook = "applypatch-msg"
	PostApplyPatchHook   Hook = "post-applypatch"
	PostCheckoutHook     Hook = "post-checkout"
	PostCommitHook       Hook = "post-commit"
	PostMergeHook        Hook = "post-merge"
	PostRewriteHook      Hook = "post-rewrite"
	PrepareCommitMsgHook Hook = "prepare-commit-msg"
	PreApplyPatchHook    Hook = "pre-applypatch"
	PreAutoGCHook        Hook = "pre-auto-gc"
	PrePushHook          Hook = "pre-push"
	PreRebaseHook        Hook = "pre-rebase"
)

// HookPath returns the path where git looks for the given hook.
//
// git is asked directly, because the location depends on core.hooksPath, which
// may be set in any config scope, pulled in through an include directive, or
// expressed relative to the top of the working tree. Only git knows the
// precedence rules, and a hook written anywhere else is silently never run.
//
// When git cannot be consulted, the location is computed from the git
// directory, which covers the common case of core.hooksPath being unset.
func HookPath(gitDir string, hook Hook) string {
	gitDir = filepath.Clean(gitDir)
	if path, err := gitHookPath(gitDir, hook); err == nil {
		return path
	}
	return gitDirHookPath(gitDir, hook)
}

// gitHookPath resolves the hook location by asking git.
//
// git runs from inside the working tree rather than against the git directory,
// for two reasons: a relative core.hooksPath is resolved against the top of the
// working tree, and passing --git-dir without --work-tree makes git treat the
// current directory as the working tree, which would resolve a relative
// core.hooksPath against wherever maiao happens to have been invoked.
//
// rev-parse reports --git-path relative to that directory whenever it can, so a
// relative answer is joined back with it.
func gitHookPath(gitDir string, hook Hook) (string, error) {
	dir, err := workingTreeDir(gitDir)
	if err != nil {
		return "", err
	}
	c := exec.Command("git", "-C", dir, "rev-parse", "--absolute-git-dir", "--git-path", filepath.Join("hooks", string(hook)))
	out := bytes.NewBuffer(nil)
	c.Stdout = out
	// Failing here is expected: git may be absent, or gitDir may live on a
	// filesystem git cannot see, as it does in tests. The caller falls back, so
	// git's complaint is not worth showing to the user.
	c.Stderr = io.Discard
	if err := c.Run(); err != nil {
		return "", err
	}
	lines := strings.Split(strings.Trim(out.String(), " \n"), "\n")
	if len(lines) != 2 {
		return "", fmt.Errorf("unexpected git rev-parse output: %q", out.String())
	}
	// Guard against git resolving a repository that merely contains dir, which
	// happens when gitDir does not exist on the filesystem git reads.
	if !samePath(lines[0], gitDir) {
		return "", fmt.Errorf("git resolved git dir %q, expected %q", lines[0], gitDir)
	}
	path := lines[1]
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return path, nil
}

// workingTreeDir returns a directory inside the working tree gitDir belongs to.
// For a linked worktree, $GIT_DIR/gitdir holds the path of the worktree's .git
// file. Otherwise the git directory sits at the root of the working tree.
func workingTreeDir(gitDir string) (string, error) {
	data, err := afero.ReadFile(system.DefaultFileSystem, filepath.Join(gitDir, "gitdir"))
	if err != nil {
		return filepath.Dir(gitDir), nil
	}
	target := strings.Trim(string(data), " \n\r")
	if target == "" {
		return filepath.Dir(gitDir), nil
	}
	return filepath.Dir(target), nil
}

// samePath reports whether two paths denote the same location, tolerating
// symlinked parents such as macOS /tmp, which git reports as /private/tmp.
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	resolvedA, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false
	}
	resolvedB, err := filepath.EvalSymlinks(b)
	if err != nil {
		return false
	}
	return resolvedA == resolvedB
}

// gitDirHookPath computes the hook location from the git directory alone,
// following the worktree commondir indirection. It cannot account for
// core.hooksPath, so it is only used when git is unavailable.
func gitDirHookPath(gitDir string, hook Hook) string {
	commonDirPath := filepath.Join(gitDir, "commondir")
	_, err := system.DefaultFileSystem.Stat(commonDirPath)
	if err == nil {
		fd, err := system.DefaultFileSystem.Open(commonDirPath)
		if err == nil {
			defer fd.Close()
			bytes, err := ioutil.ReadAll(fd)
			if err == nil {
				gitDir = filepath.Join(gitDir, strings.TrimSpace(string(bytes)))
			}
		}
	}
	return filepath.Join(gitDir, "hooks", string(hook))
}
