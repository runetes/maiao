package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-github/v90/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubStackManager_Available_ReturnsTrue(t *testing.T) {
	client := newTestClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Contains(t, r.URL.Path, "/repos/owner/repo/stacks")
		assert.Equal(t, "2026-03-10", r.Header.Get("X-GitHub-Api-Version"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`[]`)),
		}, nil
	}))

	mgr := NewGitHubStackManager(client, "owner", "repo")
	assert.True(t, mgr.Available(context.Background()))
}

func TestGitHubStackManager_Available_ReturnsFalseOn404(t *testing.T) {
	client := newTestClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader(`{"message":"Not Found"}`)),
		}, nil
	}))

	mgr := NewGitHubStackManager(client, "owner", "repo")
	assert.False(t, mgr.Available(context.Background()))
}

func TestGitHubStackManager_Available_CachesResult(t *testing.T) {
	calls := 0
	client := newTestClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`[]`)),
		}, nil
	}))

	mgr := NewGitHubStackManager(client, "owner", "repo")
	mgr.Available(context.Background())
	mgr.Available(context.Background())
	assert.Equal(t, 1, calls)
}

func TestGitHubStackManager_CreateOrUpdateStack(t *testing.T) {
	requestCount := 0
	client := newTestClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		requestCount++
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "pull_request=1"):
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`[]`)),
			}, nil
		case r.Method == http.MethodPost:
			var body createStackRequest
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)
			assert.Equal(t, []int{1, 2, 3}, body.PullRequests)

			resp := stackResponse{
				ID:     42,
				Number: 1,
				PullRequests: []stackPullRequest{
					{Number: 1},
					{Number: 2},
					{Number: 3},
				},
			}
			b, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(strings.NewReader(string(b))),
			}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	mgr := NewGitHubStackManager(client, "owner", "repo")
	stack, err := mgr.CreateOrUpdateStack(context.Background(), []int{1, 2, 3})
	require.NoError(t, err)
	require.NotNil(t, stack)
	assert.Equal(t, "42", stack.ID)
	assert.Equal(t, []int{1, 2, 3}, stack.PRs)
}

func TestGitHubStackManager_CreateOrUpdateStack_ReturnsExistingStack(t *testing.T) {
	client := newTestClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			resp := []stackResponse{{
				ID:     99,
				Number: 1,
				PullRequests: []stackPullRequest{
					{Number: 1},
					{Number: 2},
				},
			}}
			b, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(b))),
			}, nil
		}
		t.Fatal("should not POST when stack already exists")
		return nil, nil
	}))

	mgr := NewGitHubStackManager(client, "owner", "repo")
	stack, err := mgr.CreateOrUpdateStack(context.Background(), []int{1, 2})
	require.NoError(t, err)
	require.NotNil(t, stack)
	assert.Equal(t, "99", stack.ID)
}

func TestGitHubStackManager_GetStack_ReturnsNilWhenEmpty(t *testing.T) {
	client := newTestClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`[]`)),
		}, nil
	}))

	mgr := NewGitHubStackManager(client, "owner", "repo")
	stack, err := mgr.GetStack(context.Background(), 1)
	require.NoError(t, err)
	assert.Nil(t, stack)
}

func TestGitHubStackManager_AvailableChecksAPIVersionHeader(t *testing.T) {
	client := newTestClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "2026-03-10", r.Header.Get("X-GitHub-Api-Version"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`[]`)),
		}, nil
	}))

	mgr := NewGitHubStackManager(client, "owner", "repo")
	mgr.Available(context.Background())
}

func newTestClientFromFunc(rt func(*http.Request) (*http.Response, error)) *github.Client {
	c, _ := github.NewClient(github.WithHTTPClient(&http.Client{Transport: roundTripperFunc(rt)}))
	return c
}
