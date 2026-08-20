package api

import (
	"context"
	"errors"

	"github.com/adevinta/maiao/pkg/log"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/sirupsen/logrus"
)

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

func NewPullRequester(ctx context.Context, remote *git.Remote) (PullRequester, error) {
	for _, u := range remote.Config().URLs {
		ctx := log.WithContextFields(ctx, logrus.Fields{"remote-url": u})
		endpoint, err := transport.NewEndpoint(u)
		if err != nil {
			log.ForContext(ctx).WithError(err).Errorf("failed to parse remote")
			continue
		}
		r, err := NewGitHubUpserter(ctx, endpoint)
		if err != nil {
			log.ForContext(ctx).WithError(err).Errorf("failed to instanciate github client")
			continue
		}
		return r, nil
	}
	return nil, errors.New("not implemented")
}
