//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// githubForge talks to api.github.com with its own client, so what it reports about a
// pull request is independent of the code under test.
type githubForge struct {
	slug  string // owner/repo
	owner string
	token string
}

// newGitHubForge reads the configuration, returning why it is unusable instead of a
// forge when it is.
//
// A token can be given directly, or minted from a GitHub App's key, which is what a
// throwaway repository usually has: an installation token is scoped to it and expires
// by itself.
func newGitHubForge() (forge, string) {
	slug := env("MAIAO_E2E_GITHUB_REPO")
	if slug == "" {
		return nil, "MAIAO_E2E_GITHUB_REPO=owner/repo for a throwaway GitHub repository"
	}
	owner, _, ok := strings.Cut(slug, "/")
	if !ok {
		return nil, fmt.Sprintf("MAIAO_E2E_GITHUB_REPO=%q is not owner/repo", slug)
	}

	token := env("MAIAO_E2E_GITHUB_TOKEN")
	if token == "" {
		minted, err := mintInstallationToken(
			env("MAIAO_E2E_GITHUB_APP_ID"),
			env("MAIAO_E2E_GITHUB_APP_KEY"),
			env("MAIAO_E2E_GITHUB_INSTALLATION_ID"),
		)
		if err != nil {
			return nil, fmt.Sprintf("MAIAO_E2E_GITHUB_TOKEN, or an app to mint one from: %v", err)
		}
		token = minted
	}
	return &githubForge{slug: slug, owner: owner, token: token}, ""
}

func (g *githubForge) Name() string      { return "github" }
func (g *githubForge) RemoteURL() string { return "https://github.com/" + g.slug + ".git" }

// CloneURL carries the token as x-access-token, which is the username GitHub
// documents for an app installation token and accepts for any other.
func (g *githubForge) CloneURL() string {
	return "https://x-access-token:" + g.token + "@github.com/" + g.slug + ".git"
}

// Netrc covers both hosts maiao reaches: it pushes to github.com and calls
// api.github.com, and its credential lookup is per host.
func (g *githubForge) Netrc() []string {
	return []string{
		"machine github.com login x-access-token password " + g.token,
		"machine api.github.com login x-access-token password " + g.token,
	}
}

func (g *githubForge) Secrets() []string { return []string{g.token} }

// GitConfig is empty: github.com is one of the hosts maiao resolves on its own.
func (g *githubForge) GitConfig() [][2]string { return nil }

func (g *githubForge) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
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

func (g *githubForge) PullRequestsForHead(ctx context.Context, head string) ([]pullRequest, error) {
	// state=all, or a pull request an earlier step closed would be invisible and the
	// test could not tell a reopen from a duplicate.
	var listed []struct {
		Number   int     `json:"number"`
		State    string  `json:"state"`
		MergedAt *string `json:"merged_at"`
		Base     struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	path := fmt.Sprintf("/repos/%s/pulls?state=all&sort=created&direction=desc&per_page=20&head=%s:%s",
		g.slug, g.owner, head)
	if err := g.do(ctx, http.MethodGet, path, nil, &listed); err != nil {
		return nil, err
	}
	prs := make([]pullRequest, 0, len(listed))
	for _, pr := range listed {
		prs = append(prs, pullRequest{
			Number: strconv.Itoa(pr.Number),
			State:  pr.State,
			// merged_at rather than `merged`: the list endpoint does not return the
			// latter, and a pull request closed by the reachability rule carries the
			// former even though nothing was merged.
			Merged: pr.MergedAt != nil,
			Base:   pr.Base.Ref,
		})
	}
	return prs, nil
}

func (g *githubForge) AddComment(ctx context.Context, number, body string) (string, error) {
	var created struct {
		ID int64 `json:"id"`
	}
	err := g.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%s/comments", g.slug, number),
		map[string]string{"body": body}, &created)
	return strconv.FormatInt(created.ID, 10), err
}

func (g *githubForge) CommentIDs(ctx context.Context, number string) ([]string, error) {
	var listed []struct {
		ID int64 `json:"id"`
	}
	if err := g.do(ctx, http.MethodGet,
		fmt.Sprintf("/repos/%s/issues/%s/comments?per_page=100", g.slug, number), nil, &listed); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(listed))
	for _, c := range listed {
		ids = append(ids, strconv.FormatInt(c.ID, 10))
	}
	return ids, nil
}

func (g *githubForge) Close(ctx context.Context, number string) error {
	return g.do(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/pulls/%s", g.slug, number),
		map[string]string{"state": "closed"}, nil)
}

func (g *githubForge) DeleteBranch(ctx context.Context, branch string) error {
	return g.do(ctx, http.MethodDelete,
		fmt.Sprintf("/repos/%s/git/refs/heads/%s", g.slug, branch), nil, nil)
}

// mintInstallationToken exchanges a GitHub App's private key for an installation
// token.
//
// Hand-rolled rather than pulled in as a dependency: it is a signed JSON pair and one
// POST, and maiao ships no JWT library for a test to borrow.
func mintInstallationToken(appID, keyPath, installationID string) (string, error) {
	switch {
	case appID == "":
		return "", fmt.Errorf("MAIAO_E2E_GITHUB_APP_ID is not set")
	case keyPath == "":
		return "", fmt.Errorf("MAIAO_E2E_GITHUB_APP_KEY is not set")
	case installationID == "":
		return "", fmt.Errorf("MAIAO_E2E_GITHUB_INSTALLATION_ID is not set")
	}
	if strings.HasPrefix(keyPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		keyPath = home + keyPath[1:]
	}
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return "", err
	}
	key, err := parsePrivateKey(raw)
	if err != nil {
		return "", err
	}

	// iat a minute in the past: GitHub rejects a JWT issued in the future, and reports
	// it as a bad key rather than a bad clock.
	now := time.Now()
	header := base64url(`{"alg":"RS256","typ":"JWT"}`)
	claims := base64url(fmt.Sprintf(`{"iat":%d,"exp":%d,"iss":%q}`,
		now.Add(-time.Minute).Unix(), now.Add(9*time.Minute).Unix(), appID))
	signingInput := header + "." + claims
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	jwt := signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)

	req, err := http.NewRequest(http.MethodPost,
		"https://api.github.com/app/installations/"+installationID+"/access_tokens", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		detail, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("minting an installation token: %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	var minted struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&minted); err != nil {
		return "", err
	}
	return minted.Token, nil
}

// parsePrivateKey accepts either PEM encoding GitHub hands out for an app key.
func parsePrivateKey(raw []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("the app key is not PEM encoded")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("the app key is neither PKCS#1 nor PKCS#8: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("the app key is a %T, and GitHub signs with RSA", parsed)
	}
	return key, nil
}

func base64url(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
