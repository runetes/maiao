package ssh

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hostKey generates a real key pair and returns a known_hosts line for host.
//
// A syntactically invalid key is not good enough here: ssh-keygen skips lines it
// cannot parse, so a placeholder would make removal assertions pass without
// removal ever having been attempted.
func hostKey(t testing.TB, host string) string {
	t.Helper()
	key := filepath.Join(t.TempDir(), "id_ed25519")
	require.NoError(t, exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "", "-f", key).Run())
	pub, err := os.ReadFile(key + ".pub")
	require.NoError(t, err)
	fields := strings.Fields(string(pub))
	require.GreaterOrEqual(t, len(fields), 2)
	return host + " " + fields[0] + " " + fields[1] + "\n"
}

// writeKnownHosts points HOME at a temporary directory containing content, and
// returns the path of the known_hosts file inside it.
func writeKnownHosts(t testing.TB, content string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".ssh", "known_hosts")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	return path
}

// TestRemoveHostKeysEditsTheFileAddHostKeysWrites is the regression test for
// removal and addition disagreeing about the file.
//
// ssh-keygen resolves the home directory from the passwd database, so without an
// explicit -f it edits the real user's known_hosts however HOME is set. That the
// entry disappears from the file under HOME is the whole assertion.
func TestRemoveHostKeysEditsTheFileAddHostKeysWrites(t *testing.T) {
	const host = "git.example.com"
	line := hostKey(t, host)
	path := writeKnownHosts(t, line)

	require.NoError(t, removeHostKeys(host))

	assert.Equal(t, path, knownHostsPath(), "removal and addition must agree on the path")
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(content), host,
		"the entry must be gone from the file under HOME, not from the passwd database home")
}

// TestRemoveHostKeysKeepsOtherHosts guards against -f widening the blast radius.
func TestRemoveHostKeysKeepsOtherHosts(t *testing.T) {
	stale := hostKey(t, "stale.example.com")
	keep := hostKey(t, "keep.example.com")
	path := writeKnownHosts(t, stale+keep)

	require.NoError(t, removeHostKeys("stale.example.com"))

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(content), "stale.example.com")
	assert.Contains(t, string(content), "keep.example.com", "unrelated hosts must survive")
}

// TestRemoveHostKeysWithoutKnownHostsFile covers a home directory that has never
// seen an SSH connection, where ssh-keygen -R -f would exit 255.
func TestRemoveHostKeysWithoutKnownHostsFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	assert.NoError(t, removeHostKeys("git.example.com"), "a missing file is nothing to remove, not a failure")
	assert.NoFileExists(t, knownHostsPath(), "no file should be created just to remove from it")
}
