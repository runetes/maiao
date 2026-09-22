package gitea

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/adevinta/maiao/pkg/credentials"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

type Gitea struct {
	BaseClient
}

func NewGiteaUpserter(ctx context.Context, endpoint *transport.Endpoint) (*Gitea, error) {
	orgRepo := strings.Split(strings.Trim(endpoint.Path, "/"), "/")
	if len(orgRepo) != 2 {
		return nil, fmt.Errorf("invalid repository path: %s (expected owner/repo)", endpoint.Path)
	}

	owner := orgRepo[0]
	repo := strings.TrimSuffix(orgRepo[1], ".git")

	credGetter := credentials.CredentialGetterForProvider("gitea")
	cred, err := credGetter.CredentialForHost(endpoint.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials for %s: %w", endpoint.Host, err)
	}

	apiBase := fmt.Sprintf("https://%s/api/v1", endpoint.Host)

	client := &http.Client{Transport: NewAuthTransport(cred, nil)}

	return &Gitea{
		BaseClient: BaseClient{
			Host:       endpoint.Host,
			Owner:      owner,
			Repository: repo,
			HTTPClient: client,
			APIBase:    apiBase,
		},
	}, nil
}

// NewAuthTransport authenticates every request to a Gitea-family API with cred.
// A nil delegate means http.DefaultTransport. Forgejo shares it: the two clients
// had a copy each, and a copy of the credential handling is how this package's
// keyring settings were lost once already.
func NewAuthTransport(cred *credentials.Credentials, delegate http.RoundTripper) http.RoundTripper {
	if delegate == nil {
		delegate = http.DefaultTransport
	}
	return &authTransport{username: cred.Username, token: cred.Password, delegate: delegate}
}

type authTransport struct {
	username string
	token    string
	delegate http.RoundTripper
}

// RoundTrip authenticates as basic auth whenever the credential names a user,
// because that is the only form Gitea accepts an account password in. It costs a
// token nothing: Gitea checks a basic password as an access token before trying
// it as a password (services/auth/basic.go), so the `login <user> password
// <token>` entry getting-started.md documents authenticates either way, while
// `Authorization: token` carries a token and nothing else.
func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Cloned, not modified: the caller still owns the request it passed, and a
	// RoundTripper may be handed it again on a redirect or a retry.
	authenticated := req.Clone(req.Context())
	if t.username != "" {
		authenticated.SetBasicAuth(t.username, t.token)
	} else {
		authenticated.Header.Set("Authorization", "token "+t.token)
	}
	return t.delegate.RoundTrip(authenticated)
}
