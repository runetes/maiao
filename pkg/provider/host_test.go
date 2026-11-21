package provider

import (
	"testing"

	"github.com/runetes/maiao/pkg/prompt"
	"github.com/runetes/maiao/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDetectFollowsSSHAliasToKnownHost covers the reason the per-host settings
// exist: a remote such as git@work.github.com:org/repo, where work.github.com is
// a Host entry in ~/.ssh/config picking one account's key, is still GitHub.
func TestDetectFollowsSSHAliasToKnownHost(t *testing.T) {
	testutil.IsolateHome(t)
	dir := testutil.InitRepo(t)
	testutil.Cmd(t, "git", "config", "--global", "maiao.work.github.com.host", "github.com")
	setBatch(t, true)

	result, err := Detect("work.github.com", dir)

	require.NoError(t, err)
	assert.Equal(t, GitHub, result)
}

// TestDetectAliasBeatsHostAgnosticProvider checks that an alias for a known host
// is not swept up by a host-agnostic setting meant for a self-hosted forge.
func TestDetectAliasBeatsHostAgnosticProvider(t *testing.T) {
	testutil.IsolateHome(t)
	dir := testutil.InitRepo(t)
	testutil.Cmd(t, "git", "config", "--global", providerOption, "gitea")
	testutil.Cmd(t, "git", "config", "--global", "maiao.work.github.com.host", "github.com")
	setBatch(t, true)

	result, err := Detect("work.github.com", dir)

	require.NoError(t, err)
	assert.Equal(t, GitHub, result)
}

// TestDetectReadsPerHostProvider lets someone who works against two self-hosted
// forges of different kinds describe both, which one host-agnostic setting
// cannot do.
func TestDetectReadsPerHostProvider(t *testing.T) {
	testutil.IsolateHome(t)
	dir := testutil.InitRepo(t)
	testutil.Cmd(t, "git", "config", "--global", providerOption, "gitlab")
	testutil.Cmd(t, "git", "config", "--global", "maiao.git.company.example.provider", "gitea")
	setBatch(t, true)

	company, err := Detect("git.company.example", dir)
	require.NoError(t, err)
	assert.Equal(t, Gitea, company)

	other, err := Detect("git.other.example", dir)
	require.NoError(t, err)
	assert.Equal(t, GitLab, other, "a per-host setting must not leak to other hosts")
}

func TestDetectRejectsInvalidPerHostProvider(t *testing.T) {
	testutil.IsolateHome(t)
	dir := testutil.InitRepo(t)
	testutil.Cmd(t, "git", "config", "--global", "maiao.git.company.example.provider", "nope")
	setBatch(t, true)

	_, err := Detect("git.company.example", dir)

	require.Error(t, err)
	assert.NotErrorIs(t, err, prompt.ErrNoInput)
	assert.Contains(t, err.Error(), "maiao.git.company.example.provider")
	assert.Contains(t, err.Error(), "nope")
}

func TestForgeHost(t *testing.T) {
	testutil.IsolateHome(t)
	dir := testutil.InitRepo(t)
	testutil.Cmd(t, "git", "-C", dir, "config", "maiao.work.github.com.host", "github.com")

	assert.Equal(t, "github.com", ForgeHost("work.github.com", dir))
	assert.Equal(t, "git.company.example", ForgeHost("git.company.example", dir), "an unconfigured host is its own forge host")
}

func TestDetectInBatchModeSuggestsAliasSetting(t *testing.T) {
	testutil.IsolateHome(t)
	dir := testutil.InitRepo(t)
	setBatch(t, true)

	_, err := Detect("work.github.com", dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "maiao.work.github.com.host")
}
