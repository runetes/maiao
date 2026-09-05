//go:build darwin

package credentials

import (
	"fmt"
	"testing"

	"github.com/99designs/keyring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The macOS Keychain backend of 99designs/keyring is behind a `darwin && cgo`
// build tag. Cross-compiling darwin from Linux disables cgo, which drops the
// backend without any build error: keyring.Open still succeeds because the file
// backend's opener never fails, and every credential lookup then dies with
// "No directory provided for file keyring". Released macOS binaries shipped
// that way. Build darwin with CGO_ENABLED=1 on a macOS host.
func TestKeychainBackendIsAvailable(t *testing.T) {
	assert.Contains(t, keyring.AvailableBackends(), keyring.KeychainBackend,
		"this darwin build has no Keychain backend; it was compiled without cgo")
}

// The Keychain must also be what auto mode actually reaches for, ahead of pass
// and ahead of the file store. Opening a keyring neither reads nor writes, so
// this does not touch the developer's credentials.
func TestKeychainIsPreferredOnDarwin(t *testing.T) {
	kr, err := keyring.Open(KeyringConfig(KeyringAuto))
	require.NoError(t, err, "macOS always has a Keychain to open")
	assert.Equal(t, "*keyring.keychain", fmt.Sprintf("%T", kr))
}
