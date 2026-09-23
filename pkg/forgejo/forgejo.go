package forgejo

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/runetes/maiao/pkg/credentials"
	"github.com/runetes/maiao/pkg/gitea"
)

type Forgejo struct {
	gitea.BaseClient
}

func NewForgejoUpserter(ctx context.Context, endpoint *transport.Endpoint) (*Forgejo, error) {
	orgRepo := strings.Split(strings.Trim(endpoint.Path, "/"), "/")
	if len(orgRepo) != 2 {
		return nil, fmt.Errorf("invalid repository path: %s (expected owner/repo)", endpoint.Path)
	}

	owner := orgRepo[0]
	repo := strings.TrimSuffix(orgRepo[1], ".git")

	credGetter := credentials.CredentialGetterForProvider("forgejo")
	cred, err := credGetter.CredentialForHost(endpoint.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials for %s: %w", endpoint.Host, err)
	}

	apiBase := fmt.Sprintf("https://%s/api/v1", endpoint.Host)

	client := &http.Client{Transport: gitea.NewAuthTransport(cred, nil)}

	return &Forgejo{
		BaseClient: gitea.BaseClient{
			Host:       endpoint.Host,
			Owner:      owner,
			Repository: repo,
			HTTPClient: client,
			APIBase:    apiBase,
		},
	}, nil
}
