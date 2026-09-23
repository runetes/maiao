package credentials

import (
	"errors"
	"net/http"

	"github.com/go-git/go-git/v5/plumbing/transport"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/runetes/maiao/pkg/log"
	"golang.org/x/crypto/ssh"
)

type GitAuth struct {
	Endpoint    *transport.Endpoint
	Credentials CredentialGetter
}

var _ transport.AuthMethod = &GitAuth{}
var _ gitssh.AuthMethod = &GitAuth{}

func (a *GitAuth) SetAuth(r *http.Request) {
	creds, err := a.Credentials.CredentialForHost(r.Host)
	if err != nil || creds == nil {
		log.Logger.WithField("host", r.Host).WithError(err).Infof("failed to find credentials")
		return
	}
	r.SetBasicAuth(creds.Username, creds.Password)
}

func (a *GitAuth) ClientConfig() (*ssh.ClientConfig, error) {
	if a.Endpoint == nil {
		return nil, errors.New("no endpoint found")
	}
	sshauthMethod, err := gitssh.DefaultAuthBuilder(a.Endpoint.User)
	if err != nil {
		return nil, err
	}
	return sshauthMethod.ClientConfig()
}

func (a *GitAuth) Name() string {
	return "auth from credentials"
}

func (a *GitAuth) String() string {
	return "auth from credentials"
}
