package ssh

import (
	"errors"
	"os"
	"testing"

	"github.com/adevinta/maiao/pkg/prompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolateKnownHosts points HOME at a temporary directory holding a known_hosts
// file that records host, and returns the recorded line plus a function
// reporting the file's current contents.
//
// The key is generated rather than hardcoded because ssh-keygen silently skips
// lines it cannot parse: with a placeholder key, an assertion that the entry
// survived would hold even if removal had been attempted.
func isolateKnownHosts(t testing.TB, host string) (string, func() string) {
	t.Helper()
	line := hostKey(t, host)
	path := writeKnownHosts(t, line)
	return line, func() string {
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		return string(content)
	}
}

func setBatch(t testing.TB, b bool) {
	t.Helper()
	original := prompt.Batch()
	t.Cleanup(func() { prompt.SetBatch(original) })
	prompt.SetBatch(b)
}

func setTrustNewHosts(t testing.TB, b bool) {
	t.Helper()
	original := trustNewHosts
	t.Cleanup(func() { trustNewHosts = original })
	trustNewHosts = b
}

// TestKeyMismatchIsNeverResolvedInBatchMode is the security critical case. A
// mismatch is indistinguishable from an intercepted connection, and resolving it
// deletes a key that was known to be good, so it must never happen unattended.
func TestKeyMismatchIsNeverResolvedInBatchMode(t *testing.T) {
	for _, trust := range []bool{false, true} {
		name := "without the trust opt-in"
		if trust {
			name = "even with the trust opt-in"
		}
		t.Run(name, func(t *testing.T) {
			recordedKey, knownHosts := isolateKnownHosts(t, "git.example.com")
			setBatch(t, true)
			setTrustNewHosts(t, trust)

			err := PromptAndFix("git.example.com", true)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrHostKeyMismatch)
			assert.Equal(t, recordedKey, knownHosts(),
				"the recorded key must survive: removing it unattended is what an interception would want")
			assert.Contains(t, err.Error(), "ssh-keygen -R", "the message must say how to resolve it deliberately")
		})
	}
}

// TestUnknownKeyRequiresOptInInBatchMode covers trust on first use, which is a
// lesser exposure but still must not happen by default.
func TestUnknownKeyRequiresOptInInBatchMode(t *testing.T) {
	recordedKey, knownHosts := isolateKnownHosts(t, "git.example.com")
	setBatch(t, true)
	setTrustNewHosts(t, false)

	err := PromptAndFix("new.example.com", false)

	require.Error(t, err)
	assert.ErrorIs(t, err, prompt.ErrNoInput)
	assert.NotErrorIs(t, err, ErrHostKeyMismatch, "an unknown key is not a mismatch")
	assert.Equal(t, recordedKey, knownHosts(), "known_hosts must be untouched")
	assert.Contains(t, err.Error(), TrustNewHostsEnvVar, "the message must name the opt-in")
	assert.Contains(t, err.Error(), "ssh-keyscan", "the message must offer the safer alternative")
}

// TestUnknownKeyWithOptInDoesNotPrompt checks that opting in actually skips the
// question rather than only changing the error.
func TestUnknownKeyWithOptInDoesNotPrompt(t *testing.T) {
	isolateKnownHosts(t, "git.example.com")
	setBatch(t, true)
	setTrustNewHosts(t, true)

	// ssh-keyscan cannot reach this host, so the call fails, but it fails having
	// tried to add the key rather than refusing to consider it.
	err := PromptAndFix("this-host-does-not-resolve.invalid", false)

	require.Error(t, err)
	assert.NotErrorIs(t, err, prompt.ErrNoInput, "the opt-in must be honoured rather than reported as missing input")
	assert.Contains(t, err.Error(), "add host keys")
}

func TestTrustNewHostsFromEnvironment(t *testing.T) {
	for value, expected := range map[string]bool{
		"1": true, "true": true, "TRUE": true, "yes": true, "on": true,
		"0": false, "false": false, "": false, "banana": false,
	} {
		t.Run("MAIAO_TRUST_NEW_SSH_HOSTS="+value, func(t *testing.T) {
			t.Setenv(TrustNewHostsEnvVar, value)
			assert.Equal(t, expected, envIsTrue(TrustNewHostsEnvVar))
		})
	}
}

// TestErrHostKeyMismatchIsDistinguishable makes sure a caller can tell the two
// failures apart, which is what lets them be given different exit codes.
func TestErrHostKeyMismatchIsDistinguishable(t *testing.T) {
	assert.False(t, errors.Is(ErrHostKeyMismatch, prompt.ErrNoInput))
	assert.False(t, errors.Is(prompt.ErrNoInput, ErrHostKeyMismatch))
}
