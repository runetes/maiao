package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/google/go-github/v90/github"
	"github.com/google/uuid"
	"github.com/runetes/maiao/pkg/credentials"
	gh "github.com/runetes/maiao/pkg/github"
	"github.com/runetes/maiao/pkg/log"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripperFunc func(r *http.Request) (*http.Response, error)

func (rt roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return rt(r)
}

type fakeCredentials struct {
	c    credentials.Credentials
	fail bool
}

func (f fakeCredentials) CredentialForHost(string) (*credentials.Credentials, error) {
	if f.fail {
		return nil, fmt.Errorf("testError")
	}
	return &f.c, nil
}

func setDefaultCredentials(getter credentials.CredentialGetter) {
	gh.DefaultCredentialGetter = getter
}

func newTestClient(rt http.RoundTripper) *github.Client {
	c, _ := github.NewClient(github.WithHTTPClient(&http.Client{Transport: rt}))
	return c
}

func tempEnv(key, value string) func() {
	old := os.Getenv(key)
	os.Setenv(key, value)
	return func() {
		os.Setenv(key, old)
	}
}

func TestEnsureReturnsAnErrorWhenFailingToReachGithub(t *testing.T) {
	defer func(transport http.RoundTripper) {
		http.DefaultTransport = transport
	}(http.DefaultTransport)
	g := GitHub{
		Client: newTestClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("not implemented")
		})),
	}
	pr, _, err := g.Ensure(context.Background(), PullRequestOptions{Head: "some-ref"})
	assert.Nil(t, pr)
	assert.Error(t, err)
}

// TestEnsureReturnsAnErrorWhenTheListCannotBeRead covers a list response maiao
// cannot make sense of — the second entry below is not valid JSON.
//
// It was named for a "too many matching pull requests" error it never reached: the
// decode fails first. That error is gone now, because listing every state legitimately
// returns several pull requests for one head branch, and
// TestFindPicksTheNewestWhenSeveralMatchTheHeadBranch covers what happens instead.
func TestEnsureReturnsAnErrorWhenTheListCannotBeRead(t *testing.T) {
	defer func(transport http.RoundTripper) {
		http.DefaultTransport = transport
	}(http.DefaultTransport)
	g := GitHub{
		Owner:      "test-owner",
		Repository: "test-repository",
		Client: newTestClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			responseReader := strings.NewReader(`[
				{
					"url": "https://api.github.com/repos/kubernetes/kubernetes/pulls/99491",
					"id": 580868689,
					"number": 99491,
					"state": "open",
					"locked": false,
					"title": "Fix typo in comment for purgeInitContainers.",
					"body": "",
					"created_at": "2021-02-26T13:37:21Z",
					"updated_at": "2021-02-26T13:40:57Z",
					"closed_at": null,
					"merged_at": null,
					"merge_commit_sha": "a91f8e0cd2f8a932564928b79fc482ee60e2f0a2",
					"author_association": "CONTRIBUTOR",
					"auto_merge": null,
					"active_lock_reason": null
				},
				{
					"number": 8435,
				}
				]`)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(responseReader)}, nil
		})),
	}
	pr, _, err := g.Ensure(context.Background(), PullRequestOptions{Head: "some-ref"})
	assert.Nil(t, pr)
	assert.Error(t, err)
}

func TestEnsureReturnsExisingPR(t *testing.T) {
	defer func(transport http.RoundTripper) {
		http.DefaultTransport = transport
	}(http.DefaultTransport)
	g := GitHub{
		Owner:      "test-owner",
		Repository: "test-repository",
		Client: newTestClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			fmt.Println(r.URL.String())
			responseReader := strings.NewReader(`[
				{
					"url": "https://api.github.com/repos/kubernetes/kubernetes/pulls/99491",
					"id": 580868689,
					"html_url": "https://github.com/kubernetes/kubernetes/pull/99491",
					"number": 99491,
					"state": "open",
					"locked": false,
					"title": "Fix typo in comment for purgeInitContainers.",
					"body": "",
					"created_at": "2021-02-26T13:37:21Z",
					"updated_at": "2021-02-26T13:40:57Z",
					"closed_at": null,
					"merged_at": null,
					"merge_commit_sha": "a91f8e0cd2f8a932564928b79fc482ee60e2f0a2",
					"author_association": "CONTRIBUTOR",
					"auto_merge": null,
					"active_lock_reason": null
				}
				]`)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(responseReader)}, nil
		})),
	}
	pr, _, err := g.Ensure(context.Background(), PullRequestOptions{Head: "some-ref"})
	assert.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "https://github.com/kubernetes/kubernetes/pull/99491", pr.URL)
	assert.Equal(t, "99491", pr.ID)
}

func TestEnsureCreatesAndReturnsNewPRWhenNotExisting(t *testing.T) {
	defer func(transport http.RoundTripper) {
		http.DefaultTransport = transport
	}(http.DefaultTransport)
	g := GitHub{
		Owner:      "test-owner",
		Repository: "test-repository",
		Client: newTestClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			fmt.Println(r.URL.String())
			switch r.URL.Path {
			case "/repos/test-owner/test-repository/pulls":
				switch r.Method {
				case http.MethodGet:
					responseReader := strings.NewReader(`[
					]`)
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(responseReader)}, nil
				case http.MethodPost:
					reader, writer := io.Pipe()
					go func() {
						require.NoError(t, json.NewEncoder(writer).Encode(github.PullRequest{
							Number:  github.Int(12345),
							HTMLURL: github.String("https://github.com/repos/test-owner/pull/12345"),
						}))
					}()
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(reader)}, nil
				default:
					return nil, fmt.Errorf("unexpected %s to url '%s'", r.Method, r.URL.String())
				}
			default:
				return nil, fmt.Errorf("unexpected %s to url '%s'", r.Method, r.URL.String())
			}
		})),
	}
	pr, _, err := g.Ensure(context.Background(), PullRequestOptions{Head: "some-ref"})
	assert.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "https://github.com/repos/test-owner/pull/12345", pr.URL)
	assert.Equal(t, "12345", pr.ID)
}

func TestFindReturnsNilWhenNoPRExists(t *testing.T) {
	defer func(transport http.RoundTripper) {
		http.DefaultTransport = transport
	}(http.DefaultTransport)
	g := GitHub{
		Owner:      "test-owner",
		Repository: "test-repository",
		Client: newTestClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			responseReader := strings.NewReader(`[]`)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(responseReader)}, nil
		})),
	}
	pr, err := g.Find(context.Background(), "some-ref")
	assert.NoError(t, err)
	assert.Nil(t, pr)
}

func TestFindReturnsExistingPR(t *testing.T) {
	defer func(transport http.RoundTripper) {
		http.DefaultTransport = transport
	}(http.DefaultTransport)
	g := GitHub{
		Owner:      "test-owner",
		Repository: "test-repository",
		Client: newTestClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			responseReader := strings.NewReader(`[
				{
					"url": "https://api.github.com/repos/kubernetes/kubernetes/pulls/99491",
					"id": 580868689,
					"html_url": "https://github.com/kubernetes/kubernetes/pull/99491",
					"number": 99491,
					"state": "open",
					"base": {
						"ref": "main"
					}
				}
				]`)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(responseReader)}, nil
		})),
	}
	pr, err := g.Find(context.Background(), "some-ref")
	assert.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "99491", pr.ID)
	assert.Equal(t, "https://github.com/kubernetes/kubernetes/pull/99491", pr.URL)
	assert.Equal(t, "main", pr.Base)
}

type TransportFunc func(r *http.Request) (*http.Response, error)

func (t TransportFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return t(r)
}

func TestNewGitHubUpserter(t *testing.T) {
	originalTransport := http.DefaultTransport
	t.Cleanup(func() {
		http.DefaultTransport = originalTransport
	})
	defer tempEnv("GITHUB_TOKEN", "")()
	t.Run("when the repository does not contain slashes, constructor fails", func(t *testing.T) {
		g, err := NewGitHubUpserter(context.Background(), &transport.Endpoint{Path: "non-valid"})
		assert.Error(t, err)
		assert.Nil(t, g)
	})
	t.Run("when the repository does contains several slashes, constructor fails", func(t *testing.T) {
		g, err := NewGitHubUpserter(context.Background(), &transport.Endpoint{Path: "non/valid/repo"})
		assert.Error(t, err)
		assert.Nil(t, g)
	})
	t.Run("when failing to retrieve credentials", func(t *testing.T) {
		defer setDefaultCredentials(gh.DefaultCredentialGetter)
		setDefaultCredentials(fakeCredentials{fail: true})
		t.Run("when the repository starts with a slash", func(t *testing.T) {
			g, err := NewGitHubUpserter(context.Background(), &transport.Endpoint{Path: "/org/repo"})
			assert.Error(t, err)
			assert.Nil(t, g)
		})
	})
	t.Run("when credentials are empty", func(t *testing.T) {
		defer setDefaultCredentials(gh.DefaultCredentialGetter)
		setDefaultCredentials(fakeCredentials{c: credentials.Credentials{}})
		t.Run("when the repository starts with a slash", func(t *testing.T) {
			g, err := NewGitHubUpserter(context.Background(), &transport.Endpoint{Path: "org/repo"})
			assert.Error(t, err)
			assert.Nil(t, g)
		})
	})
	t.Run("with valid credentials", func(t *testing.T) {
		defer setDefaultCredentials(gh.DefaultCredentialGetter)
		setDefaultCredentials(fakeCredentials{c: credentials.Credentials{Password: "password"}})

		http.DefaultTransport = TransportFunc(func(r *http.Request) (*http.Response, error) {
			assert.Equal(t, "/api/v3/repos/org/repo", r.URL.Path)
			b := bytes.Buffer{}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(&b),
			}
			return resp, json.NewEncoder(&b).Encode(github.Repository{
				Owner: &github.User{
					Login: github.String("owner-login"),
				},
				Name: github.String("repo-name"),
			})
		})
		t.Run("when the repository starts with a slash", func(t *testing.T) {
			g, err := NewGitHubUpserter(context.Background(), &transport.Endpoint{Path: "/org/repo", Host: "github.company.example.com"})
			assert.NoError(t, err)
			require.NotNil(t, g)
			assert.Equal(t, "owner-login", g.Owner)
			assert.Equal(t, "repo-name", g.Repository)
		})
		t.Run("when the repository ends with a slash", func(t *testing.T) {
			g, err := NewGitHubUpserter(context.Background(), &transport.Endpoint{Path: "org/repo/", Host: "github.company.example.com"})
			assert.NoError(t, err)
			require.NotNil(t, g)
			assert.Equal(t, "owner-login", g.Owner)
			assert.Equal(t, "repo-name", g.Repository)
		})
	})
	t.Run("when token is provided in the environment, the value is handled", func(t *testing.T) {
		defer tempEnv("GITHUB_TOKEN", "some-token")()
		g, err := NewGitHubUpserter(context.Background(), &transport.Endpoint{Path: "org/repo", Host: "github.company.example.com"})
		assert.NoError(t, err)
		assert.NotNil(t, g)
	})
	t.Run("when using GitHub.com api.github.com domain is used", func(t *testing.T) {
		defer tempEnv("GITHUB_TOKEN", "some-token")()
		http.DefaultTransport = TransportFunc(func(r *http.Request) (*http.Response, error) {
			assert.Equal(t, "/repos/org/repo", r.URL.Path)
			b := bytes.Buffer{}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(&b),
			}
			return resp, json.NewEncoder(&b).Encode(github.Repository{
				Owner: &github.User{
					Login: github.String("github-owner-login"),
				},
				Name: github.String("github-repo-name"),
			})
		})
		g, err := NewGitHubUpserter(context.Background(), &transport.Endpoint{Host: "github.com", Path: "org/repo"})
		assert.NoError(t, err)
		require.NotNil(t, g)
		assert.Equal(t, "https://api.github.com/", g.Client.BaseURL())
		assert.Equal(t, "github-owner-login", g.Owner)
		assert.Equal(t, "github-repo-name", g.Repository)
	})
}

func TestGitHubUpsert(t *testing.T) {
	gh, err := NewGitHubUpserter(context.Background(), &transport.Endpoint{
		Host: "github.com",
		Path: "runetes/maiao-tests",
	})
	if err != nil {
		t.Skipf("Failed to initialise GitHub upserter, " +
			"please run the tests with credentials either in your ~/.netrc" +
			"or with GITHUB_TOKEN environment variable set")
	}
	u := uuid.New().String()
	head := "tests/go/upsert/" + u
	t.Cleanup(func() {
		gh.Git.DeleteRef(context.Background(), "runetes", "maiao-tests", "refs/heads/"+head)
	})

	rc, _, err := gh.Repositories.GetCommit(context.Background(), "runetes", "maiao-tests", "main", &github.ListOptions{})
	require.NoError(t, err)

	c, _, err := gh.Git.CreateCommit(context.Background(), "runetes", "maiao-tests", github.Commit{
		Message: github.String("Test commit " + u),
		Tree:    rc.Commit.Tree,
		Parents: []*github.Commit{{SHA: rc.SHA}},
	}, nil)
	if err != nil {
		if ghError, ok := err.(*github.ErrorResponse); ok {
			if ghError.Response.StatusCode == http.StatusForbidden {
				t.Skipf("not enough permissions to run this check: %v", err)
				return
			}
		}
	}
	require.NoError(t, err)
	require.NotNil(t, c)
	_, _, err = gh.Git.CreateRef(context.Background(), "runetes", "maiao-tests", github.CreateRef{
		Ref: "refs/heads/" + head,
		SHA: c.GetSHA(),
	})
	require.NoError(t, err)
	pr, created, err := gh.Ensure(context.Background(), PullRequestOptions{Base: "main", Head: head, Title: "test-" + u})
	if pr != nil {
		require.NotEqual(t, "", pr.ID)
		id, err := strconv.Atoi(pr.ID)
		require.NoError(t, err)
		t.Cleanup(func() {
			gh.PullRequests.Edit(context.Background(), "runetes", "maiao-tests", id, &github.PullRequest{
				State: github.String("Closed"),
			})
		})
	}
	assert.True(t, created)
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.NotEqual(t, "", pr.ID)
	assert.NotEqual(t, "", pr.URL)
	_, created, err = gh.Ensure(context.Background(), PullRequestOptions{Base: "main", Head: head, Title: "test-" + u})
	require.NoError(t, err)
	assert.False(t, created)

}

func TestLinkedTopicIssues(t *testing.T) {
	g := GitHub{
		Host:  "github.com",
		Owner: "ble",
	}
	assert.Equal(
		t,
		"https://github.com/search?q=is%3Apr+is%3Aopen+%22topic-sha%22+org%3Able&type=issues",
		g.LinkedTopicIssues("topic-sha"),
	)
}

// recordedRequest is one call a test provider made, kept so a test can assert on
// what went to GitHub rather than only on what came back.
type recordedRequest struct {
	method string
	path   string
	query  string
	body   string
}

// githubReturning builds a provider whose every call is recorded, answering each
// request from respond. Returning an empty body leaves the test with `{}`, which
// decodes into any of the shapes the client expects.
func githubReturning(t *testing.T, calls *[]recordedRequest, respond func(r *http.Request) string) GitHub {
	t.Helper()
	return GitHub{
		Owner:      "test-owner",
		Repository: "test-repository",
		Client: newTestClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			body := ""
			if r.Body != nil {
				raw, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				body = strings.TrimSpace(string(raw))
			}
			*calls = append(*calls, recordedRequest{
				method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: body,
			})
			payload := respond(r)
			if payload == "" {
				payload = "{}"
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}, nil
		})),
	}
}

func listedPullRequest(number int, state, mergedAt string) string {
	merged := "null"
	if mergedAt != "" {
		merged = strconv.Quote(mergedAt)
	}
	return fmt.Sprintf(`{"number": %d, "state": %q, "merged_at": %s,
		"html_url": "https://github.com/test-owner/test-repository/pull/%d",
		"base": {"ref": "some-base"}}`, number, state, merged, number)
}

// TestFindAsksForEveryStateSoAClosedPullRequestIsVisible pins the query down
// rather than the answer.
//
// The list endpoint defaults to the open pull requests, and that default is what
// made a review open a second pull request beside a closed one for the same change,
// leaving the conversation on the first.
func TestFindAsksForEveryStateSoAClosedPullRequestIsVisible(t *testing.T) {
	calls := []recordedRequest{}
	g := githubReturning(t, &calls, func(*http.Request) string { return "[]" })

	_, err := g.Find(context.Background(), "maiao.I123")
	require.NoError(t, err)

	require.Len(t, calls, 1)
	assert.Contains(t, calls[0].query, "state=all")
	assert.Contains(t, calls[0].query, "head=test-owner%3Amaiao.I123")
}

// TestFindReportsWhetherAPullRequestCanComeBack is the distinction the rest of the
// review turns on: a closed pull request is reused, a merged one can never be.
//
// GitHub sets merged_at on a pull request it closed because the head became
// reachable from the base, even though nothing was merged, and that is exactly the
// one it refuses to reopen.
func TestFindReportsWhetherAPullRequestCanComeBack(t *testing.T) {
	for _, test := range []struct {
		name     string
		state    string
		mergedAt string
		expected PullRequestState
	}{
		{name: "open", state: "open", expected: PullRequestOpen},
		{name: "closed by hand", state: "closed", expected: PullRequestClosed},
		{name: "closed as merged by a reorder", state: "closed", mergedAt: "2026-09-26T13:43:35Z", expected: PullRequestMerged},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := []recordedRequest{}
			g := githubReturning(t, &calls, func(*http.Request) string {
				return "[" + listedPullRequest(46, test.state, test.mergedAt) + "]"
			})

			pr, err := g.Find(context.Background(), "maiao.I123")
			require.NoError(t, err)
			require.NotNil(t, pr)
			assert.Equal(t, test.expected, pr.State)
			assert.Equal(t, "46", pr.ID)
		})
	}
}

// TestFindPicksTheNewestWhenSeveralMatchTheHeadBranch replaces refusing to choose.
//
// Listing every state returns what earlier runs left behind as well as the live
// pull request, so several matches for one head branch is the damaged repository
// this change is here to cope with — not an ambiguity worth failing a review over.
func TestFindPicksTheNewestWhenSeveralMatchTheHeadBranch(t *testing.T) {
	calls := []recordedRequest{}
	g := githubReturning(t, &calls, func(*http.Request) string {
		return "[" + listedPullRequest(48, "open", "") + "," +
			listedPullRequest(46, "closed", "2026-09-26T13:43:35Z") + "]"
	})

	pr, err := g.Find(context.Background(), "maiao.I123")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "48", pr.ID)
	assert.Contains(t, calls[0].query, "sort=created")
	assert.Contains(t, calls[0].query, "direction=desc")
}

// TestFindPrefersAnOpenPullRequestOverANewerClosedOne guards the review against
// reopening a duplicate of the one it is already using.
//
// GitHub allows only one open pull request per head branch, so where there is an
// open one it is the live review whatever its age. Reopening a closed one that
// happens to be newer would ask GitHub for a second open pull request on that head.
func TestFindPrefersAnOpenPullRequestOverANewerClosedOne(t *testing.T) {
	calls := []recordedRequest{}
	g := githubReturning(t, &calls, func(*http.Request) string {
		return "[" + listedPullRequest(49, "closed", "") + "," +
			listedPullRequest(48, "open", "") + "]"
	})

	pr, err := g.Find(context.Background(), "maiao.I123")
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "48", pr.ID)
	assert.Equal(t, PullRequestOpen, pr.State)
}

// TestEnsureReopensAClosedPullRequestInsteadOfOpeningASecond is the behaviour the
// objective asks for: the change keeps the pull request it had, and the
// conversation on it.
//
// The base is moved before the reopen. GitHub closes a pull request as merged the
// moment its head is reachable from its base, so reopening one while it still names
// the base an earlier run left behind would hand it straight back to that rule —
// and that closure cannot be undone.
func TestEnsureReopensAClosedPullRequestInsteadOfOpeningASecond(t *testing.T) {
	calls := []recordedRequest{}
	g := githubReturning(t, &calls, func(r *http.Request) string {
		if r.Method == http.MethodGet {
			return "[" + listedPullRequest(46, "closed", "") + "]"
		}
		return listedPullRequest(46, "open", "")
	})

	pr, created, err := g.Ensure(context.Background(), PullRequestOptions{
		Head: "maiao.I333", Base: "maiao.I222", Title: "a change", Body: "body",
	})
	require.NoError(t, err)
	assert.False(t, created, "the pull request the change already had is not a new one")
	require.NotNil(t, pr)
	assert.Equal(t, "46", pr.ID)

	methods := []string{}
	for _, call := range calls {
		methods = append(methods, call.method+" "+call.path)
		assert.NotEqual(t, http.MethodPost, call.method, "no second pull request may be opened")
	}
	require.Len(t, methods, 3, "a lookup, then the base, then the reopen: %v", methods)
	// go-github flattens a base edit to the `base` the API expects, rather than the
	// nested ref a pull request is read back as.
	assert.Contains(t, calls[1].body, `"base":"maiao.I222"`, "the base moves first")
	assert.NotContains(t, calls[1].body, `"state"`)
	assert.Contains(t, calls[2].body, `"state":"open"`, "and only then does it reopen")
}

// TestEnsureReopensWithoutTouchingABaseThatIsAlreadyRight is the common closed pull
// request: nothing was reordered, somebody closed the review by hand.
//
// Setting the base it already has is a wasted write, and inside one of GitHub's
// native stacks it is a refusal — `Cannot change the base branch because the pull
// request is part of a stack.` — which is how an end-to-end run found this.
func TestEnsureReopensWithoutTouchingABaseThatIsAlreadyRight(t *testing.T) {
	calls := []recordedRequest{}
	g := githubReturning(t, &calls, func(r *http.Request) string {
		if r.Method == http.MethodGet {
			return `[{"number": 69, "state": "closed", "merged_at": null,
				"html_url": "https://github.com/test-owner/test-repository/pull/69",
				"base": {"ref": "maiao.I111"}}]`
		}
		return listedPullRequest(69, "open", "")
	})

	pr, created, err := g.Ensure(context.Background(), PullRequestOptions{
		Head: "maiao.I222", Base: "maiao.I111", Title: "a change",
	})
	require.NoError(t, err)
	assert.False(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "69", pr.ID)

	require.Len(t, calls, 2, "a lookup, then the reopen, and no base edit")
	assert.Contains(t, calls[1].body, `"state":"open"`)
	assert.NotContains(t, calls[1].body, `"base"`)
}

// TestEnsureLeavesAPullRequestClosedWhenItsBaseWillNotMove is the choice between two
// losses.
//
// Opening a new pull request costs the conversation on the closed one. Reopening it
// onto a base branch that contains its head costs the conversation and the pull
// request, because the provider reads that as merged and will not reopen it again. So
// a base that has to move and will not leaves the pull request closed.
func TestEnsureLeavesAPullRequestClosedWhenItsBaseWillNotMove(t *testing.T) {
	calls := []recordedRequest{}
	g := GitHub{
		Owner:      "test-owner",
		Repository: "test-repository",
		Client: newTestClient(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			calls = append(calls, recordedRequest{method: r.Method, path: r.URL.Path})
			switch {
			case r.Method == http.MethodGet:
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
					"[" + listedPullRequest(69, "closed", "") + "]"))}, nil
			case r.Method == http.MethodPatch:
				return &http.Response{StatusCode: http.StatusUnprocessableEntity, Request: r,
					Body: io.NopCloser(strings.NewReader(`{"message":"Validation Failed","errors":[
						{"resource":"PullRequest","field":"base","code":"invalid",
						 "message":"Cannot change the base branch because the pull request is part of a stack."}]}`))}, nil
			default:
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
					listedPullRequest(72, "open", "")))}, nil
			}
		})),
	}

	pr, created, err := g.Ensure(context.Background(), PullRequestOptions{
		Head: "maiao.I222", Base: "maiao.I111", Title: "a change",
	})
	require.NoError(t, err, "the review carries on with a new pull request")
	assert.True(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "72", pr.ID)

	require.Len(t, calls, 3, "a lookup, the refused base edit, then a create")
	assert.Equal(t, http.MethodPatch, calls[1].method)
	assert.Equal(t, http.MethodPost, calls[2].method, "no reopen was attempted after the refusal")
}

// TestEnsureOpensANewPullRequestWhenTheExistingOneIsMerged is the case nothing can
// recover: GitHub answers a reopen of a merged pull request with
// `422 state cannot be changed. The pull request cannot be reopened.`
func TestEnsureOpensANewPullRequestWhenTheExistingOneIsMerged(t *testing.T) {
	calls := []recordedRequest{}
	g := githubReturning(t, &calls, func(r *http.Request) string {
		if r.Method == http.MethodGet {
			return "[" + listedPullRequest(46, "closed", "2026-09-26T13:43:35Z") + "]"
		}
		return listedPullRequest(48, "open", "")
	})

	pr, created, err := g.Ensure(context.Background(), PullRequestOptions{
		Head: "maiao.I333", Base: "maiao.I222", Title: "a change", Body: "body",
	})
	require.NoError(t, err)
	assert.True(t, created)
	require.NotNil(t, pr)
	assert.Equal(t, "48", pr.ID)

	require.Len(t, calls, 2, "a lookup, then a create")
	assert.Equal(t, http.MethodPost, calls[1].method, "reopening it is refused, so a new one is opened")
}

// get all logs when running tests
func init() {
	log.Logger.SetLevel(logrus.DebugLevel)
}
