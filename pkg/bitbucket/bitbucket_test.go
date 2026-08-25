package bitbucket

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

func newTestBitbucket(rt http.RoundTripper) *Bitbucket {
	return &Bitbucket{
		Host:       "bitbucket.org",
		Workspace:  "workspace",
		RepoSlug:   "repo",
		HTTPClient: &http.Client{Transport: rt},
		apiBase:    "https://api.bitbucket.org/2.0",
	}
}

func TestEnsureReturnsExistingPR(t *testing.T) {
	b := newTestBitbucket(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, r.Method)
		body := `{"values": [{"id": 42, "links": {"html": {"href": "https://bitbucket.org/workspace/repo/pull-requests/42"}}, "state": "OPEN", "source": {"branch": {"name": "maiao.abc123"}}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, created, err := b.Ensure(context.Background(), api.PullRequestOptions{Head: "maiao.abc123"})
	require.NoError(t, err)
	assert.False(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
	assert.Equal(t, "https://bitbucket.org/workspace/repo/pull-requests/42", pr.URL)
}

func TestEnsureCreatesNewPR(t *testing.T) {
	b := newTestBitbucket(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"values": []}`))}, nil
		case http.MethodPost:
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			assert.Equal(t, "Test PR", reqBody["title"])
			source := reqBody["source"].(map[string]interface{})
			branch := source["branch"].(map[string]interface{})
			assert.Equal(t, "maiao.abc123", branch["name"])
			body := `{"id": 99, "links": {"html": {"href": "https://bitbucket.org/workspace/repo/pull-requests/99"}}}`
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
	assert.Equal(t, "https://bitbucket.org/workspace/repo/pull-requests/99", pr.URL)
}

func TestEnsureReturnsErrorWhenTooManyPRs(t *testing.T) {
	b := newTestBitbucket(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"values": [{"id": 1, "links": {"html": {"href": "url1"}}}, {"id": 2, "links": {"html": {"href": "url2"}}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, _, err := b.Ensure(context.Background(), api.PullRequestOptions{Head: "maiao.abc123"})
	assert.Nil(t, pr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too many matching pull requests")
}

func TestEnsureReturnsErrorOnAPIFailure(t *testing.T) {
	b := newTestBitbucket(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader("error"))}, nil
	}))

	pr, _, err := b.Ensure(context.Background(), api.PullRequestOptions{Head: "maiao.abc123"})
	assert.Nil(t, pr)
	assert.Error(t, err)
}

func TestUpdatePR(t *testing.T) {
	b := newTestBitbucket(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Contains(t, r.URL.Path, "/pullrequests/42")
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, "Updated Title", reqBody["title"])
		assert.Equal(t, "Updated Body", reqBody["description"])
		dest := reqBody["destination"].(map[string]interface{})
		branch := dest["branch"].(map[string]interface{})
		assert.Equal(t, "develop", branch["name"])
		body := `{"id": 42, "links": {"html": {"href": "https://bitbucket.org/workspace/repo/pull-requests/42"}}}`
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

func TestUpdateReturnsErrorOnAPIFailure(t *testing.T) {
	b := newTestBitbucket(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader("forbidden"))}, nil
	}))

	_, err := b.Update(context.Background(), &api.PullRequest{ID: "42"}, api.PullRequestOptions{
		Title: "Title",
		Base:  "main",
	})
	assert.Error(t, err)
}

func TestDefaultBranch(t *testing.T) {
	b := newTestBitbucket(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/repositories/workspace/repo")
		body := `{"mainbranch": {"name": "main"}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	branch := b.DefaultBranch(context.Background())
	assert.Equal(t, "main", branch)
}

func TestDefaultBranchReturnsEmptyOnError(t *testing.T) {
	b := newTestBitbucket(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))

	branch := b.DefaultBranch(context.Background())
	assert.Equal(t, "", branch)
}

func TestLinkedTopicIssues(t *testing.T) {
	b := &Bitbucket{Host: "bitbucket.org", Workspace: "workspace", RepoSlug: "repo"}
	result := b.LinkedTopicIssues("topic-sha")
	assert.Contains(t, result, "bitbucket.org/workspace/repo/pull-requests")
	assert.Contains(t, result, "search_query=topic-sha")
}

func TestStackManagerReturnsNil(t *testing.T) {
	b := &Bitbucket{}
	assert.Nil(t, b.StackManager())
}

func TestNewBitbucketUpserterInvalidPath(t *testing.T) {
	b, err := NewBitbucketUpserter(context.Background(), &transport.Endpoint{Host: "bitbucket.org", Path: "invalid"})
	assert.Error(t, err)
	assert.Nil(t, b)
}

func TestNewBitbucketUpserterValidPath(t *testing.T) {
	old := os.Getenv("BITBUCKET_TOKEN")
	defer os.Setenv("BITBUCKET_TOKEN", old)
	os.Setenv("BITBUCKET_TOKEN", "test-token")

	b, err := NewBitbucketUpserter(context.Background(), &transport.Endpoint{Host: "bitbucket.org", Path: "/workspace/repo.git"})
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, "bitbucket.org", b.Host)
	assert.Equal(t, "workspace", b.Workspace)
	assert.Equal(t, "repo", b.RepoSlug)
	assert.Equal(t, "https://api.bitbucket.org/2.0", b.apiBase)
}

func TestNewBitbucketUpserterNoCredentials(t *testing.T) {
	old := os.Getenv("BITBUCKET_TOKEN")
	defer os.Setenv("BITBUCKET_TOKEN", old)
	os.Unsetenv("BITBUCKET_TOKEN")

	b, err := NewBitbucketUpserter(context.Background(), &transport.Endpoint{Host: "no-cred-host.example.com", Path: "/workspace/repo"})
	assert.Error(t, err)
	assert.Nil(t, b)
}

func TestBasicAuthTransportSetsHeader(t *testing.T) {
	var capturedAuth string
	tr := &basicAuthTransport{
		username: "user",
		password: "pass",
		delegate: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			capturedAuth = r.Header.Get("Authorization")
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		}),
	}

	req, _ := http.NewRequest(http.MethodGet, "https://api.bitbucket.org/2.0/repos", nil)
	tr.RoundTrip(req)
	assert.NotEmpty(t, capturedAuth)
	assert.Contains(t, capturedAuth, "Basic")
}

func TestListPRsUsesQueryFilter(t *testing.T) {
	b := newTestBitbucket(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Contains(t, r.URL.RawQuery, "source.branch.name")
		assert.Contains(t, r.URL.RawQuery, "maiao.abc123")
		body := `{"values": []}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	prs, err := b.listPRs(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	assert.Empty(t, prs)
}
