package api

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// PullRequester defines the interface to implement to handle pull requests
type PullRequester interface {
	// Update defines the interface to create or update a pull request to match options
	Update(context.Context, *PullRequest, PullRequestOptions) (*PullRequest, error)
	// Ensure ensures one and only one pull request exists for the given head
	Ensure(context.Context, PullRequestOptions) (*PullRequest, bool, error)
	// Find returns the pull request for the given head branch whatever state it is
	// in, or nil when there is none. Unlike Ensure it never creates one, so it can
	// be called before the head branch exists on the remote.
	//
	// It reports closed pull requests because a review that only looked at the open
	// ones opened a second pull request beside a closed one for the same change,
	// leaving its conversation behind. The caller decides what a given state is
	// good for: State says which.
	Find(ctx context.Context, head string) (*PullRequest, error)
	LinkedTopicIssues(topicSearchString string) string
	DefaultBranch(context.Context) string
	// StackManager returns the stack manager if native stacks are supported, or nil.
	StackManager() StackManager
	// BodyFormatter returns the formatter used to render PR body sections.
	BodyFormatter() BodyFormatter
}

// PullRequestOptions are the options available to create or update a pull request
type PullRequestOptions struct {
	Base             string
	Head             string
	Title            string
	Body             string
	WIP              bool
	Ready            bool
	ParentPullNumber string
}

// ErrPullRequestNotReopenable reports that a closed pull request could not be
// brought back, so the change it belongs to needs a new one.
//
// Reopening keeps a conversation; reopening onto a base branch that already contains
// the pull request's head loses the conversation *and* the pull request, because a
// provider reads that as merged and will not reopen it a second time. So a base that
// has to move and will not is answered with this rather than with a reopen.
var ErrPullRequestNotReopenable = errors.New("the closed pull request of this change cannot be reopened")

// ReportAbandonedConversation tells the user which review a change is leaving behind
// when its closed pull request could not be brought back.
//
// On stderr rather than in a log, and here rather than in each provider: the default
// verbosity prints no logs at all, a review that quietly opens a second one is the
// complaint this whole behaviour answers, and five providers saying it five ways would
// drift. It names no noun, so it reads right on GitLab, where a review is a merge
// request.
func ReportAbandonedConversation(pr *PullRequest) {
	fmt.Fprintf(os.Stderr,
		"%s could not be reopened, so a new one is opened for this change; the review conversation stays on %s\n",
		pr.URL, pr.URL)
}

// PullRequestState is the state a provider reports a pull request in.
//
// It exists because the three cases differ in what a review may do with the pull
// request it found, and a bare "is it open" cannot tell the last two apart.
type PullRequestState string

const (
	// PullRequestOpen is a pull request a review can carry on using as it is.
	PullRequestOpen PullRequestState = "open"
	// PullRequestClosed is a pull request that is closed and can be reopened, so a
	// review reuses it rather than opening a second one for the same change.
	PullRequestClosed PullRequestState = "closed"
	// PullRequestMerged is a pull request the provider considers merged. GitHub
	// refuses to reopen one — `422: state cannot be changed. The pull request cannot
	// be reopened.` — whether or not anything was really merged, which is how a
	// reorder that outran the parking pass strands a conversation for good.
	PullRequestMerged PullRequestState = "merged"
)

// PullRequest defines the object
type PullRequest struct {
	ID  string
	URL string
	// State is what the provider reports the pull request as. An empty one is a
	// provider that does not say, which every caller treats as open, because that is
	// what the only lookup maiao had before this reported.
	State PullRequestState
	// Base is the branch the pull request currently targets on the provider. It
	// is what tells a review whose stack has been reordered which pull requests
	// have to be moved out of the way before the branches are pushed.
	Base string
}
