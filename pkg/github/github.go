package gh

import (
	"context"
	"fmt"
	"net/http"

	"github.com/adevinta/maiao/pkg/log"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/google/go-github/v90/github"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

func GitHubAPIDomain(domain string) string {
	if domain == "github.com" {
		return "api.github.com"
	}
	return domain
}

func NewHTTPClientForDomain(ctx context.Context, domain string) (*http.Client, error) {
	domain = GitHubAPIDomain(domain)
	// TODO: move this to handle unauthorized calls.
	token, err := getGithubToken(domain)
	if err != nil {
		log.ForContext(ctx).WithError(err).WithField("domain", domain).Errorf("unable to find token")
		return nil, fmt.Errorf("unable to find token for %s: %s", domain, err.Error())
	}
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	return tc, nil
}

func NewGraphQLClient(httpClient *http.Client, domain string) (*api.GraphQLClient, error) {
	opts := api.ClientOptions{
		AuthToken: "overridden by Transport",
		Host:      GitHubAPIDomain(domain),
		Transport: httpClient.Transport,
	}
	client, err := api.NewGraphQLClient(opts)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// NewClient instanciates a new github client depending on the domain name
//
// When requesting a client for a different host than github.com,
// a client for github enterprise is considered
//
// Credentials are even taken from GITHUB_TOKEN environment variable or
// from your ~/.netrc file
func NewClient(httpClient *http.Client, domain string) (*github.Client, error) {
	opts := []github.ClientOptionsFunc{
		github.WithHTTPClient(httpClient),
	}
	switch domain {
	case "github.com", "api.github.com":
	default:
		apiDomain := GitHubAPIDomain(domain)
		baseURL := "https://" + apiDomain + "/api/v3/"
		uploadURL := "https://" + apiDomain + "/api/v3/upload/"
		opts = append(opts, github.WithEnterpriseURLs(baseURL, uploadURL))
	}
	return github.NewClient(opts...)
}

func getGithubToken(domain string) (string, error) {
	creds, err := DefaultCredentialGetter.CredentialForHost(domain)
	if err != nil {
		return "", err
	}
	token := findFirstNonEmptyString(creds.Password, creds.Username)
	if token != "" {
		Logger.WithFields(logrus.Fields{
			"context": "initializing GitHub client",
		}).Debugf("using github token from credentials store")
		return token, nil
	}
	return "", fmt.Errorf("unable to find a token for domain %s", domain)
}

func findFirstNonEmptyString(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
