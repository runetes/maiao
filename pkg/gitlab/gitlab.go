package gitlab

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

type GitLab struct {
	Host       string
	ProjectID  string
	Owner      string
	Repository string
	HTTPClient *http.Client
	apiBase    string
}

type mergeRequest struct {
	IID      int    `json:"iid"`
	WebURL   string `json:"web_url"`
	Title    string `json:"title"`
	Draft    bool   `json:"draft"`
	State    string `json:"state"`
	SHA      string `json:"sha"`
	SourceBr string `json:"source_branch"`
	TargetBr string `json:"target_branch"`
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

func (g *GitLab) Ensure(ctx context.Context, options api.PullRequestOptions) (*api.PullRequest, bool, error) {
	mrs, err := g.listMRs(ctx, options.Head)
	if err != nil {
		return nil, false, err
	}

	switch len(mrs) {
	case 0:
		mr, err := g.createMR(ctx, options)
		if err != nil {
			return nil, false, err
		}
		return &api.PullRequest{
			ID:  fmt.Sprintf("%d", mr.IID),
			URL: mr.WebURL,
		}, true, nil
	case 1:
		return &api.PullRequest{
			ID:  fmt.Sprintf("%d", mrs[0].IID),
			URL: mrs[0].WebURL,
		}, false, nil
	default:
		return nil, false, fmt.Errorf("too many matching merge requests (%d)", len(mrs))
	}
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
		ID:  fmt.Sprintf("%d", mr.IID),
		URL: mr.WebURL,
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

func (g *GitLab) listMRs(ctx context.Context, sourceBranch string) ([]mergeRequest, error) {
	params := url.Values{}
	params.Add("source_branch", sourceBranch)
	params.Add("state", "opened")
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
