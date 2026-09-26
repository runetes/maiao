package origin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/runetes/maiao/pkg/api"
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

func TestFindReturnsNilWhenNoPRExists(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"pullRequests": []}`))}, nil
	}))

	pr, err := o.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	assert.Nil(t, pr)
}

func TestFindReturnsExistingPR(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "maiao.abc123", r.URL.Query().Get("head"), "Origin filters the head itself, which is what bounds a state=all listing")
		assert.Equal(t, "all", r.URL.Query().Get("state"))
		body := `{"pullRequests": [{"number": "42", "state": "open", "head": {"ref": "maiao.abc123", "sha": "abc123"}, "base": {"ref": "main", "sha": "def"}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, err := o.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
	assert.Equal(t, "https://origin.cursor.com/owner/repo/pull/42", pr.URL)
	assert.Equal(t, "main", pr.Base)
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

// TestFindPicksNewestWhenNoneIsOpen covers the wreckage a run that closed a pull
// request and then opened a second one for the same change leaves behind: state=all can
// return more than one match for a head branch, and Find must pick one rather than
// refuse.
//
// Both rows are on the head that was asked for and carry the states Origin really sends.
// Rows on other heads proved nothing: Origin filters the head itself, so a listing for
// maiao.abc123 never contains them, and the state Find reported went unchecked.
func TestFindPicksNewestWhenNoneIsOpen(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"pullRequests": [
			{"number": "1", "state": "closed", "merged": false, "head": {"ref": "maiao.abc123", "sha": "x"}, "base": {"ref": "main", "sha": "b1"}},
			{"number": "2", "state": "closed", "merged": true, "head": {"ref": "maiao.abc123", "sha": "y"}, "base": {"ref": "main", "sha": "b2"}}
		]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, err := o.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "2", pr.ID)
	assert.Equal(t, api.PullRequestMerged, pr.State)
}

// TestFindPrefersTheOpenPullRequestOverANewerMergedOne is the loss picking the highest
// number caused: a head carrying an open pull request with the review conversation on
// it, plus a newer one an earlier run left merged on the same head. Reported as Merged,
// the change is skipped by the parking pass, its stale base ends up containing its head
// once the branches are pushed, and the forge closes the open pull request as merged
// with no way back.
func TestFindPrefersTheOpenPullRequestOverANewerMergedOne(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"pullRequests": [
			{"number": "10", "state": "open", "merged": false, "head": {"ref": "maiao.abc123", "sha": "x"}, "base": {"ref": "main", "sha": "b1"}},
			{"number": "14", "state": "closed", "merged": true, "head": {"ref": "maiao.abc123", "sha": "y"}, "base": {"ref": "main", "sha": "b2"}}
		]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, err := o.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "10", pr.ID)
	assert.Equal(t, api.PullRequestOpen, pr.State)
}

// TestFindReadsAStateItDoesNotRecogniseAsOpen: pkg/api documents a state a provider
// does not say as the one every caller treats as open. Defaulting the other way meant
// one unrecognised state string sent a live review to be parked or replaced.
func TestFindReadsAStateItDoesNotRecogniseAsOpen(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"pullRequests": [{"number": "42", "state": "a-state-origin-grew-later", "head": {"ref": "maiao.abc123", "sha": "x"}, "base": {"ref": "main", "sha": "b"}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, err := o.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, api.PullRequestOpen, pr.State)
}

// TestEnsureOpensANewPullRequestWhenTheReopenIsRefused pins the fallback. A hard error
// here ended the run after the branches had been pushed: the pull request stayed closed,
// nothing replaced it, and the change was left with a branch on the forge and no review.
// pkg/api.ErrPullRequestNotReopenable is the contract for that refusal.
func TestEnsureOpensANewPullRequestWhenTheReopenIsRefused(t *testing.T) {
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			body := `{"pullRequests": [{"number": "42", "state": "closed", "merged": false, "head": {"ref": "maiao.abc123", "sha": "x"}, "base": {"ref": "deleted-base", "sha": "y"}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case http.MethodPatch:
			body := `{"code": 9, "message": "base branch deleted-base does not exist"}`
			return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(body))}, nil
		case http.MethodPost:
			body := `{"number": "99", "head": {"ref": "maiao.abc123", "sha": "x"}, "base": {"ref": "new-base", "sha": "z"}}`
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := o.Ensure(context.Background(), api.PullRequestOptions{
		Head: "maiao.abc123",
		Base: "new-base",
	})

	require.NoError(t, err, "a refused reopen must not end the review")
	assert.True(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "99", pr.ID)
}

// TestEnsureReopensClosedPR pins the fix: a pull request Origin closed (but did not
// merge) is reopened and reused rather than left behind for a second pull request to
// open beside it.
// TestEnsureReopensWithoutSendingABaseThatIsAlreadyRight matters more here than on
// the forges whose behaviour could be read from source.
//
// Whether Origin applies a state change before a base change in one request is
// unverified, so the common closed pull request — nothing reordered, somebody closed
// the review — must not depend on it. With the base left out there is no order to get
// wrong.
func TestEnsureReopensWithoutSendingABaseThatIsAlreadyRight(t *testing.T) {
	patched := false
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			body := `{"pullRequests": [{"number": "42", "state": "closed", "merged": false, "head": {"ref": "maiao.abc123", "sha": "x"}, "base": {"ref": "same-base", "sha": "y"}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case http.MethodPatch:
			patched = true
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			assert.Equal(t, "open", reqBody["state"])
			assert.NotContains(t, reqBody, "base")
			body := `{"number": "42", "state": "open", "merged": false, "base": {"ref": "same-base", "sha": "y"}}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := o.Ensure(context.Background(), api.PullRequestOptions{
		Head: "maiao.abc123",
		Base: "same-base",
	})
	require.NoError(t, err)
	assert.False(t, created)
	assert.True(t, patched, "it still has to be reopened")
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
}

func TestEnsureReopensClosedPR(t *testing.T) {
	createCalled := false
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			body := `{"pullRequests": [{"number": "42", "state": "closed", "merged": false, "head": {"ref": "maiao.abc123", "sha": "x"}, "base": {"ref": "old-base", "sha": "y"}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case http.MethodPatch:
			assert.Contains(t, r.URL.Path, "/pulls/42")
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			assert.Equal(t, "open", reqBody["state"])
			assert.Equal(t, "new-base", reqBody["base"])
			body := `{"number": "42", "state": "open", "merged": false, "head": {"ref": "maiao.abc123", "sha": "x"}, "base": {"ref": "new-base", "sha": "z"}}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case http.MethodPost:
			createCalled = true
			t.Fatalf("Ensure must reopen the closed pull request, not create a new one")
			return nil, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := o.Ensure(context.Background(), api.PullRequestOptions{
		Head: "maiao.abc123",
		Base: "new-base",
	})
	require.NoError(t, err)
	assert.False(t, created)
	assert.False(t, createCalled)
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
	assert.Equal(t, "new-base", pr.Base)
}

// TestEnsureCreatesNewPRWhenMerged pins the other half of the fix: Origin's docs say
// "Merged is not writable; use MergePullRequest", so Ensure must not try to reopen a
// merged pull request and must open a new one instead.
func TestEnsureCreatesNewPRWhenMerged(t *testing.T) {
	patchCalled := false
	o := newTestOrigin(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			body := `{"pullRequests": [{"number": "42", "state": "closed", "merged": true, "head": {"ref": "maiao.abc123", "sha": "x"}, "base": {"ref": "old-base", "sha": "y"}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case http.MethodPatch:
			patchCalled = true
			t.Fatalf("Ensure must not try to reopen a merged pull request")
			return nil, nil
		case http.MethodPost:
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			assert.Equal(t, "new-base", reqBody["base"])
			body := `{"number": "99", "head": {"ref": "maiao.abc123", "sha": "x"}, "base": {"ref": "new-base", "sha": "z"}}`
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := o.Ensure(context.Background(), api.PullRequestOptions{
		Head: "maiao.abc123",
		Base: "new-base",
	})
	require.NoError(t, err)
	assert.True(t, created)
	assert.False(t, patchCalled)
	require.NotNil(t, pr)
	assert.Equal(t, "99", pr.ID)
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
