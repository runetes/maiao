package maiao

import (
	"context"
	"errors"
	"fmt"

	"github.com/adevinta/maiao/pkg/api"
	"github.com/adevinta/maiao/pkg/bitbucket"
	"github.com/adevinta/maiao/pkg/forgejo"
	"github.com/adevinta/maiao/pkg/gitea"
	"github.com/adevinta/maiao/pkg/gitlab"
	"github.com/adevinta/maiao/pkg/log"
	"github.com/adevinta/maiao/pkg/provider"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/sirupsen/logrus"
)

func newPullRequester(ctx context.Context, remote *git.Remote, repoPath string) (api.PullRequester, error) {
	var lastErr error
	for _, u := range remote.Config().URLs {
		ctx := log.WithContextFields(ctx, logrus.Fields{"remote-url": u})
		endpoint, err := transport.NewEndpoint(u)
		if err != nil {
			log.ForContext(ctx).WithError(err).Errorf("failed to parse remote")
			lastErr = err
			continue
		}

		providerType, err := provider.Detect(endpoint.Host, repoPath)
		if err != nil {
			log.ForContext(ctx).WithError(err).Errorf("failed to detect provider")
			lastErr = err
			continue
		}

		switch providerType {
		case provider.GitHub:
			r, err := api.NewGitHubUpserter(ctx, endpoint)
			if err != nil {
				log.ForContext(ctx).WithError(err).Errorf("failed to instantiate github client")
				lastErr = err
				continue
			}
			return r, nil
		case provider.GitLab:
			r, err := gitlab.NewGitLabUpserter(ctx, endpoint)
			if err != nil {
				log.ForContext(ctx).WithError(err).Errorf("failed to instantiate gitlab client")
				lastErr = err
				continue
			}
			return r, nil
		case provider.Gitea:
			r, err := gitea.NewGiteaUpserter(ctx, endpoint)
			if err != nil {
				log.ForContext(ctx).WithError(err).Errorf("failed to instantiate gitea client")
				lastErr = err
				continue
			}
			return r, nil
		case provider.Forgejo:
			r, err := forgejo.NewForgejoUpserter(ctx, endpoint)
			if err != nil {
				log.ForContext(ctx).WithError(err).Errorf("failed to instantiate forgejo client")
				lastErr = err
				continue
			}
			return r, nil
		case provider.Bitbucket:
			r, err := bitbucket.NewBitbucketUpserter(ctx, endpoint)
			if err != nil {
				log.ForContext(ctx).WithError(err).Errorf("failed to instantiate bitbucket client")
				lastErr = err
				continue
			}
			return r, nil
		default:
			return nil, fmt.Errorf("provider %q is not yet supported", providerType)
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("failed to create provider client: %w", lastErr)
	}
	return nil, errors.New("no supported provider found for remote")
}
