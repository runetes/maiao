package credentials

import (
	"os/exec"
	"strings"

	"github.com/runetes/maiao/pkg/log"
)

// GitConfigKeyringMode selects how maiao uses the OS password manager.
//
// Which password manager a machine has is a property of the machine, not of a
// checkout, so this is normally set once:
//
//	git config --global maiao.keyring enabled
const GitConfigKeyringMode = "maiao.keyring"

// gitConfigValue is a seam: tests replace it rather than writing to the
// developer's real git configuration.
var gitConfigValue = readGitConfigValue

func readGitConfigValue(key string) (string, error) {
	// --get searches system, global and local scopes, so the setting works both
	// machine-wide and per repository. git exits 1 when the key is unset.
	out, err := exec.Command("git", "config", "--get", key).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// KeyringModeFromGitConfig reads maiao.keyring. An unset, unreadable or
// unrecognised value means KeyringAuto: a credential lookup is not the place to
// fail over a preference.
func KeyringModeFromGitConfig() KeyringMode {
	raw, err := gitConfigValue(GitConfigKeyringMode)
	if err != nil {
		return KeyringAuto
	}
	mode, ok := ParseKeyringMode(raw)
	if !ok {
		log.Logger.WithField(GitConfigKeyringMode, strings.TrimSpace(raw)).
			Warnf("unknown value, expecting one of auto, enabled or disabled; using %s", KeyringAuto)
		return KeyringAuto
	}
	return mode
}

// keyringGetter opens the password manager for mode.
//
// It reports false when there is no password manager to add to the credential
// chain. Under KeyringAuto that is an ordinary state — a machine with no
// keychain, no secret-service and no pass — so it is not worth an error on
// every command; under KeyringEnabled the user asked for one, so say why.
func keyringGetter(mode KeyringMode) (CredentialGetter, bool) {
	if mode == KeyringDisabled {
		return nil, false
	}

	kr, err := NewKeyring(KeyringConfig(mode))
	if err != nil {
		entry := log.Logger.WithError(err).WithField(GitConfigKeyringMode, string(mode))
		if mode == KeyringEnabled {
			entry.Warn("no password manager available to store credentials")
		} else {
			entry.Debug("no password manager available, skipping it in the credential chain")
		}
		return nil, false
	}
	return kr, true
}
