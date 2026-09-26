package gitea

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/runetes/maiao/pkg/api"
	"github.com/runetes/maiao/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripperFunc func(r *http.Request) (*http.Response, error)

func (rt roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return rt(r)
}

// firstPageOnly answers body for page 1 and an empty page for any page after it,
// which is what Gitea does once its history runs out and what ends Find's walk.
func firstPageOnly(r *http.Request, body string) *http.Response {
	if r.URL.Query().Get("page") != "1" {
		body = "[]"
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
}

// listing serves a paginated pull request listing the way Gitea does — rows keyed by
// page number, any page nobody put rows on empty — and records the pages asked for, so
// a test can pin the pagination that goes out and not only what comes back.
type listing struct {
	t         *testing.T
	pages     map[int]string
	requested []int
}

func (l *listing) RoundTrip(r *http.Request) (*http.Response, error) {
	assert.Equal(l.t, http.MethodGet, r.Method)
	query := r.URL.Query()
	assert.Equal(l.t, "all", query.Get("state"))
	assert.Equal(l.t, strconv.Itoa(listPageSize), query.Get("limit"), "a page has to be asked for at Gitea's cap, not left at DEFAULT_PAGING_NUM")
	page, err := strconv.Atoi(query.Get("page"))
	require.NoError(l.t, err, "each page has to be asked for by number")
	l.requested = append(l.requested, page)
	body, ok := l.pages[page]
	if !ok {
		body = "[]"
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
}

// otherHeads renders a full page of pull requests on heads no lookup asks about: the
// busy repository in which the change's own pull request is not on page 1.
func otherHeads(count int) string {
	rows := make([]string, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, fmt.Sprintf(`{"number": %d, "state": "open", "head": {"ref": "maiao.other%d"}}`, i+1, i))
	}
	return "[" + strings.Join(rows, ",") + "]"
}

// captureStderr runs f with os.Stderr redirected and returns what it wrote there. That
// line is the user's only sign that a conversation was left behind — the default
// verbosity prints no logs — so it is asserted rather than assumed.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	saved := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = saved }()
	f()
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
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

func TestFindReturnsNilWhenNoPRExists(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("[]"))}, nil
	}))

	pr, err := b.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	assert.Nil(t, pr)
}

func TestFindReturnsExistingPR(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := `[{"number": 42, "html_url": "https://gitea.example.com/owner/repo/pulls/42", "state": "open", "head": {"ref": "maiao.abc123"}, "base": {"ref": "main"}}]`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))

	pr, err := b.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
	assert.Equal(t, "https://gitea.example.com/owner/repo/pulls/42", pr.URL)
	assert.Equal(t, "main", pr.Base)
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

// TestFindPicksNewestWhenNoneIsOpen covers the wreckage a run that closed a pull
// request and then opened a second one for the same change leaves behind: state=all
// can return more than one match for a head branch, and Find must pick one rather
// than refuse.
//
// The rows carry the states Gitea really sends, and the state Find reports is asserted
// too. Without both, a lookup that mistook the pull request it picked for closed —
// which is what sends a live review to be replaced instead of reused — read as a pass.
func TestFindPicksNewestWhenNoneIsOpen(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return firstPageOnly(r, `[
			{"number": 1, "html_url": "url1", "state": "closed", "merged": false, "head": {"ref": "maiao.abc123"}},
			{"number": 2, "html_url": "url2", "state": "closed", "merged": true, "head": {"ref": "maiao.abc123"}}
		]`), nil
	}))

	pr, err := b.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "2", pr.ID)
	assert.Equal(t, api.PullRequestMerged, pr.State)
}

// TestFindPrefersTheOpenPullRequestOverANewerMergedOne is the loss picking the highest
// number caused: a head carrying an open pull request with the review conversation on
// it, plus a newer one an earlier run left closed-and-merged on the same head. Reported
// as Merged, the change is skipped by the parking pass, its stale base ends up
// containing its head once the branches are pushed, and the forge closes the open pull
// request as merged with no way back.
func TestFindPrefersTheOpenPullRequestOverANewerMergedOne(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return firstPageOnly(r, `[
			{"number": 10, "html_url": "url10", "state": "open", "merged": false, "head": {"ref": "maiao.abc123"}, "base": {"ref": "main"}},
			{"number": 14, "html_url": "url14", "state": "closed", "merged": true, "head": {"ref": "maiao.abc123"}, "base": {"ref": "main"}}
		]`), nil
	}))

	pr, err := b.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "10", pr.ID)
	assert.Equal(t, api.PullRequestOpen, pr.State)
}

// TestFindWalksPagesBecauseGiteaHasNoHeadFilter pins the fix for a lookup that could
// not find the pull request at all. Gitea's list endpoint filters by base_branch and
// nothing else, so with state=all the candidate set is the repository's whole history
// and the change's pull request is routinely not on the first page.
func TestFindWalksPagesBecauseGiteaHasNoHeadFilter(t *testing.T) {
	l := &listing{t: t, pages: map[int]string{
		1: otherHeads(listPageSize),
		2: `[{"number": 60, "html_url": "url60", "state": "open", "head": {"ref": "maiao.abc123"}, "base": {"ref": "main"}}]`,
	}}
	b := newTestBaseClient(l)

	pr, err := b.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	require.NotNil(t, pr, "the head is on page 2, which is where a busy repository puts it")
	assert.Equal(t, "60", pr.ID)
	assert.Equal(t, api.PullRequestOpen, pr.State)
	assert.Equal(t, []int{1, 2}, l.requested)
}

// TestFindStopsAtAShortPage: a page carrying fewer rows than the ones before it is the
// last one Gitea has, so there is nothing to ask for after it.
func TestFindStopsAtAShortPage(t *testing.T) {
	l := &listing{t: t, pages: map[int]string{
		1: otherHeads(listPageSize),
		2: otherHeads(3),
	}}
	b := newTestBaseClient(l)

	pr, err := b.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	assert.Nil(t, pr)
	assert.Equal(t, []int{1, 2}, l.requested)
}

// TestFindStopsAtThePageCeiling guards the walk against a repository — or a forge bug —
// that keeps serving full pages: a lookup that cannot find its head still has to
// return, reporting what it found.
func TestFindStopsAtThePageCeiling(t *testing.T) {
	pages := map[int]string{}
	for page := 1; page <= listMaxPages*2; page++ {
		pages[page] = otherHeads(listPageSize)
	}
	l := &listing{t: t, pages: pages}
	b := newTestBaseClient(l)

	pr, err := b.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	assert.Nil(t, pr)
	assert.Len(t, l.requested, listMaxPages)
}

// TestFindStopsAtAnOpenMatchWithoutReadingTheWholeHistory: an open pull request is the
// live review and the one Find reports whatever older pages hold, so the walk ends
// there rather than reading a busy repository's history on every review.
func TestFindStopsAtAnOpenMatchWithoutReadingTheWholeHistory(t *testing.T) {
	pages := map[int]string{
		1: `[{"number": 7, "html_url": "url7", "state": "open", "head": {"ref": "maiao.abc123"}, "base": {"ref": "main"}}]`,
	}
	for page := 2; page <= listMaxPages; page++ {
		pages[page] = otherHeads(listPageSize)
	}
	l := &listing{t: t, pages: pages}
	b := newTestBaseClient(l)

	pr, err := b.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "7", pr.ID)
	assert.Equal(t, []int{1}, l.requested)
}

// TestFindReadsAStateItDoesNotRecogniseAsOpen: pkg/api documents a state a provider
// does not say as the one every caller treats as open. Defaulting the other way meant
// one unrecognised state string sent a live review to be parked or replaced.
func TestFindReadsAStateItDoesNotRecogniseAsOpen(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return firstPageOnly(r, `[{"number": 42, "html_url": "url42", "state": "a-state-gitea-grew-later", "head": {"ref": "maiao.abc123"}, "base": {"ref": "main"}}]`), nil
	}))

	pr, err := b.Find(context.Background(), "maiao.abc123")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, api.PullRequestOpen, pr.State)
}

// TestEnsureReopensClosedPR pins the fix: a pull request Gitea closed (but did not
// merge) is reopened and reused rather than left behind for a second pull request to
// open beside it.
func TestEnsureReopensClosedPR(t *testing.T) {
	createCalled := false
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			return firstPageOnly(r, `[{"number": 42, "html_url": "https://gitea.example.com/owner/repo/pulls/42", "state": "closed", "merged": false, "head": {"ref": "maiao.abc123"}, "base": {"ref": "old-base"}}]`), nil
		case http.MethodPatch:
			assert.Contains(t, r.URL.Path, "/pulls/42")
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			assert.Equal(t, "open", reqBody["state"])
			assert.Equal(t, "new-base", reqBody["base"])
			body := `{"number": 42, "html_url": "https://gitea.example.com/owner/repo/pulls/42", "state": "open", "merged": false, "base": {"ref": "new-base"}}`
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

	pr, created, err := b.Ensure(context.Background(), api.PullRequestOptions{
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

// TestEnsureReopensWithoutSendingABaseThatIsAlreadyRight is the common closed pull
// request: nothing was reordered, somebody closed the review by hand.
//
// Gitea skips a base equal to the one the pull request already has, so sending it
// changes nothing; not sending it keeps the request to the one field it needs. The
// GitHub provider learned this the hard way — there, a needless base edit is refused
// outright when a native stack owns the branch.
func TestEnsureReopensWithoutSendingABaseThatIsAlreadyRight(t *testing.T) {
	patched := false
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			return firstPageOnly(r, `[{"number": 42, "html_url": "https://gitea.example.com/owner/repo/pulls/42", "state": "closed", "merged": false, "head": {"ref": "maiao.abc123"}, "base": {"ref": "same-base"}}]`), nil
		case http.MethodPatch:
			patched = true
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			assert.Equal(t, "open", reqBody["state"])
			assert.NotContains(t, reqBody, "base")
			body := `{"number": 42, "html_url": "https://gitea.example.com/owner/repo/pulls/42", "state": "open", "merged": false, "base": {"ref": "same-base"}}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := b.Ensure(context.Background(), api.PullRequestOptions{
		Head: "maiao.abc123",
		Base: "same-base",
	})
	require.NoError(t, err)
	assert.False(t, created)
	assert.True(t, patched, "it still has to be reopened")
	require.NotNil(t, pr)
	assert.Equal(t, "42", pr.ID)
}

// TestEnsureCreatesNewPRWhenMerged pins the other half of the fix: Gitea refuses to
// reopen a merged pull request (412, go-gitea/gitea PR #17192), so Ensure must not
// try and must open a new one instead.
func TestEnsureCreatesNewPRWhenMerged(t *testing.T) {
	patchCalled := false
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			return firstPageOnly(r, `[{"number": 42, "html_url": "url42", "state": "closed", "merged": true, "head": {"ref": "maiao.abc123"}, "base": {"ref": "old-base"}}]`), nil
		case http.MethodPatch:
			patchCalled = true
			t.Fatalf("Ensure must not try to reopen a merged pull request")
			return nil, nil
		case http.MethodPost:
			var reqBody map[string]interface{}
			json.NewDecoder(r.Body).Decode(&reqBody)
			assert.Equal(t, "new-base", reqBody["base"])
			body := `{"number": 99, "html_url": "url99"}`
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	pr, created, err := b.Ensure(context.Background(), api.PullRequestOptions{
		Head: "maiao.abc123",
		Base: "new-base",
	})
	require.NoError(t, err)
	assert.True(t, created)
	assert.False(t, patchCalled)
	require.NotNil(t, pr)
	assert.Equal(t, "99", pr.ID)
}

// TestEnsureOpensANewPullRequestWhenTheReopenIsRefused pins the fallback. A hard
// error here ended the run after the branches had been pushed: the pull request stayed
// closed, nothing replaced it, and the change was left with a branch on the forge and
// no review. pkg/api.ErrPullRequestNotReopenable is the contract for that refusal, and
// a new pull request plus a line on stderr is what the contract asks for.
func TestEnsureOpensANewPullRequestWhenTheReopenIsRefused(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			return firstPageOnly(r, `[{"number": 42, "html_url": "https://gitea.example.com/owner/repo/pulls/42", "state": "closed", "merged": false, "head": {"ref": "maiao.abc123"}, "base": {"ref": "deleted-base"}}]`), nil
		case http.MethodPatch:
			body := `{"message": "cannot change state of this pull request, it was already merged"}`
			return &http.Response{StatusCode: http.StatusPreconditionFailed, Body: io.NopCloser(strings.NewReader(body))}, nil
		case http.MethodPost:
			body := `{"number": 99, "html_url": "url99", "base": {"ref": "new-base"}}`
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	}))

	var pr *api.PullRequest
	var created bool
	var err error
	stderr := captureStderr(t, func() {
		pr, created, err = b.Ensure(context.Background(), api.PullRequestOptions{
			Head: "maiao.abc123",
			Base: "new-base",
		})
	})

	require.NoError(t, err, "a refused reopen must not end the review")
	assert.True(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "99", pr.ID)
	assert.Contains(t, stderr, "https://gitea.example.com/owner/repo/pulls/42", "the user has to be told which conversation is left behind")
}

func TestEnsureFiltersbyHead(t *testing.T) {
	b := newTestBaseClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return firstPageOnly(r, `[
			{"number": 1, "html_url": "url1", "state": "open", "head": {"ref": "maiao.abc123"}},
			{"number": 2, "html_url": "url2", "state": "open", "head": {"ref": "maiao.other"}}
		]`), nil
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

// capturedAuth sends one request through tr and reports the Authorization
// header the server would have seen.
func capturedAuth(t *testing.T, cred *credentials.Credentials) *http.Request {
	t.Helper()
	var captured *http.Request
	tr := NewAuthTransport(cred, roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		captured = r
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))

	req, err := http.NewRequest(http.MethodGet, "https://gitea.example.com/api/v1/repos", nil)
	require.NoError(t, err)
	_, err = tr.RoundTrip(req)
	require.NoError(t, err)
	require.NotNil(t, captured)
	return captured
}

func TestAuthTransportSendsATokenAsATokenHeader(t *testing.T) {
	req := capturedAuth(t, &credentials.Credentials{Password: "my-secret-token"})

	assert.Equal(t, "token my-secret-token", req.Header.Get("Authorization"))
}

// TestAuthTransportSendsANamedCredentialAsBasicAuth covers the entry maiao's own
// documentation tells users to write, `login <user> password <secret>`. Gitea
// checks a basic password as an access token before it tries it as a password
// (services/auth/basic.go), so basic auth carries both kinds of secret, where
// `Authorization: token` carries only one.
func TestAuthTransportSendsANamedCredentialAsBasicAuth(t *testing.T) {
	req := capturedAuth(t, &credentials.Credentials{Username: "me", Password: "my-secret"})

	user, password, ok := req.BasicAuth()
	require.True(t, ok, "a credential with a username must go out as basic auth")
	assert.Equal(t, "me", user)
	assert.Equal(t, "my-secret", password)
}

// TestAuthTransportLeavesTheCallersRequestAlone matters because a RoundTripper
// may be handed the same request again on a retry or redirect.
func TestAuthTransportLeavesTheCallersRequestAlone(t *testing.T) {
	tr := NewAuthTransport(&credentials.Credentials{Password: "t"}, roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))

	req, err := http.NewRequest(http.MethodGet, "https://gitea.example.com/api/v1/repos", nil)
	require.NoError(t, err)
	_, err = tr.RoundTrip(req)

	require.NoError(t, err)
	assert.Empty(t, req.Header.Get("Authorization"))
}
