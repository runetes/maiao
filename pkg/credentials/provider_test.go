package credentials

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCredentialGetterForProviderUsesCorrectEnvVar(t *testing.T) {
	tests := []struct {
		provider string
		envKey   string
	}{
		{"github", "GITHUB_TOKEN"},
		{"gitlab", "GITLAB_TOKEN"},
		{"gitea", "GITEA_TOKEN"},
		{"forgejo", "FORGEJO_TOKEN"},
		{"bitbucket", "BITBUCKET_TOKEN"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			old := os.Getenv(tt.envKey)
			defer os.Setenv(tt.envKey, old)
			os.Setenv(tt.envKey, "test-token-"+tt.provider)

			getter := CredentialGetterForProvider(tt.provider)
			cred, err := getter.CredentialForHost("any-host.example.com")
			require.NoError(t, err)
			assert.Equal(t, "test-token-"+tt.provider, cred.Password)
			assert.Equal(t, "x-token", cred.Username)
		})
	}
}

func TestCredentialGetterForProviderFallsBackToGitHubForUnknown(t *testing.T) {
	old := os.Getenv("GITHUB_TOKEN")
	defer os.Setenv("GITHUB_TOKEN", old)
	os.Setenv("GITHUB_TOKEN", "fallback-token")

	getter := CredentialGetterForProvider("unknown")
	cred, err := getter.CredentialForHost("any-host.example.com")
	require.NoError(t, err)
	assert.Equal(t, "fallback-token", cred.Password)
}

func TestCredentialGetterForProviderReturnsErrorWhenNoCredentials(t *testing.T) {
	// Ensure env var is unset
	old := os.Getenv("GITLAB_TOKEN")
	defer os.Setenv("GITLAB_TOKEN", old)
	os.Unsetenv("GITLAB_TOKEN")

	getter := CredentialGetterForProvider("gitlab")
	_, err := getter.CredentialForHost("non-existent-host-in-netrc.example.com")
	assert.Error(t, err)
}
