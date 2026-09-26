package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/runetes/maiao/pkg/api"
	"github.com/runetes/maiao/pkg/credentials"
	"github.com/runetes/maiao/pkg/log"
)

type GitLab struct {
	Host       string
	ProjectID  string
	Owner      string
	Repository string
	HTTPClient *http.Client
	apiBase    string
}

type mergeRequest struct {
	IID    int    `json:"iid"`
	WebURL string `json:"web_url"`
	Title  string `json:"title"`
	Draft  bool   `json:"draft"`
	// State is one of opened/closed/locked/merged — unlike GitHub or Gitea, GitLab
	// tells a merge from a plain close through this field alone, with no separate
	// merged flag to cross-check. https://docs.gitlab.com/api/merge_requests/
	State    string `json:"state"`
	SHA      string `json:"sha"`
	SourceBr string `json:"source_branch"`
	TargetBr string `json:"target_branch"`
}

// gitlabState maps what GitLab reports onto the states a review can act on.
// "locked" is a short-lived transitional state a merge request passes through while
// still open, so it is treated as open like an unrecognised value.
// https://docs.gitlab.com/api/merge_requests/
func gitlabState(mr mergeRequest) api.PullRequestState {
	switch mr.State {
	case "merged":
		return api.PullRequestMerged
	case "closed":
		return api.PullRequestClosed
	default:
		return api.PullRequestOpen
	}
}

type project struct {
	DefaultBranch string `json:"default_branch"`
}

func NewGitLabUpserter(ctx context.Context, endpoint *transport.Endpoint) (*GitLab, error) {
	orgRepo := strings.Split(strings.Trim(endpoint.Path, "/"), "/")
	if len(orgRepo) < 2 {
		return nil, fmt.Errorf("invalid repository path: %s", endpoint.Path)
	}

	owner := orgRepo[0]
	repo := strings.TrimSuffix(orgRepo[len(orgRepo)-1], ".git")
	projectPath := strings.TrimSuffix(strings.TrimPrefix(endpoint.Path, "/"), ".git")

	credGetter := credentials.CredentialGetterForProvider("gitlab")
	cred, err := credGetter.CredentialForHost(endpoint.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials for %s: %w", endpoint.Host, err)
	}

	apiBase := fmt.Sprintf("https://%s/api/v4", endpoint.Host)

	client := &http.Client{
		Transport: &tokenTransport{
			token:    cred.Password,
			delegate: http.DefaultTransport,
		},
	}

	return &GitLab{
		Host:       endpoint.Host,
		ProjectID:  url.PathEscape(projectPath),
		Owner:      owner,
		Repository: repo,
		HTTPClient: client,
		apiBase:    apiBase,
	}, nil
}

type tokenTransport struct {
	token    string
	delegate http.RoundTripper
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("PRIVATE-TOKEN", t.token)
	return t.delegate.RoundTrip(req)
}

// Find looks up the merge request for head, whatever state GitLab reports it in. It
// owns the list-and-pick switch so Ensure cannot drift from what a plain lookup
// reports.
//
// Listing state=all rather than the open-only default is what lets a review reuse a
// merge request an earlier run left closed instead of opening a second one beside it.
// https://docs.gitlab.com/api/merge_requests/
func (g *GitLab) Find(ctx context.Context, head string) (*api.PullRequest, error) {
	mrs, err := g.listMRs(ctx, head)
	if err != nil {
		return nil, err
	}
	if len(mrs) == 0 {
		return nil, nil
	}
	picked := pick(mrs)
	return &api.PullRequest{
		ID:    fmt.Sprintf("%d", picked.IID),
		URL:   picked.WebURL,
		Base:  picked.TargetBr,
		State: gitlabState(picked),
	}, nil
}

// pick chooses which of several merge requests on one source branch is the change's.
//
// state=all can return more than one where the open-only listing returned at most one;
// the extras are the wreckage of an earlier run rather than an ambiguity to refuse.
//
// An open one wins over any closed or merged one, however much newer that one is,
// because it is the live review. Taking the highest internal ID instead reported a
// source branch whose open merge request carried the conversation as Merged, which
// makes the parking pass skip it, so the push leaves its stale base containing its
// source branch and GitLab closes it as merged for good. Among equals the highest
// internal ID wins, IIDs only growing, which keeps the answer independent of the order
// GitLab lists in. https://docs.gitlab.com/api/merge_requests/
func pick(mrs []mergeRequest) mergeRequest {
	picked := mrs[0]
	pickedOpen := gitlabState(picked) == api.PullRequestOpen
	for _, mr := range mrs[1:] {
		open := gitlabState(mr) == api.PullRequestOpen
		if (open && !pickedOpen) || (open == pickedOpen && mr.IID > picked.IID) {
			picked, pickedOpen = mr, open
		}
	}
	return picked
}

func (g *GitLab) Ensure(ctx context.Context, options api.PullRequestOptions) (*api.PullRequest, bool, error) {
	pr, err := g.Find(ctx, options.Head)
	if err != nil {
		return nil, false, err
	}
	switch {
	case pr == nil, pr.State == api.PullRequestMerged:
		// GitLab's own state machine has no transition out of "merged" — `event
		// :reopen { transition closed: :opened }` is the only path back to opened,
		// defined nowhere for merged — so this change needs a new merge request.
		// app/models/merge_request.rb
	case pr.State == api.PullRequestClosed:
		reopened, err := g.reopen(ctx, pr, options)
		switch {
		case err == nil:
			return reopened, false, nil
		case errors.Is(err, api.ErrPullRequestNotReopenable):
			// Falling through to create, rather than ending the run as a hard error did:
			// that left the change with its branch already pushed and no merge request.
			api.ReportAbandonedConversation(pr)
		default:
			return nil, false, err
		}
	default:
		return pr, false, nil
	}

	mr, err := g.createMR(ctx, options)
	if err != nil {
		return nil, false, err
	}
	return &api.PullRequest{
		ID:   fmt.Sprintf("%d", mr.IID),
		URL:  mr.WebURL,
		Base: mr.TargetBr,
	}, true, nil
}

// reopen brings a closed merge request back, so the change keeps the merge request it
// already had and the conversation on it.
//
// Two requests, reopen strictly before the base moves: GitLab's update service reads
// merge_request.closed_or_merged_without_fork? — the state before this request's own
// changes are applied — and drops target_branch from the params entirely when it is
// true (app/services/merge_requests/update_service.rb, general_fallback). Sending
// state_event and target_branch together would therefore reopen the merge request but
// silently keep the stale base. This is the opposite order from GitHub, whose stack
// feature closes a pull request the moment its head is reachable from a stale base,
// so GitHub needs the base moved before it is safe to reopen.
func (g *GitLab) reopen(ctx context.Context, pr *api.PullRequest, options api.PullRequestOptions) (*api.PullRequest, error) {
	reqURL := fmt.Sprintf("%s/projects/%s/merge_requests/%s", g.apiBase, g.ProjectID, pr.ID)

	resp, err := g.doJSON(ctx, http.MethodPut, reqURL, map[string]interface{}{
		"state_event": "reopen",
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// A refusal is answered with ErrPullRequestNotReopenable rather than a hard error:
	// that is pkg/api's contract for a closed merge request which cannot be brought
	// back, and the caller then opens a new one instead of ending the review with the
	// change's branch pushed and nothing on it. GitLab refuses for reasons maiao cannot
	// fix from here — a state machine with no transition out of merged, a source branch
	// since deleted, an approval rule — so a retry would fail the same way.
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: %s: %s %s", api.ErrPullRequestNotReopenable, pr.URL, resp.Status, string(respBody))
	}
	// Most closed merge requests are already on the right base — nothing was
	// reordered, somebody closed the review — and the second request is then a write
	// that can only go wrong.
	if pr.Base == options.Base {
		var mr mergeRequest
		if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
			return nil, err
		}
		return &api.PullRequest{
			ID:    fmt.Sprintf("%d", mr.IID),
			URL:   mr.WebURL,
			Base:  mr.TargetBr,
			State: gitlabState(mr),
		}, nil
	}
	io.Copy(io.Discard, resp.Body)

	resp, err = g.doJSON(ctx, http.MethodPut, reqURL, map[string]interface{}{
		"target_branch": options.Base,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		// The reopen above has already landed, so leaving it here is a merge request open
		// on the base an earlier run gave it — the stale base the push is about to make
		// contain its own source branch, which is how GitLab closes a review as merged
		// with nothing merged. Closing it back restores what the caller is then told:
		// this one stays closed and a new merge request carries the change. Best effort,
		// because a failure here is not something failing louder would fix.
		g.closeMR(ctx, reqURL)
		return nil, fmt.Errorf("%w: %s: failed to move the base of the reopened merge request: %s %s", api.ErrPullRequestNotReopenable, pr.URL, resp.Status, string(respBody))
	}

	var mr mergeRequest
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err
	}
	return &api.PullRequest{
		ID:    fmt.Sprintf("%d", mr.IID),
		URL:   mr.WebURL,
		Base:  mr.TargetBr,
		State: gitlabState(mr),
	}, nil
}

func (g *GitLab) closeMR(ctx context.Context, reqURL string) {
	resp, err := g.doJSON(ctx, http.MethodPut, reqURL, map[string]interface{}{
		"state_event": "close",
	})
	if err != nil {
		log.ForContext(ctx).WithError(err).Warn("could not close the merge request again after its base refused to move")
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
}

func (g *GitLab) Update(ctx context.Context, pr *api.PullRequest, options api.PullRequestOptions) (*api.PullRequest, error) {
	title := options.Title
	if options.WIP && !strings.HasPrefix(title, "Draft: ") {
		title = "Draft: " + title
	}
	if options.Ready && strings.HasPrefix(title, "Draft: ") {
		title = strings.TrimPrefix(title, "Draft: ")
	}

	body := map[string]interface{}{
		"title":         title,
		"description":   options.Body,
		"target_branch": options.Base,
	}

	reqURL := fmt.Sprintf("%s/projects/%s/merge_requests/%s", g.apiBase, g.ProjectID, pr.ID)
	resp, err := g.doJSON(ctx, http.MethodPut, reqURL, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to update merge request: %s %s", resp.Status, string(respBody))
	}

	var mr mergeRequest
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err
	}

	return &api.PullRequest{
		ID:   fmt.Sprintf("%d", mr.IID),
		URL:  mr.WebURL,
		Base: mr.TargetBr,
	}, nil
}

func (g *GitLab) DefaultBranch(ctx context.Context) string {
	reqURL := fmt.Sprintf("%s/projects/%s", g.apiBase, g.ProjectID)
	resp, err := g.doRequest(ctx, http.MethodGet, reqURL)
	if err != nil {
		log.ForContext(ctx).WithError(err).Error("failed to get project info")
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var p project
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return ""
	}
	return p.DefaultBranch
}

func (g *GitLab) LinkedTopicIssues(topicSearchString string) string {
	values := url.Values{}
	values.Add("search", topicSearchString)
	values.Add("state", "opened")
	return fmt.Sprintf("https://%s/%s/%s/-/merge_requests?%s", g.Host, g.Owner, g.Repository, values.Encode())
}

func (g *GitLab) StackManager() api.StackManager {
	return nil
}

func (g *GitLab) BodyFormatter() api.BodyFormatter {
	return api.HTMLBodyFormatter{}
}

// listMRs needs no page walk, unlike the Gitea client: source_branch is a filter
// GitLab applies itself — "Returns merge requests with the given source branch"
// (https://docs.gitlab.com/api/merge_requests/) — so widening state from open to all
// widens the result from the open merge requests on this one branch to every merge
// request on it, a handful either way, rather than to the project's whole history.
func (g *GitLab) listMRs(ctx context.Context, sourceBranch string) ([]mergeRequest, error) {
	params := url.Values{}
	params.Add("source_branch", sourceBranch)
	// state=all, explicit rather than relied on as the default: a review that cannot
	// see a closed merge request opens a second one for the same change and leaves
	// the conversation on the first. https://docs.gitlab.com/api/merge_requests/
	params.Add("state", "all")
	reqURL := fmt.Sprintf("%s/projects/%s/merge_requests?%s", g.apiBase, g.ProjectID, params.Encode())

	resp, err := g.doRequest(ctx, http.MethodGet, reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list merge requests: %s %s", resp.Status, string(respBody))
	}

	var mrs []mergeRequest
	if err := json.NewDecoder(resp.Body).Decode(&mrs); err != nil {
		return nil, err
	}
	return mrs, nil
}

func (g *GitLab) createMR(ctx context.Context, options api.PullRequestOptions) (*mergeRequest, error) {
	title := options.Title
	if options.WIP {
		title = "Draft: " + title
	}

	body := map[string]interface{}{
		"source_branch": options.Head,
		"target_branch": options.Base,
		"title":         title,
		"description":   options.Body,
	}

	reqURL := fmt.Sprintf("%s/projects/%s/merge_requests", g.apiBase, g.ProjectID)
	resp, err := g.doJSON(ctx, http.MethodPost, reqURL, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to create merge request: %s %s", resp.Status, string(respBody))
	}

	var mr mergeRequest
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err
	}
	return &mr, nil
}

func (g *GitLab) doJSON(ctx context.Context, method, reqURL string, body interface{}) (*http.Response, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return g.HTTPClient.Do(req)
}

func (g *GitLab) doRequest(ctx context.Context, method, reqURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return nil, err
	}
	return g.HTTPClient.Do(req)
}
