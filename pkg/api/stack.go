package api

import "context"

// StackManager defines the interface for GitHub native stack operations.
type StackManager interface {
	// CreateOrUpdateStack registers a set of PRs as a GitHub native stack.
	// prNumbers must be ordered bottom-to-top (first targets the base branch).
	CreateOrUpdateStack(ctx context.Context, prNumbers []int) (*Stack, error)
	// GetStack retrieves the stack associated with a given PR number.
	GetStack(ctx context.Context, prNumber int) (*Stack, error)
	// Unstack dissolves a stack, leaving each pull request open on the base
	// branch it currently targets.
	//
	// It is all or nothing: GitHub offers no way to take one pull request out of
	// a stack, and none to reorder one, so a stack whose order has changed can
	// only be rebuilt by dissolving it and creating it again.
	// See https://docs.github.com/en/rest/pulls/stacks.
	//
	// Pull requests that cannot be unstacked, such as those queued for merge, are
	// left in the stack, and the call still succeeds.
	Unstack(ctx context.Context, stackID string) error
	// Available reports whether the GitHub Stack API is supported on this instance.
	Available(ctx context.Context) bool
}

// Stack represents a GitHub native stack of pull requests.
type Stack struct {
	ID  string
	PRs []int
}
