package provider

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())
	// git requires user config for some operations
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = dir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = dir
	cmd.Run()
	return dir
}

func TestDetectKnownHosts(t *testing.T) {
	tests := []struct {
		host     string
		expected Type
	}{
		{"github.com", GitHub},
		{"gitlab.com", GitLab},
		{"codeberg.org", Forgejo},
		{"bitbucket.org", Bitbucket},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			result, err := Detect(tt.host, "")
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetectReadsFromGitConfig(t *testing.T) {
	dir := initTestRepo(t)

	cmd := exec.Command("git", "config", "--local", "maiao.provider", "gitlab")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	result, err := Detect("git.company.com", dir)
	assert.NoError(t, err)
	assert.Equal(t, GitLab, result)
}

func TestDetectIgnoresInvalidGitConfigValue(t *testing.T) {
	dir := initTestRepo(t)

	cmd := exec.Command("git", "config", "--local", "maiao.provider", "invalid-provider")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	// Should fall through to prompt since config value is invalid.
	// Since we can't easily mock the prompt in this package, we test
	// readProviderFromConfig directly.
	_, err := readProviderFromConfig(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider type")
}

func TestReadProviderFromConfigNoConfig(t *testing.T) {
	dir := initTestRepo(t)
	_, err := readProviderFromConfig(dir)
	assert.Error(t, err)
}

func TestReadProviderFromConfigValidValues(t *testing.T) {
	for _, pt := range AllTypes {
		t.Run(string(pt), func(t *testing.T) {
			dir := initTestRepo(t)
			cmd := exec.Command("git", "config", "--local", "maiao.provider", string(pt))
			cmd.Dir = dir
			require.NoError(t, cmd.Run())

			result, err := readProviderFromConfig(dir)
			assert.NoError(t, err)
			assert.Equal(t, pt, result)
		})
	}
}

func TestWriteProviderToConfig(t *testing.T) {
	dir := initTestRepo(t)
	writeProviderToConfig(dir, GitLab)

	cmd := exec.Command("git", "config", "--local", "maiao.provider")
	cmd.Dir = dir
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "gitlab\n", string(out))
}

func TestWriteProviderToConfigUpdatesExisting(t *testing.T) {
	dir := initTestRepo(t)
	writeProviderToConfig(dir, GitLab)
	writeProviderToConfig(dir, Gitea)

	cmd := exec.Command("git", "config", "--local", "maiao.provider")
	cmd.Dir = dir
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "gitea\n", string(out))
}

func TestDetectKnownHostTakesPrecedenceOverConfig(t *testing.T) {
	dir := initTestRepo(t)

	cmd := exec.Command("git", "config", "--local", "maiao.provider", "bitbucket")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	// github.com is a known host and should take precedence
	result, err := Detect("github.com", dir)
	assert.NoError(t, err)
	assert.Equal(t, GitHub, result)
}

func TestDetectWithEmptyRepoPath(t *testing.T) {
	// For known hosts, empty repoPath should still work
	result, err := Detect("gitlab.com", "")
	assert.NoError(t, err)
	assert.Equal(t, GitLab, result)
}

func TestDetectWithNonExistentPath(t *testing.T) {
	// Unknown host + non-existent path should fail (can't read config, can't prompt in test)
	nonExistent := filepath.Join(os.TempDir(), "non-existent-repo-path-12345")
	_, err := Detect("git.company.com", nonExistent)
	assert.Error(t, err)
}
