package origin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/adevinta/maiao/pkg/api"
	"github.com/adevinta/maiao/pkg/credentials"
	"github.com/adevinta/maiao/pkg/log"
	"github.com/go-git/go-git/v5/plumbing/transport"
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
	Number string         `json:"number"`
	Title  string         `json:"title"`
	Body   string         `json:"body"`
	State  string         `json:"state"`
	Draft  bool           `json:"draft"`
	Head   pullRequestRef `json:"head"`
	Base   pullRequestRef `json:"base"`
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

func (o *Origin) Ensure(ctx context.Context, options api.PullRequestOptions) (*api.PullRequest, bool, error) {
	prs, err := o.listPRs(ctx, options.Head)
	if err != nil {
		return nil, false, err
	}

	switch len(prs) {
	case 0:
		pr, err := o.createPR(ctx, options)
		if err != nil {
			return nil, false, err
		}
		return &api.PullRequest{
			ID:  pr.Number,
			URL: o.prURL(pr.Number),
		}, true, nil
	case 1:
		return &api.PullRequest{
			ID:  prs[0].Number,
			URL: o.prURL(prs[0].Number),
		}, false, nil
	default:
		return nil, false, fmt.Errorf("too many matching pull requests (%d)", len(prs))
	}
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
		ID:  result.Number,
		URL: o.prURL(result.Number),
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

func (o *Origin) listPRs(ctx context.Context, head string) ([]pullRequest, error) {
	params := url.Values{}
	params.Add("head", head)
	params.Add("state", "open")
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
