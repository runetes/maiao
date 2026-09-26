package gitea

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/runetes/maiao/pkg/api"
	"github.com/runetes/maiao/pkg/log"
)

type BaseClient struct {
	Host       string
	Owner      string
	Repository string
	HTTPClient *http.Client
	APIBase    string
}

type pullRequest struct {
	ID      int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Title   string `json:"title"`
	// State is only ever "open" or "closed" — Gitea reports a merge through the
	// separate Merged flag below, not a third state value.
	// https://docs.gitea.com/api/1.27/operations/repo-list-pull-requests
	State  string `json:"state"`
	Merged bool   `json:"merged"`
	Head   struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

// giteaState maps what Gitea reports onto the states a review can act on.
// https://docs.gitea.com/api/1.27/operations/repo-list-pull-requests
// A state Gitea does not report, or one this does not recognise, is taken to be open:
// pkg/api.PullRequest documents an unsaid state as the one every caller treats as
// open, and the costs are asymmetric — reading an open pull request as closed parks or
// replaces a live review, while reading a dead one as open at worst reuses something
// the forge then refuses to move.
func giteaState(pr pullRequest) api.PullRequestState {
	if pr.Merged {
		return api.PullRequestMerged
	}
	if pr.State == "closed" {
		return api.PullRequestClosed
	}
	return api.PullRequestOpen
}

type repository struct {
	DefaultBranch string `json:"default_branch"`
}

// Find looks up the pull request for head, whatever state Gitea reports it in. It
// owns the list-and-pick switch so Ensure cannot drift from what a plain lookup
// reports.
//
// Listing state=all rather than the open-only default is what lets a review reuse a
// pull request an earlier run left closed instead of opening a second one beside it.
// https://docs.gitea.com/api/1.27/operations/repo-list-pull-requests
func (b *BaseClient) Find(ctx context.Context, head string) (*api.PullRequest, error) {
	prs, err := b.listPRs(ctx, head)
	if err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	picked := pick(prs)
	return &api.PullRequest{
		ID:    fmt.Sprintf("%d", picked.ID),
		URL:   picked.HTMLURL,
		Base:  picked.Base.Ref,
		State: giteaState(picked),
	}, nil
}

// pick chooses which of several pull requests on one head branch is the change's.
//
// state=all can return more than one where the open-only listing returned at most one;
// the extras are the wreckage of an earlier run rather than an ambiguity to refuse.
//
// An open one wins over any closed or merged one, however much newer that one is,
// because it is the live review. Taking the highest number instead reported a head
// whose open pull request carried the conversation as Merged, which makes the parking
// pass skip it, so the push leaves its stale base containing its head and the forge
// closes it as merged — irreversibly. Among equals the highest number wins, numbers
// only growing, which keeps the answer independent of the order Gitea lists in;
// the endpoint promises none.
// https://docs.gitea.com/api/1.27/operations/repo-list-pull-requests
func pick(prs []pullRequest) pullRequest {
	picked := prs[0]
	pickedOpen := giteaState(picked) == api.PullRequestOpen
	for _, pr := range prs[1:] {
		open := giteaState(pr) == api.PullRequestOpen
		if (open && !pickedOpen) || (open == pickedOpen && pr.ID > picked.ID) {
			picked, pickedOpen = pr, open
		}
	}
	return picked
}

func (b *BaseClient) Ensure(ctx context.Context, options api.PullRequestOptions) (*api.PullRequest, bool, error) {
	pr, err := b.Find(ctx, options.Head)
	if err != nil {
		return nil, false, err
	}
	switch {
	case pr == nil, pr.State == api.PullRequestMerged:
		// Gitea refuses to change the state of a merged pull request — 412
		// "cannot change state of this pull request, it was already merged"
		// (go-gitea/gitea PR #17192; routers/api/v1/repo/pull.go, EditPullRequest) —
		// so this change needs a new one.
	case pr.State == api.PullRequestClosed:
		reopened, err := b.reopen(ctx, pr, options)
		switch {
		case err == nil:
			return reopened, false, nil
		case errors.Is(err, api.ErrPullRequestNotReopenable):
			// Falling through to create, rather than ending the run as a hard error did:
			// that left the change with its branch already pushed and no pull request.
			api.ReportAbandonedConversation(pr)
		default:
			return nil, false, err
		}
	default:
		return pr, false, nil
	}

	created, err := b.createPR(ctx, options)
	if err != nil {
		return nil, false, err
	}
	return &api.PullRequest{
		ID:   fmt.Sprintf("%d", created.ID),
		URL:  created.HTMLURL,
		Base: created.Base.Ref,
	}, true, nil
}

// reopen brings a closed pull request back, so the change keeps the pull request it
// already had and the conversation on it.
//
// State and base travel in the same request on purpose, and in that order: Gitea's
// edit handler applies a state change before a base change within one call
// (routers/api/v1/repo/pull.go, EditPullRequest: the `form.State` block precedes the
// `form.Base` block), and ChangeTargetBranch refuses to move the base of a pull
// request whose issue is still closed (services/pull/pull.go, ChangeTargetBranch:
// `if pr.Issue.IsClosed { return ErrIssueIsClosed{...} }`). Reopening has to happen
// before the base moves, which is the opposite order from GitHub — whose stack
// feature closes a pull request the moment its head is reachable from a stale base,
// so GitHub needs the base moved before it is safe to reopen.
func (b *BaseClient) reopen(ctx context.Context, pr *api.PullRequest, options api.PullRequestOptions) (*api.PullRequest, error) {
	body := map[string]interface{}{"state": "open"}
	// Only when it has actually moved. Gitea skips a base equal to the one the pull
	// request already has (routers/api/v1/repo/pull.go: `form.Base != pr.BaseBranch`),
	// so sending it changes nothing — and not sending it keeps the common case, a
	// review somebody closed by hand, down to the one field it needs.
	if pr.Base != options.Base {
		body["base"] = options.Base
	}

	reqURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%s", b.APIBase, b.Owner, b.Repository, pr.ID)
	resp, err := b.doJSON(ctx, http.MethodPatch, reqURL, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// A refusal is answered with ErrPullRequestNotReopenable rather than a hard error:
	// that is pkg/api's contract for a closed pull request which cannot be brought
	// back, and the caller then opens a new one instead of ending the review with the
	// change's branch pushed and nothing on it. Gitea refuses for reasons maiao cannot
	// fix from here — a merge it recorded (412), a base branch since deleted, a branch
	// protection rule — so a retry would fail the same way.
	//
	// Residual: the reopen and the base move travel in one PATCH and Gitea's
	// EditPullRequest is not transactional (routers/api/v1/repo/pull.go applies state
	// before base), so a refusal of the base half can leave the pull request reopened
	// on its old base beside the new one the caller then creates.
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: %s: %s %s", api.ErrPullRequestNotReopenable, pr.URL, resp.Status, string(respBody))
	}

	var result pullRequest
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &api.PullRequest{
		ID:    fmt.Sprintf("%d", result.ID),
		URL:   result.HTMLURL,
		Base:  result.Base.Ref,
		State: giteaState(result),
	}, nil
}

func (b *BaseClient) Update(ctx context.Context, pr *api.PullRequest, options api.PullRequestOptions) (*api.PullRequest, error) {
	title := options.Title
	if options.WIP && !strings.HasPrefix(title, "WIP: ") {
		title = "WIP: " + title
	}
	if options.Ready && strings.HasPrefix(title, "WIP: ") {
		title = strings.TrimPrefix(title, "WIP: ")
	}

	body := map[string]interface{}{
		"title": title,
		"body":  options.Body,
		"base":  options.Base,
	}

	reqURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%s", b.APIBase, b.Owner, b.Repository, pr.ID)
	resp, err := b.doJSON(ctx, http.MethodPatch, reqURL, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to update pull request: %s %s", resp.Status, string(respBody))
	}

	var result pullRequest
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &api.PullRequest{
		ID:   fmt.Sprintf("%d", result.ID),
		URL:  result.HTMLURL,
		Base: result.Base.Ref,
	}, nil
}

func (b *BaseClient) DefaultBranch(ctx context.Context) string {
	reqURL := fmt.Sprintf("%s/repos/%s/%s", b.APIBase, b.Owner, b.Repository)
	resp, err := b.doRequest(ctx, http.MethodGet, reqURL)
	if err != nil {
		log.ForContext(ctx).WithError(err).Error("failed to get repository info")
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var r repository
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return ""
	}
	return r.DefaultBranch
}

func (b *BaseClient) LinkedTopicIssues(topicSearchString string) string {
	values := url.Values{}
	values.Add("q", topicSearchString)
	values.Add("type", "pulls")
	values.Add("state", "open")
	return fmt.Sprintf("https://%s/%s/%s/pulls?%s", b.Host, b.Owner, b.Repository, values.Encode())
}

func (b *BaseClient) StackManager() api.StackManager {
	return nil
}

func (b *BaseClient) BodyFormatter() api.BodyFormatter {
	return api.HTMLBodyFormatter{}
}

// listPageSize is the largest page Gitea will serve. Every paginated response is
// capped by the [api] section's MAX_RESPONSE_ITEMS, which defaults to 50, and a page
// size that is not asked for is DEFAULT_PAGING_NUM, only 30 — which is what made the
// unpaginated listing this replaces look like it worked.
// https://github.com/go-gitea/gitea/blob/main/modules/setting/api.go
// (`MaxResponseItems: 50`, `DefaultPagingNum: 30`). An instance configured lower
// clamps the limit silently, which the walk below copes with by reading how many rows
// it was actually given rather than trusting the limit it asked for.
const listPageSize = 50

// listMaxPages bounds the walk at listMaxPages*listPageSize pull requests of history.
// Some ceiling is needed because Gitea's list endpoint has no head-branch filter — it
// filters by base_branch only — so the only way to learn that a head has no pull
// request is to read the repository's whole history, and nothing else stops the walk
// if pages keep coming. Reaching the ceiling reports what was found, which for a head
// buried deeper than that is nothing: the behaviour this had before, no worse.
// https://docs.gitea.com/api/1.27/operations/repo-list-pull-requests
const listMaxPages = 20

// listPRs returns every pull request Gitea has on the head branch.
//
// It pages because the matching happens here and not on the forge: the list endpoint
// takes no head filter, so with state=all the candidate set is the repository's whole
// pull request history, where with state=open it was bounded by the open pull
// requests. A busy repository answered a lookup for a head whose pull request was not
// on the single page the API serves with nothing, and the review opened a duplicate —
// or died on `409 pull request already exists`.
func (b *BaseClient) listPRs(ctx context.Context, head string) ([]pullRequest, error) {
	var matching []pullRequest
	// How many rows a page actually carries, which is the instance's own cap rather
	// than the limit asked for. A page shorter than the ones before it is the last.
	served := 0
	for page := 1; page <= listMaxPages; page++ {
		rows, err := b.listPRPage(ctx, page)
		if err != nil {
			return nil, err
		}
		for _, pr := range rows {
			if pr.Head.Ref == head {
				matching = append(matching, pr)
			}
		}
		// An open pull request is the live review, and pick prefers an open one over
		// anything closed or merged however new, so no older page can change the answer.
		// What stopping here gives up is the ordering between two open pull requests on
		// one head that fell on different pages — a case only a broken run produces, and
		// not worth walking a busy repository's whole history on every review to settle.
		if containsOpen(matching) {
			return matching, nil
		}
		if len(rows) == 0 || len(rows) < served {
			break
		}
		if len(rows) > served {
			served = len(rows)
		}
	}
	return matching, nil
}

func containsOpen(prs []pullRequest) bool {
	for _, pr := range prs {
		if giteaState(pr) == api.PullRequestOpen {
			return true
		}
	}
	return false
}

func (b *BaseClient) listPRPage(ctx context.Context, page int) ([]pullRequest, error) {
	params := url.Values{}
	// state=all: the API defaults to state=open, and a review that cannot see a
	// closed pull request opens a second one for the same change and leaves the
	// conversation on the first.
	// https://docs.gitea.com/api/1.27/operations/repo-list-pull-requests
	params.Add("state", "all")
	params.Add("limit", strconv.Itoa(listPageSize))
	params.Add("page", strconv.Itoa(page))
	reqURL := fmt.Sprintf("%s/repos/%s/%s/pulls?%s", b.APIBase, b.Owner, b.Repository, params.Encode())

	resp, err := b.doRequest(ctx, http.MethodGet, reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list pull requests: %s %s", resp.Status, string(respBody))
	}

	var prs []pullRequest
	if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
		return nil, err
	}
	return prs, nil
}

func (b *BaseClient) createPR(ctx context.Context, options api.PullRequestOptions) (*pullRequest, error) {
	title := options.Title
	if options.WIP {
		title = "WIP: " + title
	}

	body := map[string]interface{}{
		"head":  options.Head,
		"base":  options.Base,
		"title": title,
		"body":  options.Body,
	}

	reqURL := fmt.Sprintf("%s/repos/%s/%s/pulls", b.APIBase, b.Owner, b.Repository)
	resp, err := b.doJSON(ctx, http.MethodPost, reqURL, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to create pull request: %s %s", resp.Status, string(respBody))
	}

	var pr pullRequest
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

func (b *BaseClient) doJSON(ctx context.Context, method, reqURL string, body interface{}) (*http.Response, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return b.HTTPClient.Do(req)
}

func (b *BaseClient) doRequest(ctx context.Context, method, reqURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return nil, err
	}
	return b.HTTPClient.Do(req)
}
