package api

import "context"

// StackManager defines the interface for GitHub native stack operations.
type StackManager interface {
	// CreateOrUpdateStack registers a set of PRs as a GitHub native stack.
	// prNumbers must be ordered bottom-to-top (first targets the base branch).
	CreateOrUpdateStack(ctx context.Context, prNumbers []int) (*Stack, error)
	// GetStack retrieves the stack associated with a given PR number.
	GetStack(ctx context.Context, prNumber int) (*Stack, error)
	// DeleteStack removes a stack by its ID, unstacking all PRs in it.
	DeleteStack(ctx context.Context, stackID string) error
	// Available reports whether the GitHub Stack API is supported on this instance.
	Available(ctx context.Context) bool
}

// Stack represents a GitHub native stack of pull requests.
type Stack struct {
	ID  string
	PRs []int
}
