package maiao

import (
	"context"
	"errors"
	"testing"

	lgit "github.com/adevinta/maiao/pkg/git"
	"github.com/adevinta/maiao/pkg/testutil"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubRebase replaces the rebase with one that reports the given error, so that a
// stopped rebase can be exercised without provoking a real conflict.
func stubRebase(t testing.TB, err error) *bool {
	t.Helper()
	called := false
	original := rebase
	t.Cleanup(func() { rebase = original })
	rebase = func(context.Context, lgit.Repository, plumbing.Hash, plumbing.Hash, string) error {
		called = true
		return err
	}
	return &called
}

// repoToRebase builds a repository with commits to review and returns it with the
// hash the review would rebase onto.
func repoToRebase(t testing.TB) (*git.Repository, plumbing.Hash, plumbing.Hash) {
	t.Helper()
	dir := testutil.InitRepo(t)
	base := testutil.CmdOutput(t, "git", "-C", dir, "rev-parse", "HEAD")
	testutil.CommitFile(t, dir, "one.txt", "one", "first change")
	head := testutil.CommitFile(t, dir, "two.txt", "two", "second change")
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	return repo, plumbing.NewHash(base), plumbing.NewHash(head)
}

// TestRebaseThatStopsIsReported is the behaviour this commit exists to change.
//
// git leaves the rebase where it stopped, and the step that creates the reviews is
// the last entry in its todo list, so nothing was submitted. Returning nil said
// otherwise, and a caller checking the exit status could not tell the run apart
// from one with nothing to review.
func TestRebaseThatStopsIsReported(t *testing.T) {
	repo, base, head := repoToRebase(t)
	called := stubRebase(t, errors.New("exit status 1"))

	err := rebaseCommits(context.Background(), repo, ReviewOptions{}, base, base, head)

	require.Error(t, err)
	assert.True(t, *called)
	assert.ErrorIs(t, err, ErrRebaseIncomplete)
	assert.Contains(t, err.Error(), "git rebase --continue", "the message must say how to finish")
	assert.Contains(t, err.Error(), "exit status 1", "git's own failure must not be swallowed")
}

// TestRebaseThatCompletesIsSilent guards the ordinary path: the reviews are created
// by the step git runs at the end of the rebase, so there is nothing to report.
func TestRebaseThatCompletesIsSilent(t *testing.T) {
	repo, base, head := repoToRebase(t)
	called := stubRebase(t, nil)

	require.NoError(t, rebaseCommits(context.Background(), repo, ReviewOptions{}, base, base, head))
	assert.True(t, *called)
}

// TestNothingToRebaseDoesNotRebase covers the case where every change has already
// been merged: there is no failure, and no rebase to run either.
func TestNothingToRebaseDoesNotRebase(t *testing.T) {
	repo, _, head := repoToRebase(t)
	called := stubRebase(t, errors.New("must not be reached"))

	require.NoError(t, rebaseCommits(context.Background(), repo, ReviewOptions{}, head, head, head))
	assert.False(t, *called)
}
