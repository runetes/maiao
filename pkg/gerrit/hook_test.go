package gerrit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adevinta/maiao/pkg/system"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// changeIDHook stands in for the gerrit hook: what identifies it is that it
	// deals in Change-Ids, not the rest of the script.
	changeIDHook = `#!/bin/sh
echo "Change-Id: I0000000000000000000000000000000000000000" >> "$1"
`
	// foreignHook is a commit message hook of the kind husky or lefthook installs,
	// which lands at exactly the path maiao resolves to.
	foreignHook = `#!/bin/sh
npx --no -- commitlint --edit "$1"
`
)

func TestStateAt(t *testing.T) {
	fs := afero.NewMemMapFs()
	system.DefaultFileSystem = fs
	t.Cleanup(system.Reset)

	for _, test := range []struct {
		name    string
		content string
		state   HookState
	}{
		{"the gerrit hook", changeIDHook, MaiaoHook},
		{"a hook chaining to maiao's", foreignHook + ChainCall(ChainedHookName) + "\n", MaiaoHook},
		{"a hook manager's own hook", foreignHook, ForeignHook},
		{"an empty file", "", ForeignHook},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join("/repo", test.name, "commit-msg")
			system.EnsureTestFileContent(t, fs, path, test.content)
			assert.Equal(t, test.state, StateAt(path))
		})
	}

	t.Run("nothing at all", func(t *testing.T) {
		assert.Equal(t, NoHook, StateAt("/repo/absent/commit-msg"))
	})

	// The state is logged, and that log is what someone whose commits have no
	// Change-Id ends up reading.
	t.Run("every state has a name", func(t *testing.T) {
		for _, state := range []HookState{NoHook, MaiaoHook, ForeignHook} {
			assert.NotContains(t, state.String(), "unknown")
		}
	})
}

func TestChainable(t *testing.T) {
	fs := afero.NewMemMapFs()
	system.DefaultFileSystem = fs
	t.Cleanup(system.Reset)

	for _, test := range []struct {
		shebang   string
		chainable bool
	}{
		{"#!/bin/sh", true},
		{"#!/bin/sh -e", true},
		{"#!/bin/bash", true},
		{"#!/usr/bin/env bash", true},
		{"#!/usr/bin/env zsh", true},
		// The call would be inserted as POSIX shell into something that is not a
		// shell, breaking a working hook outright.
		{"#!/usr/bin/env python3", false},
		{"#!/usr/bin/perl", false},
		// Ends in "sh" but shares none of the syntax.
		{"#!/usr/bin/env fish", false},
		{"echo no shebang at all", false},
	} {
		t.Run(test.shebang, func(t *testing.T) {
			path := filepath.Join("/chainable", test.shebang, "commit-msg")
			system.EnsureTestFileContent(t, fs, path, test.shebang+"\necho hello\n")
			assert.Equal(t, test.chainable, Chainable(path))
		})
	}

	t.Run("nothing at all", func(t *testing.T) {
		assert.False(t, Chainable("/chainable/absent/commit-msg"))
	})
}

func TestChainTo(t *testing.T) {
	fs := afero.NewMemMapFs()
	system.DefaultFileSystem = fs
	t.Cleanup(system.Reset)

	path := "/repo/.git/hooks/commit-msg"
	system.EnsureTestFileContent(t, fs, path, foreignHook)
	require.NoError(t, fs.Chmod(path, 0755))
	require.NoError(t, ChainTo(path, ChainedHookName))

	content, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	chained := string(content)

	assert.Contains(t, chained, ChainCall(ChainedHookName))
	assert.Contains(t, chained, `npx --no -- commitlint --edit "$1"`,
		"what the hook already did must survive")
	assert.Equal(t, MaiaoHook, StateAt(path), "a chained hook counts as installed")

	t.Run("the call goes after the shebang", func(t *testing.T) {
		lines := strings.Split(chained, "\n")
		require.Greater(t, len(lines), 3)
		assert.Equal(t, "#!/bin/sh", lines[0], "the shebang must stay on the first line")
		// Not at the end: a hook whose last line is `exit 0`, or that hands over
		// with `exec`, never reaches anything appended to it.
		assert.Less(t, indexOfCall(t, lines), len(lines)-2, "the call must not be last")
	})

	t.Run("the mode is preserved", func(t *testing.T) {
		info, err := fs.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0755), info.Mode().Perm(),
			"a hook that stops being executable stops running")
	})

	t.Run("chaining twice does not add the call twice", func(t *testing.T) {
		require.NoError(t, ChainTo(path, ChainedHookName))
		again, err := afero.ReadFile(fs, path)
		require.NoError(t, err)
		assert.Equal(t, 1, strings.Count(string(again), ChainCall(ChainedHookName)),
			"the hook is looked at again on every review")
	})

	t.Run("a hook that is not a shell script is left alone", func(t *testing.T) {
		python := "/repo/.git/hooks/python-commit-msg"
		system.EnsureTestFileContent(t, fs, python, "#!/usr/bin/env python3\nprint('hi')\n")
		require.Error(t, ChainTo(python, ChainedHookName))
		content, err := afero.ReadFile(fs, python)
		require.NoError(t, err)
		assert.Equal(t, "#!/usr/bin/env python3\nprint('hi')\n", string(content))
	})
}

// TestChainedHookRuns runs the chained hook the way git does, because the point of
// the call is not that it is present in the file but that both hooks execute.
func TestChainedHookRuns(t *testing.T) {
	dir := t.TempDir()
	hooks := filepath.Join(dir, "hooks")
	require.NoError(t, os.MkdirAll(hooks, 0700))

	hook := filepath.Join(hooks, "commit-msg")
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\necho theirs-ran >&2\nexit 0\n"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(hooks, ChainedHookName), []byte(changeIDHook), 0755))
	require.NoError(t, ChainTo(hook, ChainedHookName))

	message := filepath.Join(dir, "COMMIT_EDITMSG")
	require.NoError(t, os.WriteFile(message, []byte("a commit message\n"), 0600))

	// git invokes a hook from the top of the working tree, by a path that is not
	// the hook's own directory. That is why the call resolves itself from $0.
	c := exec.Command(hook, message)
	c.Dir = dir
	out, err := c.CombinedOutput()
	require.NoError(t, err, string(out))

	assert.Contains(t, string(out), "theirs-ran", "the existing hook must still run")
	produced, err := os.ReadFile(message)
	require.NoError(t, err)
	assert.Contains(t, string(produced), "Change-Id: I0000", "maiao's hook must run too")
}

func indexOfCall(t testing.TB, lines []string) int {
	t.Helper()
	for i, line := range lines {
		if strings.Contains(line, ChainCall(ChainedHookName)) {
			return i
		}
	}
	t.Fatal("the chained call is missing")
	return -1
}
