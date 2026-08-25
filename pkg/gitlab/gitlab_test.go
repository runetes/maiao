package gitlab

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/adevinta/maiao/pkg/api"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripperFunc func(r *http.Request) (*http.Response, error)

func (rt roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return rt(r)
}

func newTestGitLab(rt http.RoundTripper) *GitLab {
	return &GitLab{
		Host:       "gitlab.com",
		ProjectID:  "owner%2Frepo",
		Owner:      "owner",
		Repository: "repo",
		HTTPClient: &http.Client{Transport: rt},
		apiBase:    "https://gitlab.com/api/v4",
	}
}

func TestEnsureReturnsExistingMR(t *testing.T) {
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Contains(t, r.URL.Path, "/merge_requests")
		assert.Equal(t, http.MethodGet, r.Method)
		body := `[{"iid": 42, "web_url": "https://gitlab.com/owner/repo/-/merge_requests/42", "state": "opened"}]`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, created, err := g.Ensure(context.Background(), api.PullRequestOptions{Head: "maiao.abc123"})
	require.NoError(t, err)
	assert.False(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
	assert.Equal(t, "https://gitlab.com/owner/repo/-/merge_requests/42", pr.URL)
}

func TestEnsureCreatesNewMR(t *testing.T) {
	callCount := 0
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		callCount++
		switch {
		case r.Method == http.MethodGet:
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("[]"))}, nil
		case r.Method == http.MethodPost:
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			assert.Equal(t, "maiao.abc123", reqBody["source_branch"])
			assert.Equal(t, "main", reqBody["target_branch"])
			assert.Equal(t, "Test MR", reqBody["title"])
			body := `{"iid": 99, "web_url": "https://gitlab.com/owner/repo/-/merge_requests/99"}`
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := g.Ensure(context.Background(), api.PullRequestOptions{
		Head:  "maiao.abc123",
		Base:  "main",
		Title: "Test MR",
	})
	require.NoError(t, err)
	assert.True(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "99", pr.ID)
	assert.Equal(t, "https://gitlab.com/owner/repo/-/merge_requests/99", pr.URL)
}

func TestEnsureWithWIPCreatesDraftMR(t *testing.T) {
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("[]"))}, nil
		}
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, "Draft: Test MR", reqBody["title"])
		body := `{"iid": 100, "web_url": "https://gitlab.com/owner/repo/-/merge_requests/100", "draft": true}`
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	_, _, err := g.Ensure(context.Background(), api.PullRequestOptions{
		Head:  "maiao.abc123",
		Base:  "main",
		Title: "Test MR",
		WIP:   true,
	})
	require.NoError(t, err)
}

func TestEnsureReturnsErrorWhenTooManyMRs(t *testing.T) {
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := `[{"iid": 1, "web_url": "url1"}, {"iid": 2, "web_url": "url2"}]`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, _, err := g.Ensure(context.Background(), api.PullRequestOptions{Head: "maiao.abc123"})
	assert.Nil(t, pr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too many matching merge requests")
}

func TestEnsureReturnsErrorOnAPIFailure(t *testing.T) {
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader("error"))}, nil
	}))

	pr, _, err := g.Ensure(context.Background(), api.PullRequestOptions{Head: "maiao.abc123"})
	assert.Nil(t, pr)
	assert.Error(t, err)
}

func TestUpdateMR(t *testing.T) {
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Contains(t, r.URL.Path, "/merge_requests/42")
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, "Updated Title", reqBody["title"])
		assert.Equal(t, "Updated Body", reqBody["description"])
		assert.Equal(t, "develop", reqBody["target_branch"])
		body := `{"iid": 42, "web_url": "https://gitlab.com/owner/repo/-/merge_requests/42"}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, err := g.Update(context.Background(), &api.PullRequest{ID: "42"}, api.PullRequestOptions{
		Title: "Updated Title",
		Body:  "Updated Body",
		Base:  "develop",
	})
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
}

func TestUpdateMRAddsDraftPrefix(t *testing.T) {
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, "Draft: My Feature", reqBody["title"])
		body := `{"iid": 42, "web_url": "url"}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	_, err := g.Update(context.Background(), &api.PullRequest{ID: "42"}, api.PullRequestOptions{
		Title: "My Feature",
		Base:  "main",
		WIP:   true,
	})
	require.NoError(t, err)
}

func TestUpdateMRRemovesDraftPrefix(t *testing.T) {
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, "My Feature", reqBody["title"])
		body := `{"iid": 42, "web_url": "url"}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	_, err := g.Update(context.Background(), &api.PullRequest{ID: "42"}, api.PullRequestOptions{
		Title: "Draft: My Feature",
		Base:  "main",
		Ready: true,
	})
	require.NoError(t, err)
}

func TestUpdateReturnsErrorOnAPIFailure(t *testing.T) {
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader("forbidden"))}, nil
	}))

	_, err := g.Update(context.Background(), &api.PullRequest{ID: "42"}, api.PullRequestOptions{
		Title: "Title",
		Base:  "main",
	})
	assert.Error(t, err)
}

func TestDefaultBranch(t *testing.T) {
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.RawPath, "/projects/owner%2Frepo")
		body := `{"default_branch": "main"}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	branch := g.DefaultBranch(context.Background())
	assert.Equal(t, "main", branch)
}

func TestDefaultBranchReturnsEmptyOnError(t *testing.T) {
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))

	branch := g.DefaultBranch(context.Background())
	assert.Equal(t, "", branch)
}

func TestLinkedTopicIssues(t *testing.T) {
	g := &GitLab{Host: "gitlab.com", Owner: "owner", Repository: "repo"}
	result := g.LinkedTopicIssues("topic-sha")
	assert.Contains(t, result, "gitlab.com/owner/repo/-/merge_requests")
	assert.Contains(t, result, "search=topic-sha")
	assert.Contains(t, result, "state=opened")
}

func TestStackManagerReturnsNil(t *testing.T) {
	g := &GitLab{}
	assert.Nil(t, g.StackManager())
}

func TestNewGitLabUpserterInvalidPath(t *testing.T) {
	g, err := NewGitLabUpserter(context.Background(), &transport.Endpoint{Host: "gitlab.com", Path: "invalid"})
	assert.Error(t, err)
	assert.Nil(t, g)
}

func TestNewGitLabUpserterValidPath(t *testing.T) {
	old := os.Getenv("GITLAB_TOKEN")
	defer os.Setenv("GITLAB_TOKEN", old)
	os.Setenv("GITLAB_TOKEN", "test-token")

	g, err := NewGitLabUpserter(context.Background(), &transport.Endpoint{Host: "gitlab.com", Path: "/owner/repo.git"})
	require.NoError(t, err)
	require.NotNil(t, g)
	assert.Equal(t, "gitlab.com", g.Host)
	assert.Equal(t, "owner", g.Owner)
	assert.Equal(t, "repo", g.Repository)
	assert.Equal(t, "owner%2Frepo", g.ProjectID)
}

func TestNewGitLabUpserterNestedPath(t *testing.T) {
	old := os.Getenv("GITLAB_TOKEN")
	defer os.Setenv("GITLAB_TOKEN", old)
	os.Setenv("GITLAB_TOKEN", "test-token")

	g, err := NewGitLabUpserter(context.Background(), &transport.Endpoint{Host: "gitlab.com", Path: "/group/subgroup/repo.git"})
	require.NoError(t, err)
	require.NotNil(t, g)
	assert.Equal(t, "group", g.Owner)
	assert.Equal(t, "repo", g.Repository)
	assert.Equal(t, "group%2Fsubgroup%2Frepo", g.ProjectID)
}

func TestNewGitLabUpserterNoCredentials(t *testing.T) {
	old := os.Getenv("GITLAB_TOKEN")
	defer os.Setenv("GITLAB_TOKEN", old)
	os.Unsetenv("GITLAB_TOKEN")

	g, err := NewGitLabUpserter(context.Background(), &transport.Endpoint{Host: "no-cred-host.example.com", Path: "/owner/repo"})
	assert.Error(t, err)
	assert.Nil(t, g)
}

func TestTokenTransportSetsHeader(t *testing.T) {
	var capturedHeader string
	transport := &tokenTransport{
		token: "my-secret-token",
		delegate: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			capturedHeader = r.Header.Get("PRIVATE-TOKEN")
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		}),
	}

	req, _ := http.NewRequest(http.MethodGet, "https://gitlab.com/api/v4/projects", nil)
	transport.RoundTrip(req)
	assert.Equal(t, "my-secret-token", capturedHeader)
}
