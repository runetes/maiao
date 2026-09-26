package gitlab

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

func TestFindReturnsNilWhenNoMRExists(t *testing.T) {
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("[]"))}, nil
	}))

	pr, err := g.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	assert.Nil(t, pr)
}

func TestFindReturnsExistingMR(t *testing.T) {
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		assert.Contains(t, r.URL.Path, "/merge_requests")
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "maiao.abc123", r.URL.Query().Get("source_branch"), "GitLab filters the branch itself, which is what bounds a state=all listing")
		assert.Equal(t, "all", r.URL.Query().Get("state"))
		body := `[{"iid": 42, "web_url": "https://gitlab.com/owner/repo/-/merge_requests/42", "state": "opened", "target_branch": "main"}]`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, err := g.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
	assert.Equal(t, "https://gitlab.com/owner/repo/-/merge_requests/42", pr.URL)
	assert.Equal(t, "main", pr.Base)
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

// TestFindPicksNewestWhenMultipleMRsMatch covers the wreckage a run that closed a
// merge request and then opened a second one for the same change leaves behind:
// state=all can return more than one match for a source branch, and Find must pick
// one rather than refuse.
func TestFindPicksNewestWhenMultipleMRsMatch(t *testing.T) {
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := `[{"iid": 1, "web_url": "url1", "state": "opened"}, {"iid": 2, "web_url": "url2", "state": "opened"}]`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, err := g.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "2", pr.ID)
}

// TestFindPrefersTheOpenMergeRequestOverANewerMergedOne is the loss picking the
// highest internal ID caused: a source branch carrying an open merge request with the
// review conversation on it, plus a newer one an earlier run left merged on the same
// branch. Reported as Merged, the change is skipped by the parking pass, its stale base
// ends up containing its source branch once the branches are pushed, and GitLab closes
// the open merge request as merged with no way back.
func TestFindPrefersTheOpenMergeRequestOverANewerMergedOne(t *testing.T) {
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := `[
			{"iid": 10, "web_url": "url10", "state": "opened", "target_branch": "main"},
			{"iid": 14, "web_url": "url14", "state": "merged", "target_branch": "main"}
		]`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, err := g.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "10", pr.ID)
	assert.Equal(t, api.PullRequestOpen, pr.State)
}

// TestEnsureOpensANewMergeRequestWhenTheReopenIsRefused pins the fallback. A hard error
// here ended the run after the branches had been pushed: the merge request stayed
// closed, nothing replaced it, and the change was left with a branch on the forge and
// no review. pkg/api.ErrPullRequestNotReopenable is the contract for that refusal.
func TestEnsureOpensANewMergeRequestWhenTheReopenIsRefused(t *testing.T) {
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			body := `[{"iid": 42, "web_url": "https://gitlab.com/owner/repo/-/merge_requests/42", "state": "closed", "target_branch": "deleted-base"}]`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case http.MethodPut:
			body := `{"message": "Cannot transition state via :reopen from :closed"}`
			return &http.Response{StatusCode: http.StatusUnprocessableEntity, Body: io.NopCloser(strings.NewReader(body))}, nil
		case http.MethodPost:
			body := `{"iid": 99, "web_url": "url99", "target_branch": "new-base"}`
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := g.Ensure(context.Background(), api.PullRequestOptions{
		Head: "maiao.abc123",
		Base: "new-base",
	})

	require.NoError(t, err, "a refused reopen must not end the review")
	assert.True(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "99", pr.ID)
}

// TestEnsureClosesTheMergeRequestAgainWhenItsBaseWillNotMove covers the state the two
// requests can be caught between. The reopen has landed and the base move has been
// refused, so the merge request is open on the base an earlier run gave it — the stale
// base the push is about to make contain its own source branch, which is how GitLab
// closes a review as merged with nothing merged. It has to go back to closed before the
// change gets a replacement.
func TestEnsureClosesTheMergeRequestAgainWhenItsBaseWillNotMove(t *testing.T) {
	events := []string{}
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			body := `[{"iid": 42, "web_url": "url42", "state": "closed", "target_branch": "old-base"}]`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case http.MethodPut:
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			switch {
			case reqBody["state_event"] == "reopen":
				events = append(events, "reopen")
				body := `{"iid": 42, "web_url": "url42", "state": "opened", "target_branch": "old-base"}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			case reqBody["target_branch"] == "new-base":
				events = append(events, "retarget")
				body := `{"message": {"target_branch": ["is a protected branch"]}}`
				return &http.Response{StatusCode: http.StatusUnprocessableEntity, Body: io.NopCloser(strings.NewReader(body))}, nil
			case reqBody["state_event"] == "close":
				events = append(events, "close")
				body := `{"iid": 42, "web_url": "url42", "state": "closed", "target_branch": "old-base"}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			default:
				t.Fatalf("unexpected PUT body: %v", reqBody)
				return nil, nil
			}
		case http.MethodPost:
			events = append(events, "create")
			body := `{"iid": 99, "web_url": "url99", "target_branch": "new-base"}`
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := g.Ensure(context.Background(), api.PullRequestOptions{
		Head: "maiao.abc123",
		Base: "new-base",
	})

	require.NoError(t, err)
	assert.True(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "99", pr.ID)
	assert.Equal(t, []string{"reopen", "retarget", "close", "create"}, events)
}

// TestEnsureReopensClosedMR pins the fix: a merge request GitLab closed (but did not
// merge) is reopened and reused rather than left behind for a second merge request to
// open beside it.
// TestEnsureReopensWithOneRequestWhenTheBaseIsAlreadyRight is the common closed
// merge request: nothing was reordered, somebody closed the review by hand.
//
// The second PUT exists only to move a base that moved. Sending it anyway is a write
// that can only go wrong, and the GitHub provider found out the expensive way that a
// needless base edit is refused outright where a native stack owns the branch.
func TestEnsureReopensWithOneRequestWhenTheBaseIsAlreadyRight(t *testing.T) {
	putCalls := 0
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			body := `[{"iid": 42, "web_url": "https://gitlab.com/owner/repo/-/merge_requests/42", "state": "closed", "target_branch": "same-base"}]`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case http.MethodPut:
			putCalls++
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			assert.Equal(t, "reopen", reqBody["state_event"])
			assert.NotContains(t, reqBody, "target_branch")
			body := `{"iid": 42, "web_url": "https://gitlab.com/owner/repo/-/merge_requests/42", "state": "opened", "target_branch": "same-base"}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := g.Ensure(context.Background(), api.PullRequestOptions{
		Head: "maiao.abc123",
		Base: "same-base",
	})
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, 1, putCalls, "the base did not move, so nothing asks GitLab to move it")
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
}

func TestEnsureReopensClosedMR(t *testing.T) {
	createCalled := false
	putCalls := 0
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			body := `[{"iid": 42, "web_url": "https://gitlab.com/owner/repo/-/merge_requests/42", "state": "closed", "target_branch": "old-base"}]`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case http.MethodPut:
			putCalls++
			assert.Contains(t, r.URL.Path, "/merge_requests/42")
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			switch putCalls {
			case 1:
				// Reopen must travel alone: target_branch is silently dropped by
				// GitLab when the merge request is still closed at request time.
				assert.Equal(t, "reopen", reqBody["state_event"])
				assert.NotContains(t, reqBody, "target_branch")
				body := `{"iid": 42, "web_url": "https://gitlab.com/owner/repo/-/merge_requests/42", "state": "opened", "target_branch": "old-base"}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			case 2:
				// Only now, with the merge request open, may the base move.
				assert.Equal(t, "new-base", reqBody["target_branch"])
				assert.NotContains(t, reqBody, "state_event")
				body := `{"iid": 42, "web_url": "https://gitlab.com/owner/repo/-/merge_requests/42", "state": "opened", "target_branch": "new-base"}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			default:
				t.Fatalf("unexpected extra PUT call")
				return nil, nil
			}
		case http.MethodPost:
			createCalled = true
			t.Fatalf("Ensure must reopen the closed merge request, not create a new one")
			return nil, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := g.Ensure(context.Background(), api.PullRequestOptions{
		Head: "maiao.abc123",
		Base: "new-base",
	})
	require.NoError(t, err)
	assert.False(t, created)
	assert.False(t, createCalled)
	assert.Equal(t, 2, putCalls)
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
	assert.Equal(t, "new-base", pr.Base)
}

// TestEnsureCreatesNewMRWhenMerged pins the other half of the fix: GitLab's state
// machine has no transition out of "merged" (app/models/merge_request.rb), so Ensure
// must not try to reopen one and must open a new merge request instead.
func TestEnsureCreatesNewMRWhenMerged(t *testing.T) {
	putCalled := false
	g := newTestGitLab(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			body := `[{"iid": 42, "web_url": "url42", "state": "merged", "target_branch": "old-base"}]`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case http.MethodPut:
			putCalled = true
			t.Fatalf("Ensure must not try to reopen a merged merge request")
			return nil, nil
		case http.MethodPost:
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			assert.Equal(t, "new-base", reqBody["target_branch"])
			body := `{"iid": 99, "web_url": "url99"}`
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := g.Ensure(context.Background(), api.PullRequestOptions{
		Head: "maiao.abc123",
		Base: "new-base",
	})
	require.NoError(t, err)
	assert.True(t, created)
	assert.False(t, putCalled)
	require.NotNil(t, pr)
	assert.Equal(t, "99", pr.ID)
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
