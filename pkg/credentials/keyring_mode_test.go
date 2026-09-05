package credentials

import (
	"bytes"
	"errors"
	"testing"

	"github.com/adevinta/maiao/pkg/log"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withGitConfig makes `git config --get maiao.keyring` answer value for the
// duration of the test, so nothing here depends on the developer's own git
// configuration.
func withGitConfig(t *testing.T, value string, err error) {
	t.Helper()
	previous := gitConfigValue
	gitConfigValue = func(string) (string, error) { return value, err }
	t.Cleanup(func() { gitConfigValue = previous })
}

func TestKeyringModeFromGitConfig(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value string
		err   error
		want  KeyringMode
	}{
		{name: "unset falls back to auto", err: errors.New("exit status 1"), want: KeyringAuto},
		{name: "explicit auto", value: "auto\n", want: KeyringAuto},
		{name: "enabled", value: "enabled\n", want: KeyringEnabled},
		{name: "disabled", value: "disabled\n", want: KeyringDisabled},
		{name: "git bool true", value: "true\n", want: KeyringEnabled},
		{name: "unknown value falls back to auto", value: "yolo\n", want: KeyringAuto},
	} {
		t.Run(tt.name, func(t *testing.T) {
			withGitConfig(t, tt.value, tt.err)
			assert.Equal(t, tt.want, KeyringModeFromGitConfig())
		})
	}
}

func TestKeyringGetterDisabledOpensNothing(t *testing.T) {
	getter, ok := keyringGetter(KeyringDisabled)
	assert.False(t, ok)
	assert.Nil(t, getter)
}

// `git review -v 4` raises log.Logger to debug, and that is how a user finds out
// which backend answered. Logging through the package-level logrus logger
// instead would go to a different logger that -v never touches.
func TestKeyringGetterReportsTheBackendAtDebugLevel(t *testing.T) {
	out := &bytes.Buffer{}
	previousOut, previousLevel := log.Logger.Out, log.Logger.Level
	log.Logger.SetOutput(out)
	log.Logger.SetLevel(logrus.DebugLevel)
	t.Cleanup(func() {
		log.Logger.SetOutput(previousOut)
		log.Logger.SetLevel(previousLevel)
	})

	// Enabled rather than auto: the file backend guarantees something opens,
	// on every platform CI runs on.
	_, ok := keyringGetter(KeyringEnabled)
	require.True(t, ok)
	assert.Contains(t, out.String(), "opened password manager")
	assert.Contains(t, out.String(), "backend=")
}

// The password manager must come last: it is the only getter that asks the user
// for input, so anything it can be answered by must be tried first.
func TestCredentialGetterForProviderOrdersKeyringLast(t *testing.T) {
	withGitConfig(t, "disabled\n", nil)
	chain, ok := CredentialGetterForProvider("github").(ChainCredentialGetter)
	require.True(t, ok)
	require.Len(t, chain, 3, "keyring must be absent when maiao.keyring is disabled")
	assert.IsType(t, &EnvToken{}, chain[0])
	assert.IsType(t, &Netrc{}, chain[1])
	assert.IsType(t, &GitCredentials{}, chain[2])

	withGitConfig(t, "enabled\n", nil)
	chain, ok = CredentialGetterForProvider("github").(ChainCredentialGetter)
	require.True(t, ok)
	require.Len(t, chain, 4, "the file backend guarantees a keyring in enabled mode")
	assert.IsType(t, &Keyring{}, chain[3])
}
