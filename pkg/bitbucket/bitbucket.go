package bitbucket

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
	Title  string `json:"title"`
	State  string `json:"state"`
	Source struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
	} `json:"source"`
}

type pullRequestList struct {
	Values []pullRequest `json:"values"`
}

type repository struct {
	MainBranch struct {
		Name string `json:"name"`
	} `json:"mainbranch"`
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

func (b *Bitbucket) Ensure(ctx context.Context, options api.PullRequestOptions) (*api.PullRequest, bool, error) {
	prs, err := b.listPRs(ctx, options.Head)
	if err != nil {
		return nil, false, err
	}

	switch len(prs) {
	case 0:
		pr, err := b.createPR(ctx, options)
		if err != nil {
			return nil, false, err
		}
		return &api.PullRequest{
			ID:  fmt.Sprintf("%d", pr.ID),
			URL: pr.Links.HTML.Href,
		}, true, nil
	case 1:
		return &api.PullRequest{
			ID:  fmt.Sprintf("%d", prs[0].ID),
			URL: prs[0].Links.HTML.Href,
		}, false, nil
	default:
		return nil, false, fmt.Errorf("too many matching pull requests (%d)", len(prs))
	}
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
		ID:  fmt.Sprintf("%d", result.ID),
		URL: result.Links.HTML.Href,
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

func (b *Bitbucket) listPRs(ctx context.Context, sourceBranch string) ([]pullRequest, error) {
	query := fmt.Sprintf(`source.branch.name = "%s" AND state = "OPEN"`, sourceBranch)
	params := url.Values{}
	params.Add("q", query)
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
