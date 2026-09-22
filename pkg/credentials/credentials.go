package credentials

import "fmt"

// Credentials defines the authentication credentials values
type Credentials struct {
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
}

// CredentialGetter defines to implement to get credentials
type CredentialGetter interface {
	CredentialForHost(string) (*Credentials, error)
}

type ChainCredentialGetter []CredentialGetter

// CredentialForHost returns the first credential of the chain that can actually
// authenticate, and the reasons every other getter could not produce one.
//
// A getter answering with an empty password counts as no answer. It used to end
// the chain, so a half-filled source shadowed the ones behind it and maiao sent
// an unauthenticated request; forges answer that with 404 on a private
// repository, an error that points nowhere near the credentials.
func (c ChainCredentialGetter) CredentialForHost(host string) (*Credentials, error) {
	errors := Errors{}
	for _, getter := range c {
		cred, err := getter.CredentialForHost(host)
		if err != nil {
			errors = append(errors, err)
			continue
		}
		if cred == nil || cred.Password == "" {
			errors = append(errors, fmt.Errorf("%T returned no password for %s", getter, host))
			continue
		}
		return cred, nil
	}
	return nil, errors
}

type Errors []error

// Error implements the error interface
func (e Errors) Error() string {
	msg := ""
	sep := ""
	for _, err := range e {
		msg = fmt.Sprintf("%s%s%s", msg, sep, err.Error())
		sep = "\n"
	}
	return msg
}
