package maiao

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/runetes/maiao/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tracingRepo records the branches a review pushes, in the order it pushes them,
// and how many pushes it took.
type tracingRepo struct {
	*pushlessRepo
	trace  *[]string
	pushes int
}

func (r *tracingRepo) Push(o *git.PushOptions) error {
	r.pushes++
	branches := []string{}
	for _, refspec := range o.RefSpecs {
		_, dst, _ := strings.Cut(string(refspec), ":")
		branches = append(branches, strings.TrimPrefix(dst, "refs/heads/"))
	}
	*r.trace = append(*r.trace, "push "+strings.Join(branches, ","))
	return nil
}

// reviewOfStack sends the three changes of stackToReview to a provider that
// already holds a pull request for each of them, based on the branches given.
//
// It returns what the review asked of the remote, in order.
func reviewOfStack(t *testing.T, currentBases map[string]string) ([]string, *testAPI, *tracingRepo) {
	t.Helper()
	repo, base, head := stackToReview(t)
	trace := []string{}
	prAPI := &testAPI{
		FindFunc: func(_ context.Context, head string) (*api.PullRequest, error) {
			current, ok := currentBases[head]
			if !ok {
				return nil, nil
			}
			return &api.PullRequest{ID: head, URL: "https://example.com/" + head, Base: current}, nil
		},
		EnsureFunc: func(_ context.Context, opts api.PullRequestOptions) (*api.PullRequest, bool, error) {
			trace = append(trace, "create "+opts.Head)
			return &api.PullRequest{ID: opts.Head, URL: "https://example.com/" + opts.Head, Base: opts.Base}, true, nil
		},
		UpdateFunc: func(_ context.Context, _ *api.PullRequest, opts api.PullRequestOptions) (*api.PullRequest, error) {
			trace = append(trace, fmt.Sprintf("base %s -> %s", opts.Head, opts.Base))
			return &api.PullRequest{ID: opts.Head, Base: opts.Base}, nil
		},
	}
	withPullRequester(t, prAPI)
	tracer := &tracingRepo{pushlessRepo: repo, trace: &trace}

	_, err := sendPrs(context.Background(), tracer, ReviewOptions{Remote: "origin", Branch: "main", RepoPath: repo.url}, base, head)
	require.NoError(t, err)
	return trace, prAPI, tracer
}

// TestReorderedPullRequestsAreParkedOnTheBaseBeforeThePush is the behaviour that
// keeps a reordered stack from closing its own pull requests.
//
// GitHub marks a pull request merged the moment its head commits are contained in
// its base branch. Swap two commits and the branch a pull request still names as
// its base is force-pushed to a tip that now contains that pull request's own head,
// so GitHub closes it as merged before the review gets to correct the base. The
// next run cannot recover it either: an existing pull request is looked up among
// the open ones only, so a second one is opened for the same change.
//
// Parking it on the branch the stack is based on removes the overlap: the base
// branch is behind every change in the stack, so it can contain no head at all.
func TestReorderedPullRequestsAreParkedOnTheBaseBeforeThePush(t *testing.T) {
	// I333 used to sit directly on I111; the stack now has I222 between them, so
	// pushing maiao.I111 would hand it commits that maiao.I333 already has.
	trace, _, _ := reviewOfStack(t, map[string]string{
		"maiao.I111": "main",
		"maiao.I222": "maiao.I111",
		"maiao.I333": "maiao.I111",
	})

	parked := indexOf(t, trace, "base maiao.I333 -> main")
	assert.Less(t, parked, indexOf(t, trace, "push maiao.I111,maiao.I222,maiao.I333"),
		"maiao.I333 must be off its stale base before the branches are pushed")
	assert.Less(t, parked, indexOf(t, trace, "base maiao.I333 -> maiao.I222"),
		"its real base can only be set once the branches carry the new commits")
}

// TestAnUnchangedStackIsNotTouchedBeforeThePush keeps the guard from costing a
// write, and a visibly wrong base, on every review that reorders nothing.
func TestAnUnchangedStackIsNotTouchedBeforeThePush(t *testing.T) {
	trace, _, _ := reviewOfStack(t, map[string]string{
		"maiao.I111": "main",
		"maiao.I222": "maiao.I111",
		"maiao.I333": "maiao.I222",
	})

	assert.Equal(t, "push maiao.I111,maiao.I222,maiao.I333", trace[0],
		"nothing is asked of the provider before the push: %v", trace)
}

// TestBranchesAreSentInOnePush keeps the whole stack landing on the remote
// together, so a review that fails part way leaves no half-pushed stack behind.
func TestBranchesAreSentInOnePush(t *testing.T) {
	_, _, tracer := reviewOfStack(t, map[string]string{"maiao.I111": "main"})

	assert.Equal(t, 1, tracer.pushes)
}

// TestPullRequestsFoundBeforeThePushAreNotCreatedAgain covers the lookup being
// reused: Ensure would list the same pull requests a second time.
func TestPullRequestsFoundBeforeThePushAreNotCreatedAgain(t *testing.T) {
	trace, prAPI, _ := reviewOfStack(t, map[string]string{
		"maiao.I111": "main",
		"maiao.I222": "maiao.I111",
	})

	assert.Equal(t, 1, prAPI.EnsureCalled, "only maiao.I333 has no pull request yet")
	assert.Contains(t, trace, "create maiao.I333")
}

// TestAFailedPushSaysWhichPullRequestsAreLeftParked is what keeps parking from
// being a silent state change.
//
// The user did not put those reviews on the base branch and nothing else in the
// output mentions them, so a run that dies after parking and before the push has to
// say which ones are showing their whole stack as their own diff.
func TestAFailedPushSaysWhichPullRequestsAreLeftParked(t *testing.T) {
	repo, base, head := stackToReview(t)
	refused := errors.New("remote hung up")
	withPullRequester(t, &testAPI{
		FindFunc: func(_ context.Context, head string) (*api.PullRequest, error) {
			if head != "maiao.I333" {
				return nil, nil
			}
			return &api.PullRequest{ID: "3", URL: "https://example.com/pull/3", Base: "maiao.I111"}, nil
		},
		UpdateFunc: func(_ context.Context, pr *api.PullRequest, opts api.PullRequestOptions) (*api.PullRequest, error) {
			return &api.PullRequest{ID: pr.ID, URL: pr.URL, Base: opts.Base}, nil
		},
	})

	_, err := sendPrs(
		context.Background(),
		&failingPushRepo{pushlessRepo: repo, err: refused},
		ReviewOptions{Remote: "origin", Branch: "main", RepoPath: repo.url},
		base,
		head,
	)

	require.ErrorIs(t, err, refused, "the push failure must not be swallowed")
	assert.Contains(t, err.Error(), "https://example.com/pull/3")
	assert.Contains(t, err.Error(), "main", "the branch they are left on")
}

// TestAParkThatDidNotTakeStopsTheReviewBeforeThePush covers the provider refusing
// to move a base branch, which is what GitHub does when its own native stacks own
// it: the edit comes back reporting success, with the base where it was.
//
// Pushing after that is what destroys the review — the pull request is closed as
// merged and cannot be reopened. A failed run leaves it open and says why, which is
// the only outcome here the user can act on.
func TestAParkThatDidNotTakeStopsTheReviewBeforeThePush(t *testing.T) {
	repo, base, head := stackToReview(t)
	pushed := false
	withPullRequester(t, &testAPI{
		FindFunc: func(_ context.Context, head string) (*api.PullRequest, error) {
			if head != "maiao.I333" {
				return nil, nil
			}
			return &api.PullRequest{ID: "3", URL: "https://example.com/pull/3", Base: "maiao.I111"}, nil
		},
		UpdateFunc: func(_ context.Context, pr *api.PullRequest, _ api.PullRequestOptions) (*api.PullRequest, error) {
			// The base stays where it was, whatever was asked for.
			return &api.PullRequest{ID: pr.ID, URL: pr.URL, Base: "maiao.I111"}, nil
		},
	})

	_, err := sendPrs(
		context.Background(),
		&watchedPushRepo{pushlessRepo: repo, pushed: &pushed},
		ReviewOptions{Remote: "origin", Branch: "main", RepoPath: repo.url},
		base,
		head,
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBaseNotMoved)
	assert.Contains(t, err.Error(), "https://example.com/pull/3")
	assert.False(t, pushed, "nothing may be pushed while a pull request still names a base that is about to move")
}

// TestANativeStackIsDissolvedBeforeItsPullRequestsAreParked covers the one case
// parking cannot handle on its own.
//
// GitHub owns the base branches of a stack it manages and refuses to move them, and
// it offers no way to take one pull request out of a stack, nor to reorder one. So
// the stack is dissolved first — which is what GitHub's own documentation tells
// people to do before restructuring one — and registered again at the end of the
// review, in the order the commits are now in.
func TestANativeStackIsDissolvedBeforeItsPullRequestsAreParked(t *testing.T) {
	repo, base, head := stackToReview(t)
	trace := []string{}
	stackMgr := &testStackManager{
		available: func(context.Context) bool { return true },
		getStack: func(_ context.Context, number int) (*api.Stack, error) {
			return &api.Stack{ID: "7", PRs: []int{1, 2, 3}}, nil
		},
		unstack: func(_ context.Context, stackID string) error {
			trace = append(trace, "unstack "+stackID)
			return nil
		},
		createOrUpdateStack: func(_ context.Context, prNumbers []int) (*api.Stack, error) {
			trace = append(trace, fmt.Sprintf("stack %v", prNumbers))
			return &api.Stack{ID: "8", PRs: prNumbers}, nil
		},
	}
	prAPI := &testAPIWithStack{stackMgr: stackMgr}
	prAPI.FindFunc = func(_ context.Context, head string) (*api.PullRequest, error) {
		bases := map[string]string{"maiao.I111": "main", "maiao.I222": "maiao.I111", "maiao.I333": "maiao.I111"}
		return &api.PullRequest{ID: fmt.Sprint(len(head)), URL: "https://example.com/" + head, Base: bases[head]}, nil
	}
	prAPI.UpdateFunc = func(_ context.Context, pr *api.PullRequest, opts api.PullRequestOptions) (*api.PullRequest, error) {
		trace = append(trace, fmt.Sprintf("base %s -> %s", opts.Head, opts.Base))
		return &api.PullRequest{ID: pr.ID, URL: pr.URL, Base: opts.Base}, nil
	}
	withPullRequester(t, prAPI)

	_, err := sendPrs(
		context.Background(),
		&tracingRepo{pushlessRepo: repo, trace: &trace},
		ReviewOptions{Remote: "origin", Branch: "main", RepoPath: stackTestRepo(t), Stack: "auto"},
		base,
		head,
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"7"}, stackMgr.unstacked, "one stack holds all three, so it is dissolved once")
	assert.Less(t, indexOf(t, trace, "unstack 7"), indexOf(t, trace, "base maiao.I333 -> main"),
		"GitHub refuses to move a base it owns, so the stack goes first")
	assert.Less(t, indexOf(t, trace, "base maiao.I333 -> main"),
		indexOf(t, trace, "push maiao.I111,maiao.I222,maiao.I333"))
}

// TestNativeStacksAreLeftAloneWhenTurnedOff keeps a setting that says not to use
// the stack API from being the thing that dissolves a stack.
func TestNativeStacksAreLeftAloneWhenTurnedOff(t *testing.T) {
	repo, base, head := stackToReview(t)
	stackMgr := &testStackManager{
		available: func(context.Context) bool { return true },
		getStack: func(context.Context, int) (*api.Stack, error) {
			return &api.Stack{ID: "7", PRs: []int{1, 2, 3}}, nil
		},
	}
	prAPI := &testAPIWithStack{stackMgr: stackMgr}
	prAPI.FindFunc = func(_ context.Context, head string) (*api.PullRequest, error) {
		return &api.PullRequest{ID: "1", URL: "https://example.com/" + head, Base: "stale"}, nil
	}
	prAPI.UpdateFunc = func(_ context.Context, pr *api.PullRequest, opts api.PullRequestOptions) (*api.PullRequest, error) {
		return &api.PullRequest{ID: pr.ID, URL: pr.URL, Base: opts.Base}, nil
	}
	withPullRequester(t, prAPI)

	_, err := sendPrs(
		context.Background(),
		&pushlessRepo{Repository: repo.Repository, url: repo.url},
		ReviewOptions{Remote: "origin", Branch: "main", RepoPath: stackTestRepo(t), Stack: "false"},
		base,
		head,
	)
	require.NoError(t, err)

	assert.Empty(t, stackMgr.unstacked)
}

type watchedPushRepo struct {
	*pushlessRepo
	pushed *bool
}

func (r *watchedPushRepo) Push(*git.PushOptions) error {
	*r.pushed = true
	return nil
}

type failingPushRepo struct {
	*pushlessRepo
	err error
}

func (r *failingPushRepo) Push(*git.PushOptions) error { return r.err }

func indexOf(t *testing.T, trace []string, op string) int {
	t.Helper()
	for i, entry := range trace {
		if entry == op {
			return i
		}
	}
	require.Failf(t, "missing operation", "%q is not in %v", op, trace)
	return -1
}

// reviewOfStackWithStates sends the three changes of stackToReview to a provider
// that reports a state per head branch, which is what decides whether the parking
// pass may touch a pull request at all.
func reviewOfStackWithStates(t *testing.T, prs map[string]*api.PullRequest) []string {
	t.Helper()
	repo, base, head := stackToReview(t)
	trace := []string{}
	prAPI := &testAPI{
		FindFunc: func(_ context.Context, head string) (*api.PullRequest, error) {
			return prs[head], nil
		},
		EnsureFunc: func(_ context.Context, opts api.PullRequestOptions) (*api.PullRequest, bool, error) {
			trace = append(trace, "ensure "+opts.Head+" on "+opts.Base)
			return &api.PullRequest{ID: opts.Head, URL: "https://example.com/" + opts.Head, Base: opts.Base, State: api.PullRequestOpen}, true, nil
		},
		UpdateFunc: func(_ context.Context, _ *api.PullRequest, opts api.PullRequestOptions) (*api.PullRequest, error) {
			trace = append(trace, fmt.Sprintf("base %s -> %s", opts.Head, opts.Base))
			return &api.PullRequest{ID: opts.Head, Base: opts.Base}, nil
		},
	}
	withPullRequester(t, prAPI)
	_, err := sendPrs(context.Background(), &tracingRepo{pushlessRepo: repo, trace: &trace},
		ReviewOptions{Remote: "origin", Branch: "main", RepoPath: repo.url}, base, head)
	require.NoError(t, err)
	return trace
}

// TestAMergedPullRequestIsLeftAloneAndTheChangeGetsANewOne is the state a
// repository is left in when a reorder outran the parking pass: the provider marked
// the pull request merged, and GitHub answers a reopen with
// `422 state cannot be changed. The pull request cannot be reopened.`
//
// So there is nothing to park — its base cannot be moved and it cannot come back —
// and the change needs a pull request of its own. While the lookup reported only
// open pull requests, a merged one was invisible here and the change got a second
// pull request with nothing saying why.
func TestAMergedPullRequestIsLeftAloneAndTheChangeGetsANewOne(t *testing.T) {
	trace := reviewOfStackWithStates(t, map[string]*api.PullRequest{
		"maiao.I111": {ID: "1", URL: "https://example.com/1", Base: "main", State: api.PullRequestOpen},
		// Reordered: it belongs behind maiao.I111, so the parking pass has to move it.
		"maiao.I222": {ID: "2", URL: "https://example.com/2", Base: "maiao.I333", State: api.PullRequestOpen},
		"maiao.I333": {ID: "3", URL: "https://example.com/3", Base: "maiao.I111", State: api.PullRequestMerged},
	})

	assert.NotContains(t, trace, "base maiao.I333 -> main", "a merged pull request must not be parked")
	assert.Contains(t, trace, "ensure maiao.I333 on maiao.I222", "the change needs a pull request of its own")
	// The open one that moved is still parked before the push, which is the whole
	// point of the pass.
	assert.Less(t, indexOf(t, trace, "base maiao.I222 -> main"),
		indexOf(t, trace, "push maiao.I111,maiao.I222,maiao.I333"))
}

// TestAClosedPullRequestIsReusedRatherThanParked covers the pull request a reorder
// closed without the provider calling it merged, and the one a human closed by hand.
//
// Parking protects a pull request from being closed by the push, which a closed one
// no longer needs, and reopening it before the push would point it at a base branch
// that run is about to overwrite. So the parking pass leaves it, and Ensure reopens
// it afterwards against the base it belongs behind — reusing the pull request the
// change already had, and the conversation on it, instead of opening a second one.
func TestAClosedPullRequestIsReusedRatherThanParked(t *testing.T) {
	trace := reviewOfStackWithStates(t, map[string]*api.PullRequest{
		"maiao.I111": {ID: "1", URL: "https://example.com/1", Base: "main", State: api.PullRequestOpen},
		"maiao.I222": {ID: "2", URL: "https://example.com/2", Base: "maiao.I111", State: api.PullRequestOpen},
		"maiao.I333": {ID: "3", URL: "https://example.com/3", Base: "main", State: api.PullRequestClosed},
	})

	assert.NotContains(t, trace, "base maiao.I333 -> main", "a closed pull request cannot be closed again, so it is not parked")
	assert.Contains(t, trace, "ensure maiao.I333 on maiao.I222", "Ensure is left to reopen it on the base it belongs behind")
}
