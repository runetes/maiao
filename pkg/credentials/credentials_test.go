package credentials

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getterFunc is a stand-alone CredentialGetter whose answer the test controls.
type getterFunc func(string) (*Credentials, error)

func (f getterFunc) CredentialForHost(host string) (*Credentials, error) { return f(host) }

func failing(err string) getterFunc {
	return func(string) (*Credentials, error) { return nil, errors.New(err) }
}

func answering(c Credentials) getterFunc {
	return func(string) (*Credentials, error) { return &c, nil }
}

// TestChainSkipsPasswordlessCredentials is the bug that sent an unauthenticated
// request to Gitea: ~/.netrc held a machine block whose keys the parser did not
// recognise, so the netrc getter answered with an empty password rather than
// failing, and the chain stopped there. The forge then answered 404 for a
// private repository, which says nothing about authentication.
func TestChainSkipsPasswordlessCredentials(t *testing.T) {
	chain := ChainCredentialGetter{
		answering(Credentials{Username: "me"}),
		answering(Credentials{Username: "me", Password: "a-token"}),
	}

	c, err := chain.CredentialForHost("example.com")

	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "a-token", c.Password)
}

// TestChainReportsEveryReason keeps the diagnosis in the error: a user whose
// only credential source answered with nothing usable needs to be told which
// source that was.
func TestChainReportsEveryReason(t *testing.T) {
	chain := ChainCredentialGetter{
		failing("no GITEA_TOKEN in the environment"),
		answering(Credentials{Username: "me"}),
	}

	c, err := chain.CredentialForHost("gitea.example.com")

	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "no GITEA_TOKEN in the environment")
	assert.Contains(t, err.Error(), "gitea.example.com", "the message must name the host that could not be authenticated")
}

func TestChainReturnsTheFirstUsableCredential(t *testing.T) {
	chain := ChainCredentialGetter{
		failing("nothing here"),
		answering(Credentials{Username: "first", Password: "first-token"}),
		answering(Credentials{Username: "second", Password: "second-token"}),
	}

	c, err := chain.CredentialForHost("example.com")

	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "first-token", c.Password)
}
