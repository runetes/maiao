package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/go-github/v90/github"
	"github.com/runetes/maiao/pkg/log"
)

const stackAPIVersion = "2026-03-10"

// GitHubStackManager implements StackManager using the GitHub REST API.
type GitHubStackManager struct {
	client     *github.Client
	owner      string
	repository string
}

// NewGitHubStackManager creates a new GitHubStackManager.
func NewGitHubStackManager(client *github.Client, owner, repository string) *GitHubStackManager {
	return &GitHubStackManager{
		client:     client,
		owner:      owner,
		repository: repository,
	}
}

func (s *GitHubStackManager) Available(ctx context.Context) bool {
	url := fmt.Sprintf("repos/%s/%s/stacks?per_page=1", s.owner, s.repository)
	req, err := s.client.NewRequest(ctx, http.MethodGet, url, nil, github.WithVersion(stackAPIVersion))
	if err != nil {
		return false
	}
	resp, err := s.client.Do(req, nil)
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnsupportedMediaType) {
			return false
		}
		return false
	}
	return resp.StatusCode == http.StatusOK
}

type createStackRequest struct {
	PullRequests []int `json:"pull_requests"`
}

type stackPullRequest struct {
	Number int `json:"number"`
}

type stackResponse struct {
	ID           int64              `json:"id"`
	Number       int                `json:"number"`
	PullRequests []stackPullRequest `json:"pull_requests"`
}

func (s *GitHubStackManager) CreateOrUpdateStack(ctx context.Context, prNumbers []int) (*Stack, error) {
	existing, err := s.GetStack(ctx, prNumbers[0])
	if err == nil && existing != nil {
		log.ForContext(ctx).WithField("stackID", existing.ID).Debug("stack already exists, updating")
		return s.addToStack(ctx, existing, prNumbers)
	}

	url := fmt.Sprintf("repos/%s/%s/stacks", s.owner, s.repository)
	body := createStackRequest{PullRequests: prNumbers}
	req, err := s.client.NewRequest(ctx, http.MethodPost, url, body, github.WithVersion(stackAPIVersion))
	if err != nil {
		return nil, fmt.Errorf("creating stack request: %w", err)
	}

	var resp stackResponse
	_, err = s.client.Do(req, &resp)
	if err != nil {
		return nil, fmt.Errorf("creating stack: %w", err)
	}

	log.ForContext(ctx).WithField("stackID", resp.ID).Debug("registered GitHub native stack")
	return stackFromResponse(&resp), nil
}

func (s *GitHubStackManager) addToStack(ctx context.Context, stack *Stack, prNumbers []int) (*Stack, error) {
	existing := make(map[int]bool, len(stack.PRs))
	for _, pr := range stack.PRs {
		existing[pr] = true
	}
	var toAdd []int
	for _, pr := range prNumbers {
		if !existing[pr] {
			toAdd = append(toAdd, pr)
		}
	}
	if len(toAdd) == 0 {
		return stack, nil
	}

	url := fmt.Sprintf("repos/%s/%s/stacks/%s/add", s.owner, s.repository, stack.ID)
	body := createStackRequest{PullRequests: toAdd}
	req, err := s.client.NewRequest(ctx, http.MethodPost, url, body, github.WithVersion(stackAPIVersion))
	if err != nil {
		return nil, fmt.Errorf("adding PRs to stack request: %w", err)
	}

	var resp stackResponse
	_, err = s.client.Do(req, &resp)
	if err != nil {
		return nil, fmt.Errorf("adding PRs to stack: %w", err)
	}

	log.ForContext(ctx).WithField("stackID", stack.ID).Debug("updated GitHub native stack")
	return stackFromResponse(&resp), nil
}

// Unstack dissolves the stack.
//
// The documented call is a POST to the stack's unstack endpoint with no body; a
// DELETE on the stack itself is not part of the published API.
// See https://docs.github.com/en/rest/pulls/stacks.
func (s *GitHubStackManager) Unstack(ctx context.Context, stackID string) error {
	url := fmt.Sprintf("repos/%s/%s/stacks/%s/unstack", s.owner, s.repository, stackID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, url, nil, github.WithVersion(stackAPIVersion))
	if err != nil {
		return fmt.Errorf("creating unstack request: %w", err)
	}
	// 200 answers that pull requests are still in the stack, because they could not
	// be unstacked — queued for merge, most often. They keep their base branch
	// locked, which the caller finds out when the base it asked for does not take.
	var remaining stackResponse
	resp, err := s.client.Do(req, &remaining)
	if err != nil {
		return fmt.Errorf("unstacking: %w", err)
	}
	if resp != nil && resp.StatusCode == http.StatusOK && len(remaining.PullRequests) > 0 {
		log.ForContext(ctx).
			WithField("stackID", stackID).
			WithField("remaining", len(remaining.PullRequests)).
			Warn("some pull requests could not be unstacked and keep their base branch")
		return nil
	}
	log.ForContext(ctx).WithField("stackID", stackID).Debug("dissolved GitHub native stack")
	return nil
}

func (s *GitHubStackManager) GetStack(ctx context.Context, prNumber int) (*Stack, error) {
	url := fmt.Sprintf("repos/%s/%s/stacks?pull_request=%d", s.owner, s.repository, prNumber)
	req, err := s.client.NewRequest(ctx, http.MethodGet, url, nil, github.WithVersion(stackAPIVersion))
	if err != nil {
		return nil, err
	}

	var stacks []stackResponse
	_, err = s.client.Do(req, &stacks)
	if err != nil {
		return nil, err
	}

	if len(stacks) == 0 {
		return nil, nil
	}

	return stackFromResponse(&stacks[0]), nil
}

func stackFromResponse(resp *stackResponse) *Stack {
	prs := make([]int, len(resp.PullRequests))
	for i, p := range resp.PullRequests {
		prs[i] = p.Number
	}
	return &Stack{
		ID:  fmt.Sprintf("%d", resp.Number),
		PRs: prs,
	}
}
