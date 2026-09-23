package api

import (
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/runetes/maiao/pkg/log"
)

// API is the package handling all API interactions

// Repo describes how to reach a repository using an API
type Repo struct {
	// Domain is the domain the API is exposed on
	Domain string
	// Repository is the name of the repository exposed on the API
	// (owner/repository for github repositories)
	Repository string
	// Username contains the name of the authenticated user
	// accessing the repository API
	Username string
	// Password contains the password of the authenticated user
	// accessing the repository API
	Password string
}

// NewRepoFromGitRemote parses the a git remote URL to determine
// the API configuration
func NewRepoFromGitRemote(remoteName string) (*Repo, error) {
	endpoint, err := transport.NewEndpoint(remoteName)
	if err != nil {
		log.Logger.WithField("remote-url", remoteName).Errorf("failed to parse remote: %v", err)
		return nil, err
	}
	r := &Repo{
		Domain:     endpoint.Host,
		Repository: strings.TrimSuffix(strings.TrimPrefix(endpoint.Path, "/"), ".git"),
	}
	// The API endpoint is exposed over HTTP, those are the only credentials interesting
	// for us. ssh (and git) credentials could not be useful to access HTTPS API
	if strings.HasPrefix(endpoint.Protocol, "http") {
		r.Username = endpoint.User
		r.Password = endpoint.Password
	}
	return r, nil
}
