package gitea

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

func newTestBaseClient(rt http.RoundTripper) *BaseClient {
	return &BaseClient{
		Host:       "gitea.example.com",
		Owner:      "owner",
		Repository: "repo",
		HTTPClient: &http.Client{Transport: rt},
		APIBase:    "https://gitea.example.com/api/v1",
	}
}

func TestEnsureReturnsExistingPR(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := `[{"number": 42, "html_url": "https://gitea.example.com/owner/repo/pulls/42", "state": "open", "head": {"ref": "maiao.abc123"}}]`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, created, err := b.Ensure(context.Background(), api.PullRequestOptions{Head: "maiao.abc123"})
	require.NoError(t, err)
	assert.False(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
	assert.Equal(t, "https://gitea.example.com/owner/repo/pulls/42", pr.URL)
}

func TestEnsureCreatesNewPR(t *testing.T) {
	callCount := 0
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		callCount++
		switch r.Method {
		case http.MethodGet:
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("[]"))}, nil
		case http.MethodPost:
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			assert.Equal(t, "maiao.abc123", reqBody["head"])
			assert.Equal(t, "main", reqBody["base"])
			assert.Equal(t, "Test PR", reqBody["title"])
			body := `{"number": 99, "html_url": "https://gitea.example.com/owner/repo/pulls/99"}`
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := b.Ensure(context.Background(), api.PullRequestOptions{
		Head:  "maiao.abc123",
		Base:  "main",
		Title: "Test PR",
	})
	require.NoError(t, err)
	assert.True(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "99", pr.ID)
}

func TestEnsureWithWIPCreatesWIPPR(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("[]"))}, nil
		}
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, "WIP: Test PR", reqBody["title"])
		body := `{"number": 100, "html_url": "url"}`
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	_, _, err := b.Ensure(context.Background(), api.PullRequestOptions{
		Head:  "maiao.abc123",
		Base:  "main",
		Title: "Test PR",
		WIP:   true,
	})
	require.NoError(t, err)
}

func TestEnsureReturnsErrorWhenTooManyPRs(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := `[
			{"number": 1, "html_url": "url1", "head": {"ref": "maiao.abc123"}},
			{"number": 2, "html_url": "url2", "head": {"ref": "maiao.abc123"}}
		]`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, _, err := b.Ensure(context.Background(), api.PullRequestOptions{Head: "maiao.abc123"})
	assert.Nil(t, pr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too many matching pull requests")
}

func TestEnsureFiltersbyHead(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := `[
			{"number": 1, "html_url": "url1", "head": {"ref": "maiao.abc123"}},
			{"number": 2, "html_url": "url2", "head": {"ref": "maiao.other"}}
		]`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, created, err := b.Ensure(context.Background(), api.PullRequestOptions{Head: "maiao.abc123"})
	require.NoError(t, err)
	assert.False(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "1", pr.ID)
}

func TestEnsureReturnsErrorOnAPIFailure(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader("error"))}, nil
	}))

	pr, _, err := b.Ensure(context.Background(), api.PullRequestOptions{Head: "maiao.abc123"})
	assert.Nil(t, pr)
	assert.Error(t, err)
}

func TestUpdatePR(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Contains(t, r.URL.Path, "/pulls/42")
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, "Updated Title", reqBody["title"])
		assert.Equal(t, "Updated Body", reqBody["body"])
		assert.Equal(t, "develop", reqBody["base"])
		body := `{"number": 42, "html_url": "https://gitea.example.com/owner/repo/pulls/42"}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, err := b.Update(context.Background(), &api.PullRequest{ID: "42"}, api.PullRequestOptions{
		Title: "Updated Title",
		Body:  "Updated Body",
		Base:  "develop",
	})
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
}

func TestUpdatePRAddsWIPPrefix(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, "WIP: My Feature", reqBody["title"])
		body := `{"number": 42, "html_url": "url"}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	_, err := b.Update(context.Background(), &api.PullRequest{ID: "42"}, api.PullRequestOptions{
		Title: "My Feature",
		Base:  "main",
		WIP:   true,
	})
	require.NoError(t, err)
}

func TestUpdatePRRemovesWIPPrefix(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, "My Feature", reqBody["title"])
		body := `{"number": 42, "html_url": "url"}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	_, err := b.Update(context.Background(), &api.PullRequest{ID: "42"}, api.PullRequestOptions{
		Title: "WIP: My Feature",
		Base:  "main",
		Ready: true,
	})
	require.NoError(t, err)
}

func TestUpdateReturnsErrorOnAPIFailure(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader("forbidden"))}, nil
	}))

	_, err := b.Update(context.Background(), &api.PullRequest{ID: "42"}, api.PullRequestOptions{
		Title: "Title",
		Base:  "main",
	})
	assert.Error(t, err)
}

func TestDefaultBranch(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/repos/owner/repo")
		body := `{"default_branch": "main"}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	branch := b.DefaultBranch(context.Background())
	assert.Equal(t, "main", branch)
}

func TestDefaultBranchReturnsEmptyOnError(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))

	branch := b.DefaultBranch(context.Background())
	assert.Equal(t, "", branch)
}

func TestLinkedTopicIssues(t *testing.T) {
	b := &BaseClient{Host: "gitea.example.com", Owner: "owner", Repository: "repo"}
	result := b.LinkedTopicIssues("topic-sha")
	assert.Contains(t, result, "gitea.example.com/owner/repo/pulls")
	assert.Contains(t, result, "q=topic-sha")
	assert.Contains(t, result, "state=open")
}

func TestStackManagerReturnsNil(t *testing.T) {
	b := &BaseClient{}
	assert.Nil(t, b.StackManager())
}

func TestNewGiteaUpserterInvalidPath(t *testing.T) {
	g, err := NewGiteaUpserter(context.Background(), &transport.Endpoint{Host: "gitea.example.com", Path: "invalid"})
	assert.Error(t, err)
	assert.Nil(t, g)
}

func TestNewGiteaUpserterNestedPath(t *testing.T) {
	g, err := NewGiteaUpserter(context.Background(), &transport.Endpoint{Host: "gitea.example.com", Path: "/group/subgroup/repo"})
	assert.Error(t, err)
	assert.Nil(t, g)
}

func TestNewGiteaUpserterValidPath(t *testing.T) {
	old := os.Getenv("GITEA_TOKEN")
	defer os.Setenv("GITEA_TOKEN", old)
	os.Setenv("GITEA_TOKEN", "test-token")

	g, err := NewGiteaUpserter(context.Background(), &transport.Endpoint{Host: "gitea.example.com", Path: "/owner/repo.git"})
	require.NoError(t, err)
	require.NotNil(t, g)
	assert.Equal(t, "gitea.example.com", g.Host)
	assert.Equal(t, "owner", g.Owner)
	assert.Equal(t, "repo", g.Repository)
	assert.Equal(t, "https://gitea.example.com/api/v1", g.APIBase)
}

func TestNewGiteaUpserterNoCredentials(t *testing.T) {
	old := os.Getenv("GITEA_TOKEN")
	defer os.Setenv("GITEA_TOKEN", old)
	os.Unsetenv("GITEA_TOKEN")

	g, err := NewGiteaUpserter(context.Background(), &transport.Endpoint{Host: "no-cred-host.example.com", Path: "/owner/repo"})
	assert.Error(t, err)
	assert.Nil(t, g)
}

func TestTokenTransportSetsHeader(t *testing.T) {
	var capturedHeader string
	transport := &tokenTransport{
		token: "my-secret-token",
		delegate: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			capturedHeader = r.Header.Get("Authorization")
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		}),
	}

	req, _ := http.NewRequest(http.MethodGet, "https://gitea.example.com/api/v1/repos", nil)
	transport.RoundTrip(req)
	assert.Equal(t, "token my-secret-token", capturedHeader)
}
