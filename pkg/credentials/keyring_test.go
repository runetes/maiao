package credentials

import (
	"errors"
	"fmt"
	"testing"

	"github.com/99designs/keyring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseKeyringMode(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want KeyringMode
		ok   bool
	}{
		{"auto", KeyringAuto, true},
		{"enabled", KeyringEnabled, true},
		{"disabled", KeyringDisabled, true},
		{"AUTO", KeyringAuto, true},
		{"  enabled\n", KeyringEnabled, true},
		{"true", KeyringEnabled, true},
		{"false", KeyringDisabled, true},
		{"", "", false},
		{"maybe", "", false},
	} {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := ParseKeyringMode(tt.in)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// The keyring config regressed when CredentialGetterForProvider was introduced:
// it passed only ServiceName, dropping the PassPrefix and KeychainTrustApplication
// that pkg/github/credentials.go had always set. Losing PassPrefix hides every
// credential a `pass` user had already stored under maiao/, and losing
// KeychainTrustApplication makes macOS re-prompt for authorisation on every read.
func TestKeyringConfigKeepsBackendSpecificSettings(t *testing.T) {
	for _, mode := range []KeyringMode{KeyringAuto, KeyringEnabled} {
		t.Run(string(mode), func(t *testing.T) {
			cfg := KeyringConfig(mode)
			assert.Equal(t, "maiao", cfg.ServiceName)
			assert.Equal(t, "maiao/", cfg.PassPrefix, "pass entries must stay namespaced under maiao/")
			assert.True(t, cfg.KeychainTrustApplication, "macOS re-prompts on every read without this")
			assert.Equal(t, "maiao", cfg.LibSecretCollectionName)
		})
	}
}

// KWallet defaults its app id and folder to "keyring". maiao has never
// overridden them, so overriding them now would orphan every entry KDE users
// have already stored.
func TestKeyringConfigLeavesKWalletDefaults(t *testing.T) {
	cfg := KeyringConfig(KeyringEnabled)
	assert.Empty(t, cfg.KWalletAppID)
	assert.Empty(t, cfg.KWalletFolder)
}

// keyring's file backend opener never fails, so leaving AllowedBackends nil
// makes keyring.Open succeed with a fileKeyring{dir: ""} whose every Get
// returns `No directory provided for file keyring`. That is what a released
// macOS binary does today, because cross-compiling from Linux disables cgo and
// drops the Keychain backend entirely.
func TestKeyringConfigAutoExcludesFileBackend(t *testing.T) {
	cfg := KeyringConfig(KeyringAuto)
	require.NotEmpty(t, cfg.AllowedBackends, "nil AllowedBackends silently admits the file backend")
	assert.NotContains(t, cfg.AllowedBackends, keyring.FileBackend)
	assert.Contains(t, cfg.AllowedBackends, keyring.KeychainBackend)
	assert.Contains(t, cfg.AllowedBackends, keyring.SecretServiceBackend)
	assert.Contains(t, cfg.AllowedBackends, keyring.PassBackend)
}

// The kernel keyring does not survive a reboot, which makes it the wrong home
// for a long-lived access token. It sits ahead of pass in keyring's own
// priority order, so it has to be excluded explicitly.
func TestKeyringConfigNeverUsesKeyctl(t *testing.T) {
	for _, mode := range []KeyringMode{KeyringAuto, KeyringEnabled} {
		assert.NotContains(t, KeyringConfig(mode).AllowedBackends, keyring.KeyCtlBackend)
	}
}

// In `enabled` mode the file backend is the last resort, and it is only usable
// when it has somewhere to write and a way to ask for a passphrase.
func TestKeyringConfigEnabledHasUsableFileBackend(t *testing.T) {
	cfg := KeyringConfig(KeyringEnabled)
	require.NotEmpty(t, cfg.AllowedBackends)
	assert.Equal(t, keyring.FileBackend, cfg.AllowedBackends[len(cfg.AllowedBackends)-1], "file backend must stay last resort")
	assert.NotEmpty(t, cfg.FileDir, "an empty FileDir makes every Get fail with 'No directory provided for file keyring'")
	assert.NotNil(t, cfg.FilePasswordFunc, "a nil FilePasswordFunc panics on unlock")
}

// A full round trip through the least capable backend: it proves the fallback
// configuration actually stores and returns credentials rather than erroring.
func TestKeyringRoundTripsThroughFileBackend(t *testing.T) {
	cfg := KeyringConfig(KeyringEnabled)
	cfg.AllowedBackends = []keyring.BackendType{keyring.FileBackend}
	cfg.FileDir = t.TempDir()
	cfg.FilePasswordFunc = keyring.FixedStringPrompt("test-passphrase")

	kr, err := keyring.Open(cfg)
	require.NoError(t, err)

	store := &Keyring{kr: kr, prompt: fixedPrompt("someone", "s3cret")}
	got, err := store.CredentialForHost("github.com")
	require.NoError(t, err)
	assert.Equal(t, &Credentials{Username: "someone", Password: "s3cret"}, got)

	// A second reader must find the stored value without prompting again.
	reader := &Keyring{kr: kr, prompt: failingPrompt(t)}
	got, err = reader.CredentialForHost("github.com")
	require.NoError(t, err)
	assert.Equal(t, &Credentials{Username: "someone", Password: "s3cret"}, got)
}

// Storing is the whole point of the getter. Swallowing the error means the user
// is silently re-prompted on every single run with nothing to explain why.
func TestKeyringReportsStoreFailure(t *testing.T) {
	boom := errors.New("keychain is locked")
	store := &Keyring{
		kr:     &fakeKeyring{setErr: boom},
		prompt: fixedPrompt("someone", "s3cret"),
	}
	_, err := store.CredentialForHost("github.com")
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
}

// Only a genuinely absent entry may trigger the interactive prompt. Any other
// backend error means the store is there but unusable, and prompting would
// destroy credentials the user already has.
func TestKeyringDoesNotPromptOnBackendError(t *testing.T) {
	boom := errors.New("No directory provided for file keyring")
	store := &Keyring{
		kr:     &fakeKeyring{getErr: boom},
		prompt: failingPrompt(t),
	}
	_, err := store.CredentialForHost("github.com")
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
}

// Backends are free to wrap ErrKeyNotFound; comparing with == misses those.
func TestKeyringPromptsOnWrappedKeyNotFound(t *testing.T) {
	store := &Keyring{
		kr:     &fakeKeyring{getErr: fmt.Errorf("pass: %w", keyring.ErrKeyNotFound)},
		prompt: fixedPrompt("someone", "s3cret"),
	}
	got, err := store.CredentialForHost("github.com")
	require.NoError(t, err)
	assert.Equal(t, &Credentials{Username: "someone", Password: "s3cret"}, got)
}

func fixedPrompt(username, password string) promptFunc {
	answers := []string{username, password}
	i := 0
	return func(string, rune) (string, error) {
		if i >= len(answers) {
			return "", errors.New("unexpected prompt")
		}
		v := answers[i]
		i++
		return v, nil
	}
}

func failingPrompt(t *testing.T) promptFunc {
	t.Helper()
	return func(label string, _ rune) (string, error) {
		t.Errorf("unexpected prompt: %s", label)
		return "", errors.New("must not prompt")
	}
}

type fakeKeyring struct {
	keyring.Keyring
	items  map[string]keyring.Item
	getErr error
	setErr error
}

func (f *fakeKeyring) Get(key string) (keyring.Item, error) {
	if f.getErr != nil {
		return keyring.Item{}, f.getErr
	}
	if item, ok := f.items[key]; ok {
		return item, nil
	}
	return keyring.Item{}, keyring.ErrKeyNotFound
}

func (f *fakeKeyring) Set(item keyring.Item) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.items == nil {
		f.items = map[string]keyring.Item{}
	}
	f.items[item.Key] = item
	return nil
}
