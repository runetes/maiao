//go:build e2e

// Package e2e drives the real `git review` binary against a real forge.
//
// maiao's unit tests stand in a fake provider, and the behaviour that matters most
// here is one no fake has: a forge marks a pull request merged, and closes it, as
// soon as its head commits become reachable from its base branch. Reordering a stack
// does exactly that to the branch a pull request still names as its base, and the
// closure cannot be undone — GitHub answers a reopen with
// `422 state cannot be changed. The pull request cannot be reopened.`
//
// So these tests are the only place the fix can be shown to work, and they read the
// forge with their own client rather than through maiao's providers: a test that
// asked the code under test what happened would only prove it self-consistent.
//
// They are behind the `e2e` build tag and skip themselves unless a forge is
// configured, because they open and close real pull requests. Point them at a
// throwaway repository, never one with reviews that matter:
//
//	go test -tags e2e -count=1 -timeout 45m ./test/e2e/
//
// GitHub, with a token or a GitHub App to mint one from:
//
//	MAIAO_E2E_GITHUB_REPO=owner/repo
//	MAIAO_E2E_GITHUB_TOKEN=...
//	  or MAIAO_E2E_GITHUB_APP_ID, MAIAO_E2E_GITHUB_APP_KEY (a .pem path),
//	     MAIAO_E2E_GITHUB_INSTALLATION_ID
//
// Gitea or Forgejo:
//
//	MAIAO_E2E_GITEA_URL=https://your.gitea
//	MAIAO_E2E_GITEA_REPO=owner/repo
//	MAIAO_E2E_GITEA_USER=the account the token belongs to
//	MAIAO_E2E_GITEA_TOKEN=an access token or password
//
// MAIAO_E2E_KEEP=1 leaves the pull requests and branches behind for inspection.
package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// pullRequest is what a test needs to know about one, whatever forge it came from.
type pullRequest struct {
	Number string
	// State is the forge's own word, normalised to "open" or "closed".
	State string
	// Merged is the forge calling it merged, which is the state nothing recovers
	// from. It is set even when nothing was really merged.
	Merged bool
	Base   string
}

func (pr pullRequest) String() string {
	merged := ""
	if pr.Merged {
		merged = " merged"
	}
	return fmt.Sprintf("#%s %s%s on %s", pr.Number, pr.State, merged, pr.Base)
}

// forge is the part of a code host these tests use, which is deliberately less than
// maiao needs: enough to set a repository up, observe pull requests, and put a
// conversation on one.
type forge interface {
	// Name appears in subtest names, so it is the forge's short name.
	Name() string
	// CloneURL carries whatever credential the initial clone needs.
	CloneURL() string
	// RemoteURL is what `origin` is left pointing at, which must not carry a
	// credential: maiao derives the API host from it.
	RemoteURL() string
	// Netrc is the lines of a .netrc that let maiao authenticate, for every host it
	// will talk to — for GitHub that is both github.com and api.github.com.
	//
	// A .netrc rather than the provider's token environment variable, because that
	// variable does not carry a username and maiao sends `x-token`: Gitea answers 401
	// to that, whoever the token belongs to. The test writes this into a HOME of its
	// own, so it depends on nothing about the machine running it.
	Netrc() []string
	// Secrets is every credential this forge holds, so that nothing the test logs can
	// print one.
	Secrets() []string
	// GitConfig is set on the fixture repository, for a host maiao cannot recognise
	// on its own.
	GitConfig() [][2]string

	// PullRequestsForHead reports every pull request for a head branch in any state,
	// newest first. An empty slice means none, which is not an error.
	PullRequestsForHead(ctx context.Context, head string) ([]pullRequest, error)
	// AddComment puts a comment on a pull request and returns its identifier.
	AddComment(ctx context.Context, number, body string) (string, error)
	// CommentIDs lists the identifiers of the comments on a pull request.
	CommentIDs(ctx context.Context, number string) ([]string, error)
	// Close closes a pull request without merging it, which is the state a reopen is
	// for.
	Close(ctx context.Context, number string) error
	// DeleteBranch removes a branch, so a test leaves the repository as it found it.
	DeleteBranch(ctx context.Context, branch string) error
}

// forges returns every forge this run is configured for, skipping the test when
// there is none.
//
// A missing configuration is a skip rather than a failure: these tests need
// credentials for a throwaway repository on a live host, which a plain `go test`
// has no business assuming. The skip message says exactly what to set.
func forges(t *testing.T) []forge {
	t.Helper()
	configured := []forge{}
	missing := []string{}

	if f, why := newGitHubForge(); f != nil {
		configured = append(configured, f)
	} else {
		missing = append(missing, why)
	}
	if f, why := newGiteaForge(); f != nil {
		configured = append(configured, f)
	} else {
		missing = append(missing, why)
	}

	if len(configured) == 0 {
		t.Skipf("no forge configured for the end-to-end tests:\n  %s", strings.Join(missing, "\n  "))
	}
	return configured
}

func env(key string) string { return strings.TrimSpace(os.Getenv(key)) }
