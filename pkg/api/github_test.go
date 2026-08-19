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

	"github.com/adevinta/maiao/pkg/credentials"
	gh "github.com/adevinta/maiao/pkg/github"
	"github.com/adevinta/maiao/pkg/log"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/google/go-github/v90/github"
	"github.com/google/uuid"
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

func TestEnsureReturnsAnErrorWhenTooManyPRs(t *testing.T) {
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
		Host: "github.com",
	}
	assert.Equal(
		t,
		"https://github.com/search?q=is%3Apr+is%3Aopen+%22topic-sha%22&type=issues",
		g.LinkedTopicIssues("topic-sha"),
	)
}

// get all logs when running tests
func init() {
	log.Logger.SetLevel(logrus.DebugLevel)
}
