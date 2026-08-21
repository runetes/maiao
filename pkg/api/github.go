package api

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	gh "github.com/adevinta/maiao/pkg/github"
	"github.com/adevinta/maiao/pkg/log"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/google/go-github/v90/github"
	"github.com/shurcooL/githubv4"
	"github.com/sirupsen/logrus"
)

// GitHub implements the PullRequester interface allowing to create pull requests for a given repository
type GitHub struct {
	GraphQLClient *api.GraphQLClient
	*github.Client
	Host         string
	Owner        string
	Repository   string
	stackManager *GitHubStackManager
}

// RepoName implements the ghrepo.Interface interface required to call the github graphql API from https://github.com/cli/cli
// See https://github.com/cli/cli/blob/dc804d928714120a3f4b53f78847aec7ba282c63/internal/ghrepo/repo.go#L14
func (g *GitHub) RepoName() string {
	return g.Repository
}

// RepoHost implements the ghrepo.Interface interface required to call the github graphql API from https://github.com/cli/cli
// See https://github.com/cli/cli/blob/dc804d928714120a3f4b53f78847aec7ba282c63/internal/ghrepo/repo.go#L14
func (g *GitHub) RepoOwner() string {
	return g.Owner
}

// RepoHost implements the ghrepo.Interface interface required to call the github graphql API from https://github.com/cli/cli
// See https://github.com/cli/cli/blob/dc804d928714120a3f4b53f78847aec7ba282c63/internal/ghrepo/repo.go#L14
func (g *GitHub) RepoHost() string {
	return g.Host
}

// Ensure ensures a PR is opened for the head branch
func (g *GitHub) Ensure(ctx context.Context, options PullRequestOptions) (*PullRequest, bool, error) {
	ctx = log.WithContextFields(ctx, logrus.Fields{
		"context":    "ensuring existing pull request",
		"owner":      g.Owner,
		"repository": g.Repository,
		"prOptions":  options,
	})
	prs, _, err := g.PullRequests.List(context.Background(), g.Owner, g.Repository, &github.PullRequestListOptions{
		Head:      g.Owner + ":" + options.Head,
		Sort:      "created",
		Direction: "desc",
	})
	if err != nil {
		log.ForContext(ctx).WithError(err).Error("failed to list existing pull requests")
		return nil, false, err
	}
	switch len(prs) {
	case 0:
		newPROptions := github.CreatePullRequest{
			Title: github.String(options.Title),
			Body:  github.String(options.Body),
			Base:  options.Base,
			Head:  options.Head,
		}
		if options.WIP {
			log.ForContext(ctx).Info("adding draft marker")
			newPROptions.Draft = github.Bool(true)
		}
		pr, _, err := g.PullRequests.Create(context.Background(), g.Owner, g.Repository, newPROptions)
		if err != nil {
			log.ForContext(ctx).WithError(err).Error("failed to create new pull request")
			return nil, false, err
		}
		log.ForContext(ctx).Debug("new PR has been created")
		return &PullRequest{
			ID:  strconv.Itoa(*pr.Number),
			URL: pr.GetHTMLURL(),
		}, true, nil
	case 1:
		log.ForContext(ctx).Trace("PR already existed")
		return &PullRequest{
			ID:  strconv.Itoa(*prs[0].Number),
			URL: prs[0].GetHTMLURL(),
		}, false, nil
	}
	log.ForContext(ctx).WithError(err).Error("failed to list existing pull requests")
	return nil, false, errors.New("Too may matching pull requests")

}

// Update implements the Update interface to update an existing pull request
func (g *GitHub) Update(ctx context.Context, pr *PullRequest, options PullRequestOptions) (*PullRequest, error) {
	ctx = log.WithContextFields(ctx, logrus.Fields{
		"context":    "ensuring existing pull request",
		"owner":      g.Owner,
		"repository": g.Repository,
		"prOptions":  options,
	})
	id, err := strconv.Atoi(pr.ID)
	if err != nil {
		log.ForContext(ctx).WithField("prID", pr.ID).WithError(err).Error("failed to parse pull request ID")
		return nil, err
	}
	ctx = log.WithContextFields(ctx, logrus.Fields{"prID": id})
	prUpdateOptions := &github.PullRequest{
		Title: github.String(options.Title),
		Body:  github.String(options.Body),
		Base: &github.PullRequestBranch{
			Ref: github.String(options.Base),
		},
		Head: &github.PullRequestBranch{
			Ref: github.String(options.Head),
		},
	}
	p, _, err := g.PullRequests.Edit(ctx, g.Owner, g.Repository, id, prUpdateOptions)
	if isStackedBaseError(err) {
		log.ForContext(ctx).Info("base branch is managed by the stack, updating without base")
		prUpdateOptions.Base = nil
		p, _, err = g.PullRequests.Edit(ctx, g.Owner, g.Repository, id, prUpdateOptions)
	}
	if err != nil {
		log.ForContext(ctx).WithError(err).Error("failed to edit pull request")
		return nil, err
	}
	log.ForContext(ctx).Info("edit pull request")
	if options.Ready {
		log.ForContext(ctx).Info("marking pull request as ready")

		var mutation struct {
			MarkPullRequestReadyForReview struct {
				PullRequest struct {
					ID githubv4.ID
				}
			} `graphql:"markPullRequestReadyForReview(input: $input)"`
		}

		variables := map[string]interface{}{
			"input": githubv4.MarkPullRequestReadyForReviewInput{
				// https://github.blog/changelog/2018-05-30-end-jean-grey-preview/
				// The NodeID seems to be the pivot between the REST API and the graphQL API
				PullRequestID: p.GetNodeID(),
			},
		}

		// Unfortunately, there is no way, with the REST API to mark a PR as ready.
		// see https://docs.github.com/en/rest/pulls/pulls?apiVersion=2022-11-28#update-a-pull-request
		// Instead, use the graphQL client and in particular, use the github cli implementation
		err = g.GraphQLClient.Mutate("PullRequestReadyForReview", &mutation, variables)
		if err != nil {
			log.ForContext(ctx).WithError(err).Error("failed to mark pull request as ready")
			return nil, err
		}
	}
	return &PullRequest{
		ID:  strconv.Itoa(*p.Number),
		URL: *p.URL,
	}, err
}

// DefaultBranch returns the default branch of the remote repository
func (g *GitHub) DefaultBranch(ctx context.Context) string {
	repo, _, err := g.Repositories.Get(ctx, g.Owner, g.Repository)
	if err != nil {
		return ""
	}
	return repo.GetDefaultBranch()
}

// StackManager returns the GitHub native stack manager.
func (g *GitHub) StackManager() StackManager {
	return g.stackManager
}

func (g *GitHub) BodyFormatter() BodyFormatter {
	return HTMLBodyFormatter{}
}

// LinkedTopicIssues returns the search URL for linked issues
func (g *GitHub) LinkedTopicIssues(topicSearchString string) string {
	values := url.Values{}
	values.Add("q", fmt.Sprintf(`is:pr is:open "%s"`, topicSearchString))
	values.Add("type", "issues")
	values.Encode()
	return `https://` + g.Host + `/search?` + values.Encode()
}

func isStackedBaseError(err error) bool {
	var ghErr *github.ErrorResponse
	if !errors.As(err, &ghErr) {
		return false
	}
	for _, e := range ghErr.Errors {
		if e.Field == "base" && strings.Contains(e.Message, "part of a stack") {
			return true
		}
	}
	return false
}

// NewGitHubUpserter instanciates an upserter that uses the github API to create and update pull requests
func NewGitHubUpserter(ctx context.Context, endpoint *transport.Endpoint) (*GitHub, error) {
	ctx = log.WithContextFields(ctx, logrus.Fields{
		"context":  "initializing GitHub client",
		"endpoint": endpoint,
	})

	orgRepo := strings.Split(strings.Trim(endpoint.Path, "/"), "/")
	if len(orgRepo) != 2 {
		log.ForContext(ctx).WithField("repository", endpoint.Path).Error("invalid repository, expecting <org>/<repo>")
		return nil, fmt.Errorf("invalid repository, expecting <org>/<repo>")
	}
	httpClient, err := gh.NewHTTPClientForDomain(ctx, endpoint.Host)
	if err != nil {
		log.ForContext(ctx).WithError(err).Errorf("failed to create a new http client: %s", err.Error())
		return nil, err
	}
	client, err := gh.NewClient(httpClient, endpoint.Host)
	if err != nil {
		log.ForContext(ctx).WithError(err).Errorf("failed to create a new github client: %s", err.Error())
		return nil, err
	}
	repo, _, err := client.Repositories.Get(ctx, orgRepo[0], strings.TrimSuffix(orgRepo[1], ".git"))
	if err != nil {
		return nil, err
	}

	graphQLClient, err := gh.NewGraphQLClient(httpClient, endpoint.Host)
	if err != nil {
		log.ForContext(ctx).WithError(err).Errorf("failed to create a new github graphQL client: %s", err.Error())
		return nil, err
	}

	gh := &GitHub{
		Host:          endpoint.Host,
		Owner:         repo.GetOwner().GetLogin(),
		Repository:    repo.GetName(),
		Client:        client,
		GraphQLClient: graphQLClient,
		stackManager:  NewGitHubStackManager(client, repo.GetOwner().GetLogin(), repo.GetName()),
	}
	log.ForContext(ctx).Trace("initialized github client")
	return gh, nil
}
