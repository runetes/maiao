package ssh

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsKnownHostsErrorNilError(t *testing.T) {
	_, _, ok := IsKnownHostsError(nil)
	assert.False(t, ok)
}

func TestIsKnownHostsErrorUnrelatedError(t *testing.T) {
	_, _, ok := IsKnownHostsError(errors.New("connection refused"))
	assert.False(t, ok)
}

func TestIsKnownHostsErrorKeyMismatch(t *testing.T) {
	err := errors.New("ssh: handshake failed: knownhosts: key mismatch")
	_, isMismatch, ok := IsKnownHostsError(err)
	assert.True(t, ok)
	assert.True(t, isMismatch)
}

func TestIsKnownHostsErrorKeyUnknown(t *testing.T) {
	err := errors.New("ssh: handshake failed: knownhosts: key is unknown")
	_, isMismatch, ok := IsKnownHostsError(err)
	assert.True(t, ok)
	assert.False(t, isMismatch)
}

func TestIsKnownHostsErrorExtractsHostFromDialTcp(t *testing.T) {
	err := errors.New("dial tcp gitlab.com:22: knownhosts: key mismatch")
	host, isMismatch, ok := IsKnownHostsError(err)
	assert.True(t, ok)
	assert.True(t, isMismatch)
	assert.Equal(t, "gitlab.com", host)
}

func TestIsKnownHostsErrorExtractsHostFromBrackets(t *testing.T) {
	err := errors.New("dial tcp [gitlab.com]:22: knownhosts: key mismatch")
	host, _, ok := IsKnownHostsError(err)
	assert.True(t, ok)
	assert.Equal(t, "gitlab.com", host)
}

func TestIsKnownHostsErrorReturnsEmptyHostWhenNotParseable(t *testing.T) {
	err := errors.New("knownhosts: key mismatch")
	host, _, ok := IsKnownHostsError(err)
	assert.True(t, ok)
	assert.Equal(t, "", host)
}

func TestAddHostKeysIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Verify ssh-keyscan is available
	err := addHostKeys("github.com")
	assert.NoError(t, err)
}
