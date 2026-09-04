package maiao

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/adevinta/maiao/pkg/api"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewResultReportsEachChangeInStackOrder covers the created/updated
// distinction, which is the part of the result a caller cannot recover any other
// way: both cases end with a pull request at a URL.
func TestNewResultReportsEachChangeInStackOrder(t *testing.T) {
	changes := []*change{
		{
			changeID: "I111",
			branch:   "maiao.I111",
			created:  true,
			pr:       &api.PullRequest{URL: "https://example.com/pull/1", ID: "1"},
		},
		{
			changeID: "I222",
			branch:   "maiao.I222",
			created:  false,
			pr:       &api.PullRequest{URL: "https://example.com/pull/2", ID: "2"},
		},
	}

	assert.Equal(t, &Result{Changes: []Change{
		{ChangeID: "I111", Branch: "maiao.I111", URL: "https://example.com/pull/1", ID: "1", Status: StatusCreated},
		{ChangeID: "I222", Branch: "maiao.I222", URL: "https://example.com/pull/2", ID: "2", Status: StatusUpdated},
	}}, newResult(changes))
}

// TestNewResultWithoutChangesHasAnEmptyList keeps a nothing-to-review run from
// encoding "changes" as null, which a caller iterating the result would have to
// special-case.
func TestNewResultWithoutChangesHasAnEmptyList(t *testing.T) {
	assert.NotNil(t, newResult(nil).Changes)
	assert.Empty(t, newResult(nil).Changes)
}

// TestNewResultOmitsChangesWithNoPullRequest is what makes a partial result honest.
//
// When a review fails part way up the stack, the changes after it have no pull
// request. Reporting them anyway would list entries with an empty URL, which reads
// as a review that exists somewhere unnameable rather than one that was never made.
func TestNewResultOmitsChangesWithNoPullRequest(t *testing.T) {
	changes := []*change{
		{changeID: "I111", branch: "maiao.I111", created: true, pr: &api.PullRequest{URL: "https://example.com/pull/1", ID: "1"}},
		{changeID: "I222", branch: "maiao.I222"},
		{changeID: "I333", branch: "maiao.I333"},
	}

	result := newResult(changes)

	require.Len(t, result.Changes, 1)
	assert.Equal(t, "I111", result.Changes[0].ChangeID)
}

// pushlessRepo is a real repository with its remote interaction removed, so that
// sending pull requests can be driven without a server to push to.
type pushlessRepo struct {
	*git.Repository
	url string
}

func (r *pushlessRepo) Push(*git.PushOptions) error { return nil }

func (r *pushlessRepo) Remote(name string) (*git.Remote, error) {
	return git.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: name, URLs: []string{r.url}}), nil
}

// stackToReview builds a repository with three changes on top of its first commit.
//
// The remote is github.com so that provider detection resolves from its known hosts
// rather than prompting, and every commit carries a Change-Id so the changes are the
// shape a review that has already been rebased produces.
func stackToReview(t *testing.T) (*pushlessRepo, plumbing.Hash, plumbing.Hash) {
	t.Helper()
	d, err := os.MkdirTemp("", "send-prs-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(d) })

	gitCommand(t, d, "init")
	gitCommand(t, d, "config", "user.email", "john.doe@example.com")
	gitCommand(t, d, "config", "user.name", "John Doe")
	gitCommand(t, d, "commit", "--allow-empty", "-m", "Initial commit")
	base := gitCommand(t, d, "rev-parse", "HEAD")
	for _, changeID := range []string{"I111", "I222", "I333"} {
		gitCommand(t, d, "commit", "--allow-empty", "-m", "change "+changeID+"\n\nChange-Id: "+changeID)
	}

	repo, err := git.PlainOpen(d)
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	baseHash, err := repo.ResolveRevision(plumbing.Revision(base))
	require.NoError(t, err)
	return &pushlessRepo{Repository: repo, url: "https://github.com/example/repo.git"}, *baseHash, head.Hash()
}

// withPullRequester points the review at a stand-in provider for the duration of a
// test.
func withPullRequester(t *testing.T, prAPI api.PullRequester) {
	t.Helper()
	previous := pullRequesterFor
	t.Cleanup(func() { pullRequesterFor = previous })
	pullRequesterFor = func(context.Context, *git.Remote, string) (api.PullRequester, error) {
		return prAPI, nil
	}
}

// TestPullRequestsCreatedBeforeAFailureAreReported is the reason a failure carries a
// result at all.
//
// The branches are force-pushed before any pull request is opened, so by the time
// one fails the earlier ones are live on the remote. Returning nothing described the
// run as one that had done nothing, which is the opposite of the truth and the worst
// thing to tell a caller deciding whether to retry.
func TestPullRequestsCreatedBeforeAFailureAreReported(t *testing.T) {
	repo, base, head := stackToReview(t)
	refused := errors.New("pull request refused")
	prAPI := &testAPI{
		EnsureFunc: func(_ context.Context, opts api.PullRequestOptions) (*api.PullRequest, bool, error) {
			if opts.Head == "maiao.I222" {
				return nil, false, refused
			}
			return &api.PullRequest{URL: "https://github.com/example/repo/pull/1", ID: "1"}, true, nil
		},
	}
	withPullRequester(t, prAPI)

	result, err := sendPrs(context.Background(), repo, ReviewOptions{Remote: "origin", RepoPath: repo.url}, base, head)

	require.ErrorIs(t, err, refused)
	require.NotNil(t, result, "the pull requests already opened have to be reported")
	require.Len(t, result.Changes, 1, "only the change whose pull request exists")
	assert.Equal(t, "I111", result.Changes[0].ChangeID)
}

// TestPullRequestsAreReportedWhenUpdatingOneFails covers the second pass, where
// every pull request exists and only the descriptions and base branches are still
// being written.
func TestPullRequestsAreReportedWhenUpdatingOneFails(t *testing.T) {
	repo, base, head := stackToReview(t)
	refused := errors.New("update refused")
	prAPI := &testAPI{
		EnsureFunc: func(_ context.Context, opts api.PullRequestOptions) (*api.PullRequest, bool, error) {
			return &api.PullRequest{URL: "https://github.com/example/repo/pull/" + opts.Head, ID: "1"}, true, nil
		},
		UpdateFunc: func(context.Context, *api.PullRequest, api.PullRequestOptions) (*api.PullRequest, error) {
			return nil, refused
		},
	}
	withPullRequester(t, prAPI)

	result, err := sendPrs(context.Background(), repo, ReviewOptions{Remote: "origin", RepoPath: repo.url}, base, head)

	require.ErrorIs(t, err, refused)
	require.NotNil(t, result)
	assert.Len(t, result.Changes, 3, "all three exist, whatever state their description is in")
}

// TestStackIDIsReported threads the provider's stack identifier out to the caller.
//
// Nothing else exposes it: the pull requests are chained by their base branches
// regardless, so a caller wanting to look the stack up in the provider's API has no
// other way to learn it.
func TestStackIDIsReported(t *testing.T) {
	repo, base, head := stackToReview(t)
	prAPI := &testAPIWithStack{
		testAPI: testAPI{
			EnsureFunc: func(_ context.Context, opts api.PullRequestOptions) (*api.PullRequest, bool, error) {
				return &api.PullRequest{URL: "https://github.com/example/repo/pull/1", ID: "1"}, true, nil
			},
			UpdateFunc: func(_ context.Context, pr *api.PullRequest, _ api.PullRequestOptions) (*api.PullRequest, error) {
				return pr, nil
			},
		},
		stackMgr: &testStackManager{
			available: func(context.Context) bool { return true },
			createOrUpdateStack: func(_ context.Context, prNumbers []int) (*api.Stack, error) {
				return &api.Stack{ID: "stack-7", PRs: prNumbers}, nil
			},
		},
	}
	withPullRequester(t, prAPI)

	result, err := sendPrs(context.Background(), repo, ReviewOptions{Remote: "origin", RepoPath: stackTestRepo(t)}, base, head)

	require.NoError(t, err)
	assert.Equal(t, "stack-7", result.StackID)
}

// TestStackIDIsEmptyWithoutANativeStack keeps the field out of the result for the
// providers that have no such thing, rather than reporting an identifier a caller
// cannot use.
func TestStackIDIsEmptyWithoutANativeStack(t *testing.T) {
	repo, base, head := stackToReview(t)
	prAPI := &testAPI{
		EnsureFunc: func(_ context.Context, opts api.PullRequestOptions) (*api.PullRequest, bool, error) {
			return &api.PullRequest{URL: "https://github.com/example/repo/pull/1", ID: "1"}, true, nil
		},
		UpdateFunc: func(_ context.Context, pr *api.PullRequest, _ api.PullRequestOptions) (*api.PullRequest, error) {
			return pr, nil
		},
	}
	withPullRequester(t, prAPI)

	result, err := sendPrs(context.Background(), repo, ReviewOptions{Remote: "origin", RepoPath: stackTestRepo(t)}, base, head)

	require.NoError(t, err)
	assert.Empty(t, result.StackID)
}
