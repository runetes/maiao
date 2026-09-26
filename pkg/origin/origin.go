package origin

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

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/runetes/maiao/pkg/api"
	"github.com/runetes/maiao/pkg/credentials"
	"github.com/runetes/maiao/pkg/log"
)

type Origin struct {
	Owner      string
	Repo       string
	HTTPClient *http.Client
	apiBase    string
	host       string
}

type pullRequestRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type pullRequest struct {
	Number string `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	// State is only ever "open" or "closed" — Origin reports a merge through the
	// separate Merged flag below, not a third state value: '"open" or "closed". A draft
	// is "open"; merged and closed pull requests are both "closed".'
	// https://cursor.com/docs/api/origin/openapi.yaml, the PullRequest schema
	State  string         `json:"state"`
	Draft  bool           `json:"draft"`
	Merged bool           `json:"merged"`
	Head   pullRequestRef `json:"head"`
	Base   pullRequestRef `json:"base"`
}

// originState maps what Origin reports onto the states a review can act on.
// https://cursor.com/docs/api/origin/openapi.yaml, the PullRequest schema
//
// A state Origin does not report, or one this does not recognise, is taken to be open:
// pkg/api.PullRequest documents an unsaid state as the one every caller treats as open,
// and the costs are asymmetric — reading an open pull request as closed parks or
// replaces a live review, while reading a dead one as open at worst reuses something
// the forge then refuses to move.
func originState(pr pullRequest) api.PullRequestState {
	if pr.Merged {
		return api.PullRequestMerged
	}
	if pr.State == "closed" {
		return api.PullRequestClosed
	}
	return api.PullRequestOpen
}

type listPullRequestsResponse struct {
	PullRequests  []pullRequest `json:"pullRequests"`
	NextPageToken string        `json:"nextPageToken"`
}

type repository struct {
	DefaultBranch string `json:"defaultBranch"`
}

func NewOriginUpserter(ctx context.Context, endpoint *transport.Endpoint) (*Origin, error) {
	orgRepo := strings.Split(strings.Trim(endpoint.Path, "/"), "/")
	if len(orgRepo) != 2 {
		return nil, fmt.Errorf("invalid repository path: %s (expected owner/repo)", endpoint.Path)
	}

	owner := orgRepo[0]
	repo := strings.TrimSuffix(orgRepo[1], ".git")

	credGetter := credentials.CredentialGetterForProvider("origin")
	cred, err := credGetter.CredentialForHost(endpoint.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials for %s: %w", endpoint.Host, err)
	}

	client := &http.Client{
		Transport: &bearerTransport{
			token:    cred.Password,
			delegate: http.DefaultTransport,
		},
	}

	return &Origin{
		Owner:      owner,
		Repo:       repo,
		HTTPClient: client,
		apiBase:    "https://api.cursor.com/v1/origin",
		host:       endpoint.Host,
	}, nil
}

type bearerTransport struct {
	token    string
	delegate http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.delegate.RoundTrip(req)
}

// Find looks up the pull request for head, whatever state Origin reports it in. It
// owns the list-and-pick switch so Ensure cannot drift from what a plain lookup
// reports.
//
// Listing state=all rather than the open-only default is what lets a review reuse a
// pull request an earlier run left closed instead of opening a second one beside it.
// https://cursor.com/docs/api/origin
func (o *Origin) Find(ctx context.Context, head string) (*api.PullRequest, error) {
	prs, err := o.listPRs(ctx, head)
	if err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	picked := pick(prs)
	return &api.PullRequest{
		ID:    picked.Number,
		URL:   o.prURL(picked.Number),
		Base:  picked.Base.Ref,
		State: originState(picked),
	}, nil
}

// pick chooses which of several pull requests on one head branch is the change's.
//
// state=all can return more than one where the open-only default returned at most one;
// the extras are the wreckage of an earlier run rather than an ambiguity to refuse.
//
// An open one wins over any closed or merged one, however much newer that one is,
// because it is the live review. Taking the highest number instead reported a head
// whose open pull request carried the conversation as Merged, which makes the parking
// pass skip it, so the push leaves its stale base containing its head and the forge
// closes it as merged — irreversibly. Among equals the highest number wins, numbers
// only growing; Origin does list most recently created first by default, but the answer
// does not depend on that.
// https://cursor.com/docs/api/origin/openapi.yaml, OriginService_ListPullRequests
func pick(prs []pullRequest) pullRequest {
	picked := prs[0]
	pickedOpen := originState(picked) == api.PullRequestOpen
	for _, pr := range prs[1:] {
		open := originState(pr) == api.PullRequestOpen
		if (open && !pickedOpen) || (open == pickedOpen && numericallyAfter(pr.Number, picked.Number)) {
			picked, pickedOpen = pr, open
		}
	}
	return picked
}

func (o *Origin) Ensure(ctx context.Context, options api.PullRequestOptions) (*api.PullRequest, bool, error) {
	pr, err := o.Find(ctx, options.Head)
	if err != nil {
		return nil, false, err
	}
	switch {
	case pr == nil, pr.State == api.PullRequestMerged:
		// A merged pull request cannot be brought back: the state field of the update
		// endpoint takes `"open"` or `"closed"` and says "Merged is not writable — use
		// MergePullRequest" (https://cursor.com/docs/api/origin/openapi.yaml,
		// OriginService_UpdatePullRequest). So this change needs a new one, as it does on
		// GitHub (422) and Gitea (412).
	case pr.State == api.PullRequestClosed:
		reopened, err := o.reopen(ctx, pr, options)
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

	created, err := o.createPR(ctx, options)
	if err != nil {
		return nil, false, err
	}
	return &api.PullRequest{
		ID:   created.Number,
		URL:  o.prURL(created.Number),
		Base: created.Base.Ref,
	}, true, nil
}

// reopen brings a closed pull request back, so the change keeps the pull request it
// already had and the conversation on it.
//
// State and base travel in the same request, in which Origin applies them in the order
// this needs: "Present fields are applied in order: metadata, then
// reopen/draft/ready-for-review, then base, then stack parent, then close... reopen runs
// before base so a closed pull can be retargeted."
// https://cursor.com/docs/api/origin/openapi.yaml, OriginService_UpdatePullRequest
//
// The same paragraph names the residual: "If a later step fails, earlier steps may
// already have been committed." A refusal of the base half therefore leaves the pull
// request reopened on its old base, beside the new one the caller then creates.
// Untested against a live Origin repository, which this has never run against.
func (o *Origin) reopen(ctx context.Context, pr *api.PullRequest, options api.PullRequestOptions) (*api.PullRequest, error) {
	body := map[string]interface{}{"state": "open"}
	// Only when it has actually moved, which is what keeps the unverified ordering
	// above off the common path: a review closed by hand is already on the right base,
	// so the request asks for nothing but the reopen and no order can be got wrong.
	if pr.Base != options.Base {
		body["base"] = options.Base
	}

	reqURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%s", o.apiBase, o.Owner, o.Repo, pr.ID)
	resp, err := o.doJSON(ctx, http.MethodPatch, reqURL, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// A refusal is answered with ErrPullRequestNotReopenable rather than a hard error:
	// that is pkg/api's contract for a closed pull request which cannot be brought back,
	// and the caller then opens a new one instead of ending the review with the change's
	// branch pushed and nothing on it. A retry would fail the same way — the reasons a
	// forge refuses (a recorded merge, a base branch since deleted, a protection rule)
	// are not ones maiao can fix from here.
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: %s: %s %s", api.ErrPullRequestNotReopenable, pr.URL, resp.Status, string(respBody))
	}

	var result pullRequest
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &api.PullRequest{
		ID:    result.Number,
		URL:   o.prURL(result.Number),
		Base:  result.Base.Ref,
		State: originState(result),
	}, nil
}

// numericallyAfter reports whether a is a later pull request number than b. Origin's
// pull request numbers are decimal strings, so a lexical comparison would rank "9"
// after "10"; both parse as plain integers, and a malformed one sorts before
// everything rather than panicking.
func numericallyAfter(a, b string) bool {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	if aErr != nil {
		return false
	}
	if bErr != nil {
		return true
	}
	return an > bn
}

func (o *Origin) Update(ctx context.Context, pr *api.PullRequest, options api.PullRequestOptions) (*api.PullRequest, error) {
	body := map[string]interface{}{
		"title": options.Title,
		"body":  options.Body,
		"base":  options.Base,
	}
	if options.WIP {
		body["draft"] = true
	} else if options.Ready {
		body["draft"] = false
	}

	reqURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%s", o.apiBase, o.Owner, o.Repo, pr.ID)
	resp, err := o.doJSON(ctx, http.MethodPatch, reqURL, body)
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
		ID:   result.Number,
		URL:  o.prURL(result.Number),
		Base: result.Base.Ref,
	}, nil
}

func (o *Origin) DefaultBranch(ctx context.Context) string {
	reqURL := fmt.Sprintf("%s/repos/%s/%s", o.apiBase, o.Owner, o.Repo)
	resp, err := o.doRequest(ctx, http.MethodGet, reqURL)
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

func (o *Origin) LinkedTopicIssues(topicSearchString string) string {
	values := url.Values{}
	values.Add("q", topicSearchString)
	return fmt.Sprintf("https://%s/%s/%s/pulls?%s", o.host, o.Owner, o.Repo, values.Encode())
}

func (o *Origin) prURL(number string) string {
	return fmt.Sprintf("https://%s/%s/%s/pull/%s", o.host, o.Owner, o.Repo, number)
}

func (o *Origin) StackManager() api.StackManager {
	return nil
}

func (o *Origin) BodyFormatter() api.BodyFormatter {
	return api.HTMLBodyFormatter{}
}

// listPRs needs no page walk, unlike the Gitea client: head is a filter Origin applies
// itself — "Optional exact branch (head-ref) filter" — so widening state from its
// open-only default to all widens the result from the open pull requests on this one
// branch to every pull request on it, well inside the 30 rows a page carries by default,
// rather than to the repository's whole history. Which is why nextPageToken in the
// envelope above is decoded and not followed.
// https://cursor.com/docs/api/origin/openapi.yaml, OriginService_ListPullRequests
func (o *Origin) listPRs(ctx context.Context, head string) ([]pullRequest, error) {
	params := url.Values{}
	params.Add("head", head)
	// state=all, because a review that cannot see a closed pull request opens a second
	// one for the same change and leaves the conversation on the first. The filter takes
	// '"open" (the default), "closed", "merged", or "all"... Any other value is rejected
	// with INVALID_ARGUMENT', so a misspelling here would fail loudly rather than
	// quietly list the open ones.
	params.Add("state", "all")
	reqURL := fmt.Sprintf("%s/repos/%s/%s/pulls?%s", o.apiBase, o.Owner, o.Repo, params.Encode())

	resp, err := o.doRequest(ctx, http.MethodGet, reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list pull requests: %s %s", resp.Status, string(respBody))
	}

	var list listPullRequestsResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	return list.PullRequests, nil
}

func (o *Origin) createPR(ctx context.Context, options api.PullRequestOptions) (*pullRequest, error) {
	body := map[string]interface{}{
		"title": options.Title,
		"body":  options.Body,
		"head":  options.Head,
		"base":  options.Base,
	}
	if options.WIP {
		body["draft"] = true
	}
	if options.ParentPullNumber != "" {
		body["parentPullNumber"] = options.ParentPullNumber
	}

	reqURL := fmt.Sprintf("%s/repos/%s/%s/pulls", o.apiBase, o.Owner, o.Repo)
	resp, err := o.doJSON(ctx, http.MethodPost, reqURL, body)
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

func (o *Origin) doJSON(ctx context.Context, method, reqURL string, body interface{}) (*http.Response, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return o.HTTPClient.Do(req)
}

func (o *Origin) doRequest(ctx context.Context, method, reqURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return nil, err
	}
	return o.HTTPClient.Do(req)
}
