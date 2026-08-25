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

	client := &http.Client{
		Transport: &tokenTransport{
			token:    cred.Password,
			delegate: http.DefaultTransport,
		},
	}

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

type tokenTransport struct {
	token    string
	delegate http.RoundTripper
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "token "+t.token)
	return t.delegate.RoundTrip(req)
}
