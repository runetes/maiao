//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reorderings are the shapes worth paying a live forge round trip for.
//
// Each differs in which pull requests have to be moved out of the way before the push,
// which is what decides whether a base branch ends up containing its own pull
// request's head:
//   - the bottom of the stack changes, or it does not;
//   - a pull request keeps its base branch's *name* while that branch's contents change
//     underneath it, which needs four commits to produce and is the one case the
//     parking pass deliberately skips.
var reorderings = []struct {
	name  string
	order []int
}{
	{name: "two commits swapped", order: []int{1, 0}},
	{name: "top two swapped", order: []int{0, 2, 1}},
	{name: "bottom two swapped", order: []int{1, 0, 2}},
	{name: "fully reversed", order: []int{2, 1, 0}},
	{name: "unmoved base whose contents change", order: []int{1, 0, 2, 3}},
}

// TestReorderKeepsEveryPullRequestOpen is the behaviour the parking pass exists for.
//
// Reordering a stack points a branch at commits that contain a pull request's head, and
// a forge reads that as merged and closes it — for good. So a reorder has to leave every
// pull request open, on the number it already had, with the conversation on it.
func TestReorderKeepsEveryPullRequestOpen(t *testing.T) {
	for _, f := range forges(t) {
		t.Run(f.Name(), func(t *testing.T) {
			for _, reordering := range reorderings {
				t.Run(reordering.name, func(t *testing.T) {
					s := newStack(t, f, len(reordering.order))

					_, err := s.review("before")
					require.NoError(t, err, "the first review has to succeed for there to be anything to reorder")

					before := make([]pullRequest, len(s.changes))
					comments := make([]string, len(s.changes))
					for i := range s.changes {
						before[i] = s.only(i)
						id, err := s.forge.AddComment(context.Background(), before[i].Number,
							"this conversation must survive the reorder")
						require.NoError(t, err)
						comments[i] = id
					}

					s.reorder(reordering.order...)
					_, err = s.review("after")
					require.NoError(t, err, "a reorder must not fail the review")

					for i, c := range s.changes {
						after := s.only(i)
						assert.Equal(t, before[i].Number, after.Number,
							"change %s must keep the pull request it had", c.name)
						assert.Equal(t, "open", after.State, "change %s must stay open", c.name)
						assert.False(t, after.Merged,
							"change %s must not be reported merged: nothing was merged", c.name)

						ids, err := s.forge.CommentIDs(context.Background(), after.Number)
						require.NoError(t, err)
						assert.Contains(t, ids, comments[i],
							"the conversation on #%s must survive", after.Number)
					}

					// Bottom of the stack on the default branch, everything else on the
					// branch of the change below it in the new order.
					for position, i := range reordering.order {
						want := s.defaultBranch
						if position > 0 {
							want = s.changes[reordering.order[position-1]].branch
						}
						assert.Equal(t, want, s.only(i).Base,
							"change %s sits at position %d, so it targets the change below it",
							s.changes[i].name, position)
					}
				})
			}
		})
	}
}

// TestAClosedPullRequestIsReopenedNotDuplicated covers the state in between open and
// merged: closed, with nothing merged, which is what a human closing a review leaves.
//
// The lookup used to ask only for the open pull requests, so a closed one was invisible
// and the change got a second pull request beside it — with the conversation left on the
// first. Nothing else here reaches that state: a reorder produces *merged* ones, which
// no forge will reopen.
func TestAClosedPullRequestIsReopenedNotDuplicated(t *testing.T) {
	for _, f := range forges(t) {
		t.Run(f.Name(), func(t *testing.T) {
			s := newStack(t, f, 3)
			_, err := s.review("before")
			require.NoError(t, err)

			// The middle change: reopening it has to leave it on the branch of the change
			// below it, which is the part that decides whether the forge closes it again.
			const middle = 1
			before := s.only(middle)
			comment, err := s.forge.AddComment(context.Background(), before.Number,
				"this conversation must survive being closed and reopened")
			require.NoError(t, err)

			require.NoError(t, s.forge.Close(context.Background(), before.Number))
			closed := s.only(middle)
			require.Equal(t, "closed", closed.State, "the test needs it closed to mean anything")
			require.False(t, closed.Merged, "closing must not have marked it merged, or there is nothing to reopen")

			_, err = s.review("after")
			require.NoError(t, err)

			after := s.only(middle)
			assert.Equal(t, before.Number, after.Number, "no second pull request for the same change")
			assert.Equal(t, "open", after.State, "the closed pull request must come back")
			assert.Equal(t, s.changes[middle-1].branch, after.Base,
				"and come back on the branch of the change below it")

			ids, err := s.forge.CommentIDs(context.Background(), after.Number)
			require.NoError(t, err)
			assert.Contains(t, ids, comment, "which is the point: the conversation is still the live one")
		})
	}
}

// TestAFixupKeepsEveryPullRequestOpen is the path the reorder tests miss, and the one
// maiao's own repository is reviewed through.
//
// They hand maiao an already-reordered stack, so no rebase is needed and the pull
// requests are sent from the same process. A `fixup!` commit is not adjacent to its
// target, so the review rebases: maiao's todo squashes the fixup into the commit it
// fixes, every change above it is rebuilt on a new commit, and the pull requests are
// created by the maiao that `git rebase` execs as its last todo step.
func TestAFixupKeepsEveryPullRequestOpen(t *testing.T) {
	for _, f := range forges(t) {
		t.Run(f.Name(), func(t *testing.T) {
			// Change 0 rebuilds every branch above it; change 1 leaves the bottom alone
			// and rebuilds the rest, which is the shape of answering a review comment
			// halfway up a stack.
			for _, target := range []int{0, 1} {
				t.Run(fmt.Sprintf("fixing up change %d", target), func(t *testing.T) {
					s := newStack(t, f, 3)
					_, err := s.review("before")
					require.NoError(t, err)

					before := make([]pullRequest, len(s.changes))
					comments := make([]string, len(s.changes))
					for i := range s.changes {
						before[i] = s.only(i)
						id, err := s.forge.AddComment(context.Background(), before[i].Number,
							"this conversation must survive a fixup rebase")
						require.NoError(t, err)
						comments[i] = id
					}

					s.fixup(target)
					result, err := s.review("after")
					require.NoError(t, err, "a fixup must not fail the review")

					// Squashed into its target rather than becoming a change of its own, which
					// is what keeps the stack's order and so its base branches.
					assert.Len(t, result.Changes, len(s.changes),
						"the fixup belongs to an existing change, not to a new one")

					for i, c := range s.changes {
						after := s.only(i)
						assert.Equal(t, before[i].Number, after.Number,
							"change %s must keep the pull request it had", c.name)
						assert.Equal(t, "open", after.State, "change %s must stay open", c.name)
						assert.False(t, after.Merged, "change %s must not be reported merged", c.name)

						want := s.defaultBranch
						if i > 0 {
							want = s.changes[i-1].branch
						}
						assert.Equal(t, want, after.Base, "change %s keeps its place in the stack", c.name)

						ids, err := s.forge.CommentIDs(context.Background(), after.Number)
						require.NoError(t, err)
						assert.Contains(t, ids, comments[i], "the conversation on #%s must survive", after.Number)
					}
				})
			}
		})
	}
}
