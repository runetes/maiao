package gh

import (
	"sync"

	"github.com/runetes/maiao/pkg/credentials"
)

// DefaultCredentialGetter implements retrieving credentials for github.com.
//
// It delegates to the provider-aware chain rather than assembling its own: the
// two copies drifted apart once already, and the copy here was the one that
// still knew the keyring settings macOS and pass need.
var DefaultCredentialGetter credentials.CredentialGetter = &lazyGitHubCredentials{}

// lazyGitHubCredentials builds the credential chain on first use. Building it
// eagerly would shell out to git and open the OS password manager merely
// because something imported this package.
type lazyGitHubCredentials struct {
	once   sync.Once
	getter credentials.CredentialGetter
}

func (l *lazyGitHubCredentials) CredentialForHost(host string) (*credentials.Credentials, error) {
	l.once.Do(func() {
		l.getter = credentials.CredentialGetterForProvider("github")
	})
	return l.getter.CredentialForHost(host)
}
