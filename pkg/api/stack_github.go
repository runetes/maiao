package api

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/adevinta/maiao/pkg/log"
	"github.com/google/go-github/v90/github"
)

const stackAPIVersion = "2026-03-10"

// GitHubStackManager implements StackManager using the GitHub REST API.
type GitHubStackManager struct {
	client     *github.Client
	owner      string
	repository string

	availableOnce sync.Once
	available     bool
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
	s.availableOnce.Do(func() {
		s.available = s.probeAvailability(ctx)
	})
	return s.available
}

func (s *GitHubStackManager) probeAvailability(ctx context.Context) bool {
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
		log.ForContext(ctx).WithField("stackID", existing.ID).Debug("stack already exists")
		return existing, nil
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
		ID:  fmt.Sprintf("%d", resp.ID),
		PRs: prs,
	}
}
