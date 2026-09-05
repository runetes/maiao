package credentials

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/99designs/keyring"
	"github.com/adevinta/maiao/pkg/log"
	"github.com/manifoldco/promptui"
)

// keyringServiceName names maiao in every backend: the macOS Keychain service,
// the secret-service collection and the pass path prefix all derive from it.
// Changing it orphans every credential users have already stored.
const keyringServiceName = "maiao"

// keyringFileDir is where the last-resort encrypted file store lives. keyring
// expands the leading ~ itself.
const keyringFileDir = "~/.maiao/keyring"

// KeyringMode says whether the OS password manager takes part in the credential
// chain. It is read from `git config maiao.keyring`.
type KeyringMode string

const (
	// KeyringAuto uses an OS password manager when one is actually available and
	// stays out of the chain when none is. It is the default.
	KeyringAuto KeyringMode = "auto"
	// KeyringEnabled always stores credentials in a password manager, falling
	// back to a passphrase-protected file store when the OS offers none.
	KeyringEnabled KeyringMode = "enabled"
	// KeyringDisabled never uses a password manager.
	KeyringDisabled KeyringMode = "disabled"
)

// ParseKeyringMode reads a `maiao.keyring` git config value. It also accepts
// the booleans git itself understands, so `git config --type=bool` round-trips.
func ParseKeyringMode(value string) (KeyringMode, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(KeyringAuto):
		return KeyringAuto, true
	case string(KeyringEnabled), "true", "on", "yes", "1":
		return KeyringEnabled, true
	case string(KeyringDisabled), "false", "off", "no", "0":
		return KeyringDisabled, true
	}
	return "", false
}

// osBackends are the OS-managed credential stores, in keyring's own priority
// order.
//
// keyring.KeyCtlBackend is deliberately absent: the Linux kernel keyring does
// not survive a reboot, which makes it the wrong home for a long-lived access
// token, and keyring ranks it ahead of pass.
//
// keyring.FileBackend is absent too. Its opener never fails, so leaving
// AllowedBackends nil means keyring.Open always succeeds — handing back a file
// store with no directory whose every Get fails with "No directory provided for
// file keyring" rather than reporting that no password manager was found.
var osBackends = []keyring.BackendType{
	keyring.WinCredBackend,
	keyring.KeychainBackend,
	keyring.SecretServiceBackend,
	keyring.KWalletBackend,
	keyring.PassBackend,
}

// KeyringConfig returns the keyring configuration shared by every maiao
// credential store. Having a single definition is the point: the settings below
// were lost once already by a second copy drifting from the first.
func KeyringConfig(mode KeyringMode) keyring.Config {
	cfg := keyring.Config{
		ServiceName: keyringServiceName,

		// macOS. Without this the item is created with an empty trusted
		// application list, and the Keychain asks the user to authorise the read
		// every single time.
		KeychainTrustApplication: true,

		// pass. Entries live at <store>/maiao/<host>-credentials; dropping the
		// prefix hides everything a user has already stored.
		PassPrefix: keyringServiceName + "/",

		// secret-service. This is already keyring's default (it falls back to
		// ServiceName), stated explicitly so it cannot drift.
		LibSecretCollectionName: keyringServiceName,

		// KWalletAppID and KWalletFolder are left at keyring's "keyring"
		// defaults on purpose: maiao has never set them, and setting them now
		// would orphan KDE users' existing entries.

		// KeychainSynchronizable is not set: keyring v1.2.2 declares the field
		// but never assigns it in the keychain opener, so it has no effect.

		AllowedBackends: osBackends,
	}

	if mode == KeyringEnabled {
		cfg.AllowedBackends = append(append([]keyring.BackendType{}, osBackends...), keyring.FileBackend)
		cfg.FileDir = keyringFileDir
		cfg.FilePasswordFunc = keyring.TerminalPrompt
	}

	return cfg
}

// promptFunc asks the user for a single value. mask is echoed in place of the
// typed characters, or 0 to echo them as typed.
type promptFunc func(label string, mask rune) (string, error)

func promptUser(label string, mask rune) (string, error) {
	prompt := promptui.Prompt{
		Label:  label,
		Mask:   mask,
		Stdin:  os.Stdin,
		Stdout: os.Stderr,
	}
	return prompt.Run()
}

// Keyring resolves credentials from an OS password manager, asking the user for
// them once and storing them for later runs.
type Keyring struct {
	kr     keyring.Keyring
	prompt promptFunc
}

var _ CredentialGetter = &Keyring{}

func (k *Keyring) CredentialForHost(h string) (*Credentials, error) {
	secretKey := fmt.Sprintf("%s-credentials", h)

	item, err := k.kr.Get(secretKey)
	switch {
	case err == nil:
		creds := &Credentials{}
		if err := json.NewDecoder(bytes.NewReader(item.Data)).Decode(creds); err != nil {
			return nil, fmt.Errorf("stored credentials for %s are corrupted: %w", h, err)
		}
		return creds, nil

	case errors.Is(err, keyring.ErrKeyNotFound):
		// Nothing stored yet: ask, then remember.

	default:
		// The store exists but is unusable — locked, misconfigured, no
		// directory. Prompting here would ask the user to retype credentials
		// they already have, into a store that cannot keep them.
		return nil, fmt.Errorf("cannot read credentials for %s from the password manager: %w", h, err)
	}

	creds, err := k.askFor(h)
	if err != nil {
		return nil, err
	}

	data := bytes.NewBuffer(nil)
	if err := json.NewEncoder(data).Encode(creds); err != nil {
		return nil, fmt.Errorf("failed to encode credentials for %s: %w", h, err)
	}

	err = k.kr.Set(keyring.Item{
		Key:         secretKey,
		Label:       h,
		Description: fmt.Sprintf("json encoded user/password to access %s", h),
		Data:        data.Bytes(),
	})
	if err != nil {
		// Returning the credentials and hiding this would silently re-prompt on
		// every single run, with nothing to explain why.
		return nil, fmt.Errorf("failed to store credentials for %s in the password manager: %w", h, err)
	}

	return creds, nil
}

func (k *Keyring) askFor(host string) (*Credentials, error) {
	creds := &Credentials{}
	var err error

	creds.Username, err = k.prompt(fmt.Sprintf("username for %s", host), 0)
	if err != nil {
		return nil, err
	}
	creds.Password, err = k.prompt(fmt.Sprintf("password for %s (usually your Personal Access Token)", host), '*')
	if err != nil {
		return nil, err
	}
	return creds, nil
}

// NewKeyring opens the password manager described by cfg. It returns an error
// when none of the allowed backends is available, which is how callers learn
// that this machine has no password manager to offer.
func NewKeyring(cfg keyring.Config) (CredentialGetter, error) {
	kr, err := keyring.Open(cfg)
	if err != nil {
		return nil, err
	}
	log.Logger.WithField("backend", fmt.Sprintf("%T", kr)).Debug("opened password manager")
	return &Keyring{kr: kr, prompt: promptUser}, nil
}

func MustNewKeyring(cfg keyring.Config) CredentialGetter {
	c, err := NewKeyring(cfg)
	if err != nil {
		panic(err)
	}
	return c
}
