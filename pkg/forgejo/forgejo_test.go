package forgejo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/adevinta/maiao/pkg/api"
	"github.com/adevinta/maiao/pkg/gitea"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripperFunc func(r *http.Request) (*http.Response, error)

func (rt roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return rt(r)
}

func newTestForgejo(rt http.RoundTripper) *Forgejo {
	return &Forgejo{
		BaseClient: gitea.BaseClient{
			Host:       "codeberg.org",
			Owner:      "owner",
			Repository: "repo",
			HTTPClient: &http.Client{Transport: rt},
			APIBase:    "https://codeberg.org/api/v1",
		},
	}
}

func TestEnsureReturnsExistingPR(t *testing.T) {
	f := newTestForgejo(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := `[{"number": 42, "html_url": "https://codeberg.org/owner/repo/pulls/42", "state": "open", "head": {"ref": "maiao.abc123"}}]`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, created, err := f.Ensure(context.Background(), api.PullRequestOptions{Head: "maiao.abc123"})
	require.NoError(t, err)
	assert.False(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
	assert.Equal(t, "https://codeberg.org/owner/repo/pulls/42", pr.URL)
}

func TestEnsureCreatesNewPR(t *testing.T) {
	f := newTestForgejo(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("[]"))}, nil
		case http.MethodPost:
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			assert.Equal(t, "maiao.abc123", reqBody["head"])
			assert.Equal(t, "main", reqBody["base"])
			body := `{"number": 99, "html_url": "https://codeberg.org/owner/repo/pulls/99"}`
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := f.Ensure(context.Background(), api.PullRequestOptions{
		Head:  "maiao.abc123",
		Base:  "main",
		Title: "Test PR",
	})
	require.NoError(t, err)
	assert.True(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "99", pr.ID)
}

func TestUpdatePR(t *testing.T) {
	f := newTestForgejo(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPatch, r.Method)
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		assert.Equal(t, "Updated Title", reqBody["title"])
		body := `{"number": 42, "html_url": "https://codeberg.org/owner/repo/pulls/42"}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, err := f.Update(context.Background(), &api.PullRequest{ID: "42"}, api.PullRequestOptions{
		Title: "Updated Title",
		Body:  "Body",
		Base:  "main",
	})
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
}

func TestDefaultBranch(t *testing.T) {
	f := newTestForgejo(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"default_branch": "main"}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	branch := f.DefaultBranch(context.Background())
	assert.Equal(t, "main", branch)
}

func TestStackManagerReturnsNil(t *testing.T) {
	f := &Forgejo{}
	assert.Nil(t, f.StackManager())
}

func TestNewForgejoUpserterInvalidPath(t *testing.T) {
	f, err := NewForgejoUpserter(context.Background(), &transport.Endpoint{Host: "codeberg.org", Path: "invalid"})
	assert.Error(t, err)
	assert.Nil(t, f)
}

func TestNewForgejoUpserterValidPath(t *testing.T) {
	old := os.Getenv("FORGEJO_TOKEN")
	defer os.Setenv("FORGEJO_TOKEN", old)
	os.Setenv("FORGEJO_TOKEN", "test-token")

	f, err := NewForgejoUpserter(context.Background(), &transport.Endpoint{Host: "codeberg.org", Path: "/owner/repo.git"})
	require.NoError(t, err)
	require.NotNil(t, f)
	assert.Equal(t, "codeberg.org", f.Host)
	assert.Equal(t, "owner", f.Owner)
	assert.Equal(t, "repo", f.Repository)
}

func TestNewForgejoUpserterNoCredentials(t *testing.T) {
	old := os.Getenv("FORGEJO_TOKEN")
	defer os.Setenv("FORGEJO_TOKEN", old)
	os.Unsetenv("FORGEJO_TOKEN")

	f, err := NewForgejoUpserter(context.Background(), &transport.Endpoint{Host: "no-cred-host.example.com", Path: "/owner/repo"})
	assert.Error(t, err)
	assert.Nil(t, f)
}

func TestTokenTransportSetsHeader(t *testing.T) {
	var capturedHeader string
	tr := &tokenTransport{
		token: "my-secret-token",
		delegate: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			capturedHeader = r.Header.Get("Authorization")
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		}),
	}

	req, _ := http.NewRequest(http.MethodGet, "https://codeberg.org/api/v1/repos", nil)
	tr.RoundTrip(req)
	assert.Equal(t, "token my-secret-token", capturedHeader)
}
