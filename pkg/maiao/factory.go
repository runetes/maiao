package maiao

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/runetes/maiao/pkg/api"
	"github.com/runetes/maiao/pkg/bitbucket"
	"github.com/runetes/maiao/pkg/forgejo"
	"github.com/runetes/maiao/pkg/gitea"
	"github.com/runetes/maiao/pkg/gitlab"
	"github.com/runetes/maiao/pkg/log"
	"github.com/runetes/maiao/pkg/origin"
	"github.com/runetes/maiao/pkg/provider"
	"github.com/sirupsen/logrus"
)

// pullRequesterFor is a seam so that tests can reach the pull request logic without
// a provider to authenticate against.
var pullRequesterFor = newPullRequester

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
		case provider.Origin:
			r, err := origin.NewOriginUpserter(ctx, endpoint)
			if err != nil {
				log.ForContext(ctx).WithError(err).Errorf("failed to instantiate origin client")
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
