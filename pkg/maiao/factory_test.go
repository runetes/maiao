package maiao

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/runetes/maiao/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestForgeEndpointKeepsTransportEndpoint checks both halves of the split: the
// API is pointed at the forge host, and the endpoint fetch and push use still
// names the alias, so ssh keeps picking the key configured for it.
func TestForgeEndpointKeepsTransportEndpoint(t *testing.T) {
	testutil.IsolateHome(t)
	dir := testutil.InitRepo(t)
	testutil.Cmd(t, "git", "-C", dir, "config", "maiao.work.github.com.host", "github.com")

	endpoint, err := transport.NewEndpoint("git@work.github.com:org/repo.git")
	require.NoError(t, err)

	forge := forgeEndpoint(endpoint, dir)

	assert.Equal(t, "github.com", forge.Host)
	assert.Equal(t, endpoint.Path, forge.Path)
	assert.Equal(t, "work.github.com", endpoint.Host, "the transport endpoint must not be modified")
}
