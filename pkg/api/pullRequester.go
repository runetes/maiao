package api

import "context"

// PullRequester defines the interface to implement to handle pull requests
type PullRequester interface {
	// Update defines the interface to create or update a pull request to match options
	Update(context.Context, *PullRequest, PullRequestOptions) (*PullRequest, error)
	// Ensure ensures one and only one pull request exists for the given head
	Ensure(context.Context, PullRequestOptions) (*PullRequest, bool, error)
	LinkedTopicIssues(topicSearchString string) string
	DefaultBranch(context.Context) string
	// StackManager returns the stack manager if native stacks are supported, or nil.
	StackManager() StackManager
}

// PullRequestOptions are the options available to create or update a pull request
type PullRequestOptions struct {
	Base  string
	Head  string
	Title string
	Body  string
	WIP   bool
	Ready bool
}

// PullRequest defines the object
type PullRequest struct {
	ID  string
	URL string
}
