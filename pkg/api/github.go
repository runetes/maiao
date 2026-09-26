package api

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/google/go-github/v90/github"
	gh "github.com/runetes/maiao/pkg/github"
	"github.com/runetes/maiao/pkg/log"
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

// Find looks up the open pull request for head. It owns the list-and-count
// switch so Ensure cannot drift from what a plain lookup reports.
func (g *GitHub) Find(ctx context.Context, head string) (*PullRequest, error) {
	ctx = log.WithContextFields(ctx, logrus.Fields{
		"context":    "finding existing pull request",
		"owner":      g.Owner,
		"repository": g.Repository,
		"head":       head,
	})
	// State "all": the API defaults to the open ones, and a review that cannot see a
	// closed pull request opens a second one for the same change and leaves the
	// conversation on the first.
	prs, _, err := g.PullRequests.List(ctx, g.Owner, g.Repository, &github.PullRequestListOptions{
		Head:      g.Owner + ":" + head,
		State:     "all",
		Sort:      "created",
		Direction: "desc",
	})
	if err != nil {
		log.ForContext(ctx).WithError(err).Error("failed to list existing pull requests")
		return nil, err
	}
	// Listing every state can return more than one pull request for a head branch
	// where listing the open ones returned at most one, and those extras are the
	// wreckage of an earlier run rather than an ambiguity to refuse.
	//
	// An open one wins, whatever its age. Picking a closed one that happens to be newer
	// would reopen a second review beside the live one, and — worse — leave the live one
	// unparked, so the push closes it as merged for good. GitHub's uniqueness rule is on
	// the head and base together, not the head alone, and maiao moves bases on every
	// reorder, so two open pull requests on one head branch are reachable.
	//
	// Among equals the highest number wins, numbers only growing. Read from the numbers
	// rather than from the order the list arrived in, which asks for newest first but is
	// the server's to honour: after a run that had to abandon a merged pull request, the
	// newest is the replacement it opened, so the warning about the stranded conversation
	// is printed once and not on every review from then on.
	if len(prs) == 0 {
		return nil, nil
	}
	pr := prs[0]
	pickedOpen := pr.GetState() == "open"
	for _, candidate := range prs[1:] {
		open := candidate.GetState() == "open"
		if (open && !pickedOpen) || (open == pickedOpen && candidate.GetNumber() > pr.GetNumber()) {
			pr, pickedOpen = candidate, open
		}
	}
	log.ForContext(ctx).WithField("prID", pr.GetNumber()).Trace("PR already existed")
	return &PullRequest{
		ID:    strconv.Itoa(pr.GetNumber()),
		URL:   pr.GetHTMLURL(),
		Base:  pr.GetBase().GetRef(),
		State: githubState(pr),
	}, nil
}

// githubState maps what GitHub reports onto the states a review can act on.
//
// A pull request GitHub closed because its head became reachable from its base
// carries a merged_at even though nothing was merged, and that is precisely the one
// that cannot be reopened, so merged_at is what decides — not the merge commit and
// not `merged`, which the list endpoint does not return.
func githubState(pr *github.PullRequest) PullRequestState {
	if pr.GetState() == "open" {
		return PullRequestOpen
	}
	if !pr.GetMergedAt().IsZero() {
		return PullRequestMerged
	}
	return PullRequestClosed
}

// Ensure ensures a PR is opened for the head branch
func (g *GitHub) Ensure(ctx context.Context, options PullRequestOptions) (*PullRequest, bool, error) {
	ctx = log.WithContextFields(ctx, logrus.Fields{
		"context":    "ensuring existing pull request",
		"owner":      g.Owner,
		"repository": g.Repository,
		"prOptions":  options,
	})
	pr, err := g.Find(ctx, options.Head)
	if err != nil {
		return nil, false, err
	}
	switch {
	case pr == nil, pr.State == PullRequestMerged:
		// A merged pull request cannot be reopened, so this change needs a new one.
		// The review has already said on stderr which conversation that leaves behind.
	case pr.State == PullRequestClosed:
		reopened, err := g.reopen(ctx, pr, options)
		switch {
		case err == nil:
			return reopened, false, nil
		case errors.Is(err, ErrPullRequestNotReopenable):
			ReportAbandonedConversation(pr)
		default:
			return nil, false, err
		}
	default:
		return pr, false, nil
	}
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
	created, _, err := g.PullRequests.Create(ctx, g.Owner, g.Repository, newPROptions)
	if err != nil {
		log.ForContext(ctx).WithError(err).Error("failed to create new pull request")
		return nil, false, err
	}
	log.ForContext(ctx).Debug("new PR has been created")
	return &PullRequest{
		ID:   strconv.Itoa(*created.Number),
		URL:  created.GetHTMLURL(),
		Base: created.GetBase().GetRef(),
	}, true, nil
}

// reopen brings a closed pull request back, so the change keeps the pull request
// it already had and the conversation on it.
//
// A base that is not already where the change belongs is moved first, in its own
// edit. GitHub closes a pull request as merged the moment its head is reachable from
// its base, so reopening one while it still names the base an earlier run left
// behind would hand it straight back to that rule — and that closure is final.
// options.Base is where the change belongs in the stack as it is now, which is
// behind its head.
//
// Which is why a base that has to move and will not is answered with
// ErrPullRequestNotReopenable rather than by reopening anyway: a new pull request
// costs the conversation on this one, and reopening it onto a base that contains it
// costs the conversation *and* the pull request, with no way back.
func (g *GitHub) reopen(ctx context.Context, pr *PullRequest, options PullRequestOptions) (*PullRequest, error) {
	id, err := strconv.Atoi(pr.ID)
	if err != nil {
		return nil, err
	}
	log.ForContext(ctx).WithField("prID", id).Info("reopening the closed pull request of this change rather than opening a second one")
	// Most closed pull requests are already on the right base — nothing reordered,
	// somebody just closed the review — and asking GitHub to set the base it already
	// has is both a wasted write and, for a pull request inside a native stack, a
	// refusal: `Cannot change the base branch because the pull request is part of a
	// stack.`
	if pr.Base != options.Base {
		if _, _, err := g.PullRequests.Edit(ctx, g.Owner, g.Repository, id, &github.PullRequest{
			Base: &github.PullRequestBranch{Ref: github.String(options.Base)},
		}); err != nil {
			log.ForContext(ctx).WithField("prID", id).WithField("base", options.Base).WithError(err).
				Warn("cannot move the base of the closed pull request, so it is left closed rather than reopened onto a base that may contain it")
			return nil, fmt.Errorf("%w: %s: %w", ErrPullRequestNotReopenable, pr.URL, err)
		}
	}
	reopened, _, err := g.PullRequests.Edit(ctx, g.Owner, g.Repository, id, &github.PullRequest{
		State: github.String("open"),
	})
	if err != nil {
		log.ForContext(ctx).WithField("prID", id).WithError(err).Error("failed to reopen pull request")
		return nil, err
	}
	return &PullRequest{
		ID:    strconv.Itoa(reopened.GetNumber()),
		URL:   reopened.GetHTMLURL(),
		Base:  reopened.GetBase().GetRef(),
		State: githubState(reopened),
	}, nil
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
		log.ForContext(ctx).Warn("base could not be moved: GitHub's native stack owns it, and the pull request may be closed by the provider when the stack is reordered")
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
		ID:   strconv.Itoa(*p.Number),
		URL:  *p.URL,
		Base: p.GetBase().GetRef(),
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
	values.Add("q", fmt.Sprintf(`is:pr is:open "%s" org:%s`, topicSearchString, g.RepoOwner()))
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
