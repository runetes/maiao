package origin

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

func newTestOrigin(rt http.RoundTripper) *Origin {
	return &Origin{
		Owner:      "owner",
		Repo:       "repo",
		HTTPClient: &http.Client{Transport: rt},
		apiBase:    "https://api.cursor.com/v1/origin",
		host:       "origin.cursor.com",
	}
}

func TestEnsureReturnsExistingPR(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, r.Method)
		body := `{"pullRequests": [{"number": "42", "state": "open", "head": {"ref": "maiao.abc123", "sha": "abc123"}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, created, err := o.Ensure(context.Background(), api.PullRequestOptions{Head: "maiao.abc123"})
	require.NoError(t, err)
	assert.False(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
	assert.Equal(t, "https://origin.cursor.com/owner/repo/pull/42", pr.URL)
}

func TestEnsureCreatesNewPR(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"pullRequests": []}`))}, nil
		case http.MethodPost:
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			assert.Equal(t, "Test PR", reqBody["title"])
			assert.Equal(t, "maiao.abc123", reqBody["head"])
			assert.Equal(t, "main", reqBody["base"])
			body := `{"number": "99", "head": {"ref": "maiao.abc123", "sha": "def"}, "base": {"ref": "main", "sha": "abc"}}`
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := o.Ensure(context.Background(), api.PullRequestOptions{
		Head:  "maiao.abc123",
		Base:  "main",
		Title: "Test PR",
	})
	require.NoError(t, err)
	assert.True(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "99", pr.ID)
	assert.Equal(t, "https://origin.cursor.com/owner/repo/pull/99", pr.URL)
}

func TestEnsureCreatesNewPRWithParentPullNumber(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"pullRequests": []}`))}, nil
		case http.MethodPost:
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			assert.Equal(t, "77", reqBody["parentPullNumber"])
			body := `{"number": "100", "head": {"ref": "maiao.abc123", "sha": "def"}, "base": {"ref": "main", "sha": "abc"}}`
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := o.Ensure(context.Background(), api.PullRequestOptions{
		Head:             "maiao.abc123",
		Base:             "main",
		Title:            "Stacked PR",
		ParentPullNumber: "77",
	})
	require.NoError(t, err)
	assert.True(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "100", pr.ID)
}

func TestEnsureCreatesNewPRWithDraft(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"pullRequests": []}`))}, nil
		case http.MethodPost:
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			assert.Equal(t, true, reqBody["draft"])
			body := `{"number": "101", "draft": true, "head": {"ref": "maiao.abc123", "sha": "def"}, "base": {"ref": "main", "sha": "abc"}}`
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := o.Ensure(context.Background(), api.PullRequestOptions{
		Head:  "maiao.abc123",
		Base:  "main",
		Title: "Draft PR",
		WIP:   true,
	})
	require.NoError(t, err)
	assert.True(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "101", pr.ID)
}

func TestEnsureReturnsErrorWhenTooManyPRs(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"pullRequests": [{"number": "1", "head": {"ref": "a", "sha": "x"}}, {"number": "2", "head": {"ref": "b", "sha": "y"}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, _, err := o.Ensure(context.Background(), api.PullRequestOptions{Head: "maiao.abc123"})
	assert.Nil(t, pr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too many matching pull requests")
}

func TestEnsureReturnsErrorOnAPIFailure(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader("error"))}, nil
	}))

	pr, _, err := o.Ensure(context.Background(), api.PullRequestOptions{Head: "maiao.abc123"})
	assert.Nil(t, pr)
	assert.Error(t, err)
}

func TestUpdatePR(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Contains(t, r.URL.Path, "/pulls/42")
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, "Updated Title", reqBody["title"])
		assert.Equal(t, "Updated Body", reqBody["body"])
		assert.Equal(t, "develop", reqBody["base"])
		body := `{"number": "42", "head": {"ref": "feature", "sha": "abc"}, "base": {"ref": "develop", "sha": "def"}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, err := o.Update(context.Background(), &api.PullRequest{ID: "42"}, api.PullRequestOptions{
		Title: "Updated Title",
		Body:  "Updated Body",
		Base:  "develop",
	})
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
}

func TestUpdatePRMarkReady(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, false, reqBody["draft"])
		body := `{"number": "42", "head": {"ref": "feature", "sha": "abc"}, "base": {"ref": "main", "sha": "def"}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	_, err := o.Update(context.Background(), &api.PullRequest{ID: "42"}, api.PullRequestOptions{
		Title: "Title",
		Base:  "main",
		Ready: true,
	})
	require.NoError(t, err)
}

func TestUpdateReturnsErrorOnAPIFailure(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader("forbidden"))}, nil
	}))

	_, err := o.Update(context.Background(), &api.PullRequest{ID: "42"}, api.PullRequestOptions{
		Title: "Title",
		Base:  "main",
	})
	assert.Error(t, err)
}

func TestDefaultBranch(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/repos/owner/repo")
		body := `{"defaultBranch": "main"}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	branch := o.DefaultBranch(context.Background())
	assert.Equal(t, "main", branch)
}

func TestDefaultBranchReturnsEmptyOnError(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))

	branch := o.DefaultBranch(context.Background())
	assert.Equal(t, "", branch)
}

func TestLinkedTopicIssues(t *testing.T) {
	o := &Origin{host: "origin.cursor.com", Owner: "owner", Repo: "repo"}
	result := o.LinkedTopicIssues("topic-sha")
	assert.Contains(t, result, "origin.cursor.com/owner/repo/pulls")
	assert.Contains(t, result, "q=topic-sha")
}

func TestStackManagerReturnsNil(t *testing.T) {
	o := &Origin{}
	assert.Nil(t, o.StackManager())
}

func TestBodyFormatterReturnsHTML(t *testing.T) {
	o := &Origin{}
	f := o.BodyFormatter()
	assert.IsType(t, api.HTMLBodyFormatter{}, f)
}

func TestBearerTransportSetsHeader(t *testing.T) {
	var capturedAuth string
	tr := &bearerTransport{
		token: "my-token",
		delegate: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			capturedAuth = r.Header.Get("Authorization")
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		}),
	}

	req, _ := http.NewRequest(http.MethodGet, "https://api.cursor.com/v1/origin/repos", nil)
	tr.RoundTrip(req)
	assert.Equal(t, "Bearer my-token", capturedAuth)
}

func TestNewOriginUpserterInvalidPath(t *testing.T) {
	o, err := NewOriginUpserter(context.Background(), &transport.Endpoint{Host: "origin.cursor.com", Path: "invalid"})
	assert.Error(t, err)
	assert.Nil(t, o)
}

func TestNewOriginUpserterValidPath(t *testing.T) {
	old := os.Getenv("ORIGIN_TOKEN")
	defer os.Setenv("ORIGIN_TOKEN", old)
	os.Setenv("ORIGIN_TOKEN", "test-token")

	o, err := NewOriginUpserter(context.Background(), &transport.Endpoint{Host: "origin.cursor.com", Path: "/owner/repo.git"})
	require.NoError(t, err)
	require.NotNil(t, o)
	assert.Equal(t, "owner", o.Owner)
	assert.Equal(t, "repo", o.Repo)
	assert.Equal(t, "https://api.cursor.com/v1/origin", o.apiBase)
	assert.Equal(t, "origin.cursor.com", o.host)
}

func TestNewOriginUpserterNoCredentials(t *testing.T) {
	old := os.Getenv("ORIGIN_TOKEN")
	defer os.Setenv("ORIGIN_TOKEN", old)
	os.Unsetenv("ORIGIN_TOKEN")

	o, err := NewOriginUpserter(context.Background(), &transport.Endpoint{Host: "no-cred-host.example.com", Path: "/owner/repo"})
	assert.Error(t, err)
	assert.Nil(t, o)
}
