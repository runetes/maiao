package credentials

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// writeKey generates an ed25519 key pair, writes the private half where ssh
// expects to read it, and returns the public half.
func writeKey(t *testing.T, path string) ssh.PublicKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	block, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(block), 0o600))

	signer, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	return signer
}

// sshHome lays out a HOME the way a user's machine looks: an ssh config naming
// an identity for host, the key it names, and a known_hosts entry.
func sshHome(t *testing.T, host string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".ssh")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	t.Setenv("HOME", home)

	writeKey(t, filepath.Join(dir, "the_configured_key"))
	hostKey := writeKey(t, filepath.Join(dir, "unrelated_host_key"))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "config"), []byte(fmt.Sprintf(
		"Host %s\n  User git\n  IdentityFile ~/.ssh/the_configured_key\n  IdentitiesOnly yes\n", host)), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "known_hosts"), []byte(fmt.Sprintf(
		"%s %s\n", host, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(hostKey))))), 0o600))
}

// TestClientConfigReadsTheIdentityFileWithoutAnAgent is the failure this
// replaced: `git review` stopped at "error creating SSH agent: SSH agent
// requested but SSH_AUTH_SOCK not-specified" against a host that plain `git`
// reaches without trouble, because go-git's default builder only ever asks the
// agent while ssh reads IdentityFile out of ~/.ssh/config.
func TestClientConfigReadsTheIdentityFileWithoutAnAgent(t *testing.T) {
	sshHome(t, "gitea.example.com")
	t.Setenv("SSH_AUTH_SOCK", "")

	endpoint, err := transport.NewEndpoint("git@gitea.example.com:owner/repo.git")
	require.NoError(t, err)

	cfg, err := (&GitAuth{Endpoint: endpoint}).ClientConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "git", cfg.User)
	assert.NotEmpty(t, cfg.Auth, "the configured identity must be offered")
}

// TestClientConfigVerifiesTheHostKey guards the one thing the ssh config library
// gets wrong for maiao's purposes: it builds its config with
// ssh.InsecureIgnoreHostKey, and maiao's whole host-key story — the mismatch
// error, the trust-on-first-use opt-in — rests on known_hosts being consulted.
func TestClientConfigVerifiesTheHostKey(t *testing.T) {
	sshHome(t, "gitea.example.com")
	unknown := writeKey(t, filepath.Join(t.TempDir(), "some_other_key"))

	endpoint, err := transport.NewEndpoint("git@gitea.example.com:owner/repo.git")
	require.NoError(t, err)
	cfg, err := (&GitAuth{Endpoint: endpoint}).ClientConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg.HostKeyCallback)

	err = cfg.HostKeyCallback("gitea.example.com:22", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}, unknown)

	assert.Error(t, err, "a key that is not in known_hosts must be refused")
}

// TestClientConfigFallsBackToTheAgent covers the users this change must not
// break: the config reader rejects a file containing an option it does not
// implement, and `ForwardAgent yes` under `Host *` is common enough that it
// would otherwise take maiao away from everyone who has one. They reached their
// remote through the agent before, so the agent is what they fall back to — here
// there is none, and the error has to name both halves of the failure.
func TestClientConfigFallsBackToTheAgent(t *testing.T) {
	sshHome(t, "gitea.example.com")
	require.NoError(t, os.WriteFile(filepath.Join(os.Getenv("HOME"), ".ssh", "config"),
		[]byte("Host *\n  ForwardAgent yes\n"), 0o600))
	t.Setenv("SSH_AUTH_SOCK", "")

	endpoint, err := transport.NewEndpoint("git@gitea.example.com:owner/repo.git")
	require.NoError(t, err)

	cfg, err := (&GitAuth{Endpoint: endpoint}).ClientConfig()

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "ForwardAgent", "the option that stopped the config being read")
	assert.Contains(t, err.Error(), "SSH_AUTH_SOCK", "and why the fallback did not save it")
}

func TestClientConfigWithoutEndpoint(t *testing.T) {
	cfg, err := (&GitAuth{}).ClientConfig()

	assert.Error(t, err)
	assert.Nil(t, cfg)
}
