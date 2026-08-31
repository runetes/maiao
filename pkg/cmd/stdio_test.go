package cmd

import (
	"os"
	"testing"

	lgit "github.com/adevinta/maiao/pkg/git"
	"github.com/adevinta/maiao/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStdio replaces the process's standard output and error with temporary
// files and returns a function reporting what was written to each.
//
// The streams are swapped at the os.Stdout / os.Stderr level rather than through
// cobra's writers, because the point is to catch code that reaches for the real
// stream directly. Going through cobra would test the harness instead.
func captureStdio(t testing.TB) func() (string, string) {
	t.Helper()
	dir := t.TempDir()
	replace := func(name string, target **os.File) *os.File {
		f, err := os.Create(dir + "/" + name)
		require.NoError(t, err)
		original := *target
		t.Cleanup(func() { *target = original; f.Close() })
		*target = f
		return f
	}
	outFile := replace("stdout", &os.Stdout)
	errFile := replace("stderr", &os.Stderr)
	read := func(f *os.File) string {
		content, err := os.ReadFile(f.Name())
		require.NoError(t, err)
		return string(content)
	}
	return func() (string, string) { return read(outFile), read(errFile) }
}

// TestGuidanceGoesToStderr covers the declined-hook path, the one place a review
// writes prose before doing any network work.
//
// Standard output is reserved for output a caller parses, so guidance meant for a
// human belongs on standard error, where this codebase already writes its prompts.
func TestGuidanceGoesToStderr(t *testing.T) {
	testutil.IsolateHome(t)
	setBatch(t, false)
	repo := testutil.InitRepo(t)
	gitDir, err := lgit.FindGitDir(repo)
	require.NoError(t, err)
	stubConfirm(t, func(string) bool { return false })
	expectNoInstall(t)
	stdio := captureStdio(t)

	proceed, err := ensureCommitMsgHook(repo, gitDir)
	require.NoError(t, err)
	require.False(t, proceed)

	stdout, stderr := stdio()
	assert.Empty(t, stdout, "nothing may be written to standard output")
	assert.Contains(t, stderr, "missing change ids", "the guidance must still reach the user")
	assert.Contains(t, stderr, lgit.HookPath(gitDir, lgit.CommitMsgHook))
}
