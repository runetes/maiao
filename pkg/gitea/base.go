package gitea

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/adevinta/maiao/pkg/api"
	"github.com/adevinta/maiao/pkg/log"
)

type BaseClient struct {
	Host       string
	Owner      string
	Repository string
	HTTPClient *http.Client
	APIBase    string
}

type pullRequest struct {
	ID     int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Title  string `json:"title"`
	State  string `json:"state"`
}

type repository struct {
	DefaultBranch string `json:"default_branch"`
}

func (b *BaseClient) Ensure(ctx context.Context, options api.PullRequestOptions) (*api.PullRequest, bool, error) {
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
			URL: pr.HTMLURL,
		}, true, nil
	case 1:
		return &api.PullRequest{
			ID:  fmt.Sprintf("%d", prs[0].ID),
			URL: prs[0].HTMLURL,
		}, false, nil
	default:
		return nil, false, fmt.Errorf("too many matching pull requests (%d)", len(prs))
	}
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
		ID:  fmt.Sprintf("%d", result.ID),
		URL: result.HTMLURL,
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

func (b *BaseClient) listPRs(ctx context.Context, head string) ([]pullRequest, error) {
	params := url.Values{}
	params.Add("state", "open")
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

	var allPRs []struct {
		pullRequest
		Head struct {
			Ref string `json:"ref"`
		} `json:"head"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&allPRs); err != nil {
		return nil, err
	}

	var matching []pullRequest
	for _, pr := range allPRs {
		if pr.Head.Ref == head {
			matching = append(matching, pr.pullRequest)
		}
	}
	return matching, nil
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
