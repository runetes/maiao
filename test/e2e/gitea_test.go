//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// giteaForge talks to a Gitea (or Forgejo) instance with its own client.
//
// Worth having beyond GitHub: maiao's own repository is reviewed through Gitea, and
// the two forges disagree about the order a reopen and a base change may be asked for
// in, so the provider code is genuinely different and needs its own evidence.
type giteaForge struct {
	apiBase string // https://host/api/v1
	host    string
	slug    string // owner/repo
	owner   string
	repo    string
	user    string
	token   string
}

func newGiteaForge() (forge, string) {
	raw := env("MAIAO_E2E_GITEA_URL")
	slug := env("MAIAO_E2E_GITEA_REPO")
	user := env("MAIAO_E2E_GITEA_USER")
	token := env("MAIAO_E2E_GITEA_TOKEN")
	switch {
	case raw == "":
		return nil, "MAIAO_E2E_GITEA_URL=https://your.gitea for a throwaway Gitea repository"
	case slug == "":
		return nil, "MAIAO_E2E_GITEA_REPO=owner/repo for a throwaway Gitea repository"
	case user == "":
		// Required, and not defaulted: Gitea authenticates a token as the password of a
		// real account, and answers 401 to a made-up username.
		return nil, "MAIAO_E2E_GITEA_USER, the account MAIAO_E2E_GITEA_TOKEN belongs to"
	case token == "":
		return nil, "MAIAO_E2E_GITEA_TOKEN, an access token or password for that account"
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Sprintf("MAIAO_E2E_GITEA_URL=%q is not a URL", raw)
	}
	owner, repo, ok := strings.Cut(slug, "/")
	if !ok {
		return nil, fmt.Sprintf("MAIAO_E2E_GITEA_REPO=%q is not owner/repo", slug)
	}
	return &giteaForge{
		apiBase: strings.TrimSuffix(raw, "/") + "/api/v1",
		host:    parsed.Host,
		slug:    slug, owner: owner, repo: repo, user: user, token: token,
	}, ""
}

func (g *giteaForge) Name() string      { return "gitea" }
func (g *giteaForge) RemoteURL() string { return "https://" + g.host + "/" + g.slug + ".git" }
func (g *giteaForge) CloneURL() string {
	return "https://" + url.UserPassword(g.user, g.token).String() + "@" + g.host + "/" + g.slug + ".git"
}

func (g *giteaForge) Netrc() []string {
	return []string{"machine " + g.host + " login " + g.user + " password " + g.token}
}

func (g *giteaForge) Secrets() []string { return []string{g.token} }

// GitConfig names the provider, because a self-hosted Gitea is not a host maiao can
// recognise from its name.
func (g *giteaForge) GitConfig() [][2]string {
	return [][2]string{{"maiao.provider", "gitea"}}
}

func (g *giteaForge) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.apiBase+path, reader)
	if err != nil {
		return err
	}
	// Basic auth rather than the `token` scheme: it works whether the secret is an
	// access token or the account's password, and the test has no business caring
	// which.
	req.SetBasicAuth(g.user, g.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(detail)))
	}
	if out == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (g *giteaForge) PullRequestsForHead(ctx context.Context, head string) ([]pullRequest, error) {
	// Gitea's list endpoint has no head filter, so every pull request comes back and
	// the head is matched here. state=all, or one closed earlier in a test would be
	// invisible and a reopen would read as a duplicate.
	var listed []struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Merged bool   `json:"merged"`
		Head   struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	path := fmt.Sprintf("/repos/%s/pulls?state=all&limit=50", g.slug)
	if err := g.do(ctx, http.MethodGet, path, nil, &listed); err != nil {
		return nil, err
	}
	prs := []pullRequest{}
	for _, pr := range listed {
		if pr.Head.Ref != head {
			continue
		}
		prs = append(prs, pullRequest{
			Number: strconv.Itoa(pr.Number),
			State:  pr.State,
			Merged: pr.Merged,
			Base:   pr.Base.Ref,
		})
	}
	// Newest first, to match what the GitHub forge reports. Gitea's own ordering is
	// not newest-first and the tests read prs[0].
	sort.Slice(prs, func(i, j int) bool {
		a, _ := strconv.Atoi(prs[i].Number)
		b, _ := strconv.Atoi(prs[j].Number)
		return a > b
	})
	return prs, nil
}

func (g *giteaForge) AddComment(ctx context.Context, number, body string) (string, error) {
	var created struct {
		ID int64 `json:"id"`
	}
	err := g.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%s/comments", g.slug, number),
		map[string]string{"body": body}, &created)
	return strconv.FormatInt(created.ID, 10), err
}

func (g *giteaForge) CommentIDs(ctx context.Context, number string) ([]string, error) {
	var listed []struct {
		ID int64 `json:"id"`
	}
	if err := g.do(ctx, http.MethodGet,
		fmt.Sprintf("/repos/%s/issues/%s/comments", g.slug, number), nil, &listed); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(listed))
	for _, c := range listed {
		ids = append(ids, strconv.FormatInt(c.ID, 10))
	}
	return ids, nil
}

func (g *giteaForge) Close(ctx context.Context, number string) error {
	return g.do(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/pulls/%s", g.slug, number),
		map[string]string{"state": "closed"}, nil)
}

func (g *giteaForge) DeleteBranch(ctx context.Context, branch string) error {
	return g.do(ctx, http.MethodDelete,
		fmt.Sprintf("/repos/%s/branches/%s", g.slug, url.PathEscape(branch)), nil, nil)
}
