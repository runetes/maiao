package maiao

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/runetes/maiao/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tracingRepo records the branches a review pushes, in the order it pushes them.
type tracingRepo struct {
	*pushlessRepo
	trace *[]string
}

func (r *tracingRepo) Push(o *git.PushOptions) error {
	for _, refspec := range o.RefSpecs {
		_, dst, _ := strings.Cut(string(refspec), ":")
		*r.trace = append(*r.trace, "push "+strings.TrimPrefix(dst, "refs/heads/"))
	}
	return nil
}

// tracingAPI is a provider on which every pull request already exists, recording
// what the review asks of it.
func tracingAPI(trace *[]string) *testAPI {
	return &testAPI{
		EnsureFunc: func(_ context.Context, opts api.PullRequestOptions) (*api.PullRequest, bool, error) {
			*trace = append(*trace, "ensure "+opts.Head)
			return &api.PullRequest{ID: opts.Head, URL: "https://github.com/example/repo/pull/" + opts.Head}, false, nil
		},
		UpdateFunc: func(_ context.Context, _ *api.PullRequest, opts api.PullRequestOptions) (*api.PullRequest, error) {
			*trace = append(*trace, fmt.Sprintf("base %s -> %s", opts.Head, opts.Base))
			return &api.PullRequest{ID: opts.Head}, nil
		},
	}
}

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

// TestBranchesArePushedOnlyOnceTheEarlierPullRequestsNameTheirNewBase is the
// behaviour that keeps a reordered stack from being closed.
//
// GitHub marks a pull request merged the moment its head commits are contained in
// its base branch. Reordering commits locally and force-pushing every branch before
// re-pointing any pull request does exactly that: the branch a pull request still
// names as its base is force-pushed to a tip that now contains that pull request's
// own head, and GitHub closes it as merged before the review ever gets to correct
// the base. Worse, the next run cannot recover it, because an existing pull request
// is looked up among the open ones only, so a second one is opened instead.
//
// So no branch may be pushed while an earlier pull request in the stack still names
// a base the review is about to move.
func TestBranchesArePushedOnlyOnceTheEarlierPullRequestsNameTheirNewBase(t *testing.T) {
	repo, base, head := stackToReview(t)
	trace := []string{}
	withPullRequester(t, tracingAPI(&trace))

	_, err := sendPrs(
		context.Background(),
		&tracingRepo{pushlessRepo: repo, trace: &trace},
		ReviewOptions{Remote: "origin", Branch: "main", RepoPath: repo.url},
		base,
		head,
	)
	require.NoError(t, err)

	branches := []string{"maiao.I111", "maiao.I222", "maiao.I333"}
	bases := []string{"main", "maiao.I111", "maiao.I222"}
	for i, branch := range branches {
		for j := 0; j < i; j++ {
			assert.Less(t,
				indexOf(t, trace, fmt.Sprintf("base %s -> %s", branches[j], bases[j])),
				indexOf(t, trace, "push "+branch),
				"%s must already point at %s before %s is force-pushed", branches[j], bases[j], branch,
			)
		}
	}
}
