package bitbucket

import (
	"context"
	"encoding/json"
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

type Bitbucket struct {
	Host       string
	Workspace  string
	RepoSlug   string
	HTTPClient *http.Client
	apiBase    string
}

type pullRequest struct {
	ID    int `json:"id"`
	Links struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
	Title string `json:"title"`
	// State is one of OPEN, MERGED, DECLINED, SUPERSEDED — Bitbucket's own OpenAPI
	// spec for this field (see bitbucketState below).
	State  string `json:"state"`
	Source struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
	} `json:"source"`
	Destination struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
	} `json:"destination"`
}

type pullRequestList struct {
	Values []pullRequest `json:"values"`
}

type repository struct {
	MainBranch struct {
		Name string `json:"name"`
	} `json:"mainbranch"`
}

// bitbucketState maps what Bitbucket reports onto the states a review can act on.
// The pull request "state" enum is exactly OPEN, MERGED, DECLINED, SUPERSEDED
// (developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/, the
// paginated_pullrequests schema). SUPERSEDED — a pull request Bitbucket auto-closes
// because another one landed the same change — is no more reusable than DECLINED, so
// both map to Closed; Ensure never reopens either kind (see the comment on Ensure).
//
// Anything outside that enum is taken to be open, which pkg/api.PullRequest documents
// as what every caller does with a state a provider does not say. Defaulting to Closed
// was the opposite, and Ensure's gate on exactly PullRequestOpen turned one state
// string Bitbucket grew later into an open pull request neither parked nor reused, with
// a duplicate opened beside it.
func bitbucketState(pr pullRequest) api.PullRequestState {
	switch pr.State {
	case "MERGED":
		return api.PullRequestMerged
	case "DECLINED", "SUPERSEDED":
		return api.PullRequestClosed
	default:
		return api.PullRequestOpen
	}
}

func NewBitbucketUpserter(ctx context.Context, endpoint *transport.Endpoint) (*Bitbucket, error) {
	orgRepo := strings.Split(strings.Trim(endpoint.Path, "/"), "/")
	if len(orgRepo) != 2 {
		return nil, fmt.Errorf("invalid repository path: %s (expected workspace/repo)", endpoint.Path)
	}

	workspace := orgRepo[0]
	repoSlug := strings.TrimSuffix(orgRepo[1], ".git")

	credGetter := credentials.CredentialGetterForProvider("bitbucket")
	cred, err := credGetter.CredentialForHost(endpoint.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials for %s: %w", endpoint.Host, err)
	}

	apiBase := "https://api.bitbucket.org/2.0"

	client := &http.Client{
		Transport: &basicAuthTransport{
			username: cred.Username,
			password: cred.Password,
			delegate: http.DefaultTransport,
		},
	}

	return &Bitbucket{
		Host:       endpoint.Host,
		Workspace:  workspace,
		RepoSlug:   repoSlug,
		HTTPClient: client,
		apiBase:    apiBase,
	}, nil
}

type basicAuthTransport struct {
	username string
	password string
	delegate http.RoundTripper
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(t.username, t.password)
	return t.delegate.RoundTrip(req)
}

// Find looks up the pull request for head, whatever state Bitbucket reports it in.
// It owns the list-and-pick switch so Ensure cannot drift from what a plain lookup
// reports.
//
// Listing every state rather than the open-only default is what lets a review notice
// a pull request an earlier run left declined, even though Bitbucket gives it no way
// to reuse one (see Ensure).
func (b *Bitbucket) Find(ctx context.Context, head string) (*api.PullRequest, error) {
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
		URL:   picked.Links.HTML.Href,
		Base:  picked.Destination.Branch.Name,
		State: bitbucketState(picked),
	}, nil
}

// pick chooses which of several pull requests on one source branch is the change's.
//
// Listing every state can return more than one where the open-only listing returned at
// most one; the extras are the wreckage of an earlier run rather than an ambiguity to
// refuse.
//
// An open one wins over any merged or declined one, however much newer that one is,
// because it is the live review. Taking the highest ID instead reported a branch whose
// open pull request carried the conversation as Merged, which makes the parking pass
// skip it, so the push leaves its stale base containing its own source branch and the
// forge closes it as merged for good. Among equals the highest ID wins, IDs only
// growing, which keeps the answer independent of the order Bitbucket lists in.
func pick(prs []pullRequest) pullRequest {
	picked := prs[0]
	pickedOpen := bitbucketState(picked) == api.PullRequestOpen
	for _, pr := range prs[1:] {
		open := bitbucketState(pr) == api.PullRequestOpen
		if (open && !pickedOpen) || (open == pickedOpen && pr.ID > picked.ID) {
			picked, pickedOpen = pr, open
		}
	}
	return picked
}

// Ensure never reopens a non-open pull request: Bitbucket Cloud's pull request API
// has no reopen endpoint (developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/,
// confirmed against BCLOUD-23807 "Ability to reopen a declined pull request", still
// open as a feature request, and renovatebot/renovate#14243 hitting the same gap in
// production), and its own spec says the PUT that could otherwise move the base
// "Only open pull requests can be mutated." So a closed or merged pull request is
// left as dead history and this change always gets a new one — there is nothing to
// reuse, only something to report (a review pass above already tells the user on
// stderr which conversation that leaves behind).
//
// The switch names the states that cannot be reused and reuses the pull request
// otherwise, which is the shape the other providers have. Gating on exactly
// PullRequestOpen was the other way round, so any state this did not recognise — and
// Bitbucket has grown its enum before — meant a live pull request was neither parked nor
// reused and a duplicate was opened beside it.
func (b *Bitbucket) Ensure(ctx context.Context, options api.PullRequestOptions) (*api.PullRequest, bool, error) {
	pr, err := b.Find(ctx, options.Head)
	if err != nil {
		return nil, false, err
	}
	switch {
	case pr == nil, pr.State == api.PullRequestMerged, pr.State == api.PullRequestClosed:
		// Nothing to reuse: a merged, declined, or superseded pull request is dead
		// history on a forge with no reopen.
	default:
		return pr, false, nil
	}

	created, err := b.createPR(ctx, options)
	if err != nil {
		return nil, false, err
	}
	return &api.PullRequest{
		ID:   fmt.Sprintf("%d", created.ID),
		URL:  created.Links.HTML.Href,
		Base: created.Destination.Branch.Name,
	}, true, nil
}

func (b *Bitbucket) Update(ctx context.Context, pr *api.PullRequest, options api.PullRequestOptions) (*api.PullRequest, error) {
	if options.WIP {
		log.ForContext(ctx).Warn("Bitbucket Cloud does not support draft pull requests")
	}

	body := map[string]interface{}{
		"title":       options.Title,
		"description": options.Body,
		"destination": map[string]interface{}{
			"branch": map[string]string{
				"name": options.Base,
			},
		},
	}

	reqURL := fmt.Sprintf("%s/repositories/%s/%s/pullrequests/%s", b.apiBase, b.Workspace, b.RepoSlug, pr.ID)
	resp, err := b.doJSON(ctx, http.MethodPut, reqURL, body)
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
		URL:  result.Links.HTML.Href,
		Base: result.Destination.Branch.Name,
	}, nil
}

func (b *Bitbucket) DefaultBranch(ctx context.Context) string {
	reqURL := fmt.Sprintf("%s/repositories/%s/%s", b.apiBase, b.Workspace, b.RepoSlug)
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
	return r.MainBranch.Name
}

func (b *Bitbucket) LinkedTopicIssues(topicSearchString string) string {
	values := url.Values{}
	values.Add("search_query", topicSearchString)
	return fmt.Sprintf("https://%s/%s/%s/pull-requests?%s", b.Host, b.Workspace, b.RepoSlug, values.Encode())
}

func (b *Bitbucket) StackManager() api.StackManager {
	return nil
}

func (b *Bitbucket) BodyFormatter() api.BodyFormatter {
	return MarkdownBodyFormatter{}
}

// listPRs needs no page walk, unlike the Gitea client: the source branch is filtered by
// Bitbucket itself, through the BBQL q clause below, so widening state from open to all
// widens the result from the open pull requests on this one branch to every pull request
// on it rather than to the repository's whole history. Filtering a nested field this way
// is what Atlassian's own answer to the question does
// (support.atlassian.com/bitbucket-cloud/kb/using-bitbucket-rest-api-to-get-list-of-merged-pull-requests-for-a-specific-source-and-destination-branches/).
//
// Only the first page of the paginated envelope is read. The default pagelen is
// **unverified** — developer.atlassian.com/cloud/bitbucket/rest/intro/ could not be
// retrieved — so a head carrying more pull requests than one page holds would have the
// oldest of them invisible here. That takes a head branch reused by many pull requests,
// which one change per head does not produce.
func (b *Bitbucket) listPRs(ctx context.Context, sourceBranch string) ([]pullRequest, error) {
	query := fmt.Sprintf(`source.branch.name = "%s"`, sourceBranch)
	params := url.Values{}
	params.Add("q", query)
	// state is a distinct, repeatable query parameter (not a BBQL q clause): "By
	// default only open pull requests are returned... To retrieve pull requests that
	// are in one of multiple states, repeat the state parameter for each individual
	// state." Repeating it for all four is what lets Find see a pull request an
	// earlier run left declined instead of only ever seeing open ones.
	// developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/
	for _, state := range []string{"OPEN", "MERGED", "DECLINED", "SUPERSEDED"} {
		params.Add("state", state)
	}
	reqURL := fmt.Sprintf("%s/repositories/%s/%s/pullrequests?%s", b.apiBase, b.Workspace, b.RepoSlug, params.Encode())

	resp, err := b.doRequest(ctx, http.MethodGet, reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list pull requests: %s %s", resp.Status, string(respBody))
	}

	var list pullRequestList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	return list.Values, nil
}

func (b *Bitbucket) createPR(ctx context.Context, options api.PullRequestOptions) (*pullRequest, error) {
	if options.WIP {
		log.ForContext(ctx).Warn("Bitbucket Cloud does not support draft pull requests")
	}

	body := map[string]interface{}{
		"title":       options.Title,
		"description": options.Body,
		"source": map[string]interface{}{
			"branch": map[string]string{
				"name": options.Head,
			},
		},
		"destination": map[string]interface{}{
			"branch": map[string]string{
				"name": options.Base,
			},
		},
	}

	reqURL := fmt.Sprintf("%s/repositories/%s/%s/pullrequests", b.apiBase, b.Workspace, b.RepoSlug)
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

func (b *Bitbucket) doJSON(ctx context.Context, method, reqURL string, body interface{}) (*http.Response, error) {
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

func (b *Bitbucket) doRequest(ctx context.Context, method, reqURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return nil, err
	}
	return b.HTTPClient.Do(req)
}
