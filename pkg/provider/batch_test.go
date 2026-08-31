package provider

import (
	"testing"
	"time"

	"github.com/adevinta/maiao/pkg/prompt"
	"github.com/adevinta/maiao/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setBatch(t testing.TB, b bool) {
	t.Helper()
	original := prompt.Batch()
	t.Cleanup(func() { prompt.SetBatch(original) })
	prompt.SetBatch(b)
}

// TestDetectInBatchModeFailsWithGuidance covers the case an automated caller
// actually hits: a self-hosted host maiao does not recognise, with nothing
// configured to say what it is.
func TestDetectInBatchModeFailsWithGuidance(t *testing.T) {
	testutil.IsolateHome(t)
	dir := testutil.InitRepo(t)
	setBatch(t, true)

	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		_, err = Detect("git.company.example", dir)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Detect blocked instead of failing: batch mode must never wait for input")
	}

	require.Error(t, err)
	assert.ErrorIs(t, err, prompt.ErrNoInput)
	assert.Contains(t, err.Error(), "git.company.example", "the message must name the host it could not identify")
	assert.Contains(t, err.Error(), providerOption, "the message must name the setting that resolves it")
	for _, typ := range Types {
		assert.Contains(t, err.Error(), string(typ), "the message must list the accepted values")
	}
}

// TestDetectReadsProviderFromGlobalConfig is the reason readProviderFromConfig
// stopped restricting itself to local scope: one setting should cover every
// repository on a self-hosted host, rather than being repeated in each clone.
func TestDetectReadsProviderFromGlobalConfig(t *testing.T) {
	testutil.IsolateHome(t)
	dir := testutil.InitRepo(t)
	testutil.Cmd(t, "git", "config", "--global", providerOption, "gitea")
	// Batch mode makes the assertion meaningful: had the global value not been
	// found, this would fail rather than quietly prompt.
	setBatch(t, true)

	result, err := Detect("git.company.example", dir)

	require.NoError(t, err)
	assert.Equal(t, Gitea, result)
}

// TestDetectPrefersLocalOverGlobalProvider checks scope precedence, so that a
// global default can still be overridden for one repository.
func TestDetectPrefersLocalOverGlobalProvider(t *testing.T) {
	testutil.IsolateHome(t)
	dir := testutil.InitRepo(t)
	testutil.Cmd(t, "git", "config", "--global", providerOption, "gitea")
	testutil.Cmd(t, "git", "-C", dir, "config", "--local", providerOption, "gitlab")
	setBatch(t, true)

	result, err := Detect("git.company.example", dir)

	require.NoError(t, err)
	assert.Equal(t, GitLab, result)
}

// TestDetectKnownHostNeedsNoConfig documents that the common case never reaches
// either config or a prompt, which is why batch mode is not disruptive.
func TestDetectKnownHostNeedsNoConfig(t *testing.T) {
	testutil.IsolateHome(t)
	setBatch(t, true)

	result, err := Detect("github.com", "")

	require.NoError(t, err)
	assert.Equal(t, GitHub, result)
}
