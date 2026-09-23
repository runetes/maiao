package credentials_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/runetes/maiao/pkg/credentials"
	"github.com/runetes/maiao/pkg/system"
)

func TestWithNoUsernameEnvironmentVariableDefaultIsUsed(t *testing.T) {
	t.Cleanup(system.Reset)
	os.Setenv("MY_TOKEN", "some-value")
	os.Unsetenv("MY_USER")
	t.Run("When the credentials does not define a default username, x-token is used", func(t *testing.T) {
		getter := credentials.EnvToken{PasswordKey: "MY_TOKEN"}
		creds, err := getter.CredentialForHost("somehost.io")
		require.NoError(t, err)
		assert.Equal(t, "x-token", creds.Username)
	})
	t.Run("When the credentials does define a default username, it is used", func(t *testing.T) {
		getter := credentials.EnvToken{PasswordKey: "MY_TOKEN", DefaultUserName: "my-user"}
		creds, err := getter.CredentialForHost("somehost.io")
		require.NoError(t, err)
		assert.Equal(t, "my-user", creds.Username)
	})
	t.Run("When the username environment variable is not defined, default is used", func(t *testing.T) {
		getter := credentials.EnvToken{PasswordKey: "MY_TOKEN", DefaultUserName: "my-user", UsernameKey: "MY_USER"}
		creds, err := getter.CredentialForHost("somehost.io")
		require.NoError(t, err)
		assert.Equal(t, "my-user", creds.Username)
	})
	t.Run("When the username environment variable is  defined, it is used", func(t *testing.T) {
		os.Setenv("MY_USER", "some-user")
		getter := credentials.EnvToken{PasswordKey: "MY_TOKEN", DefaultUserName: "my-user", UsernameKey: "MY_USER"}
		creds, err := getter.CredentialForHost("somehost.io")
		require.NoError(t, err)
		assert.Equal(t, "some-user", creds.Username)
	})
}

func TestEnvCredentialsRequiredEnvironmentKey(t *testing.T) {
	getter := credentials.EnvToken{}
	creds, err := getter.CredentialForHost("somehost.io")
	require.Error(t, err)
	assert.Nil(t, creds)
}

func TestEnvCredentialsRetrievesTokenFromEnv(t *testing.T) {
	t.Cleanup(system.Reset)
	os.Unsetenv("MY_TOKEN")
	t.Run("When the the password environment variable is not set, credentials getting fails", func(t *testing.T) {
		getter := credentials.EnvToken{PasswordKey: "MY_TOKEN"}
		creds, err := getter.CredentialForHost("somehost.io")
		require.Error(t, err)
		assert.Nil(t, creds)
	})
	t.Run("When the credentials don't specify a username, the default is used and proper credentials are retrieved", func(t *testing.T) {
		os.Setenv("MY_TOKEN", "token-value")
		getter := credentials.EnvToken{PasswordKey: "MY_TOKEN"}
		creds, err := getter.CredentialForHost("somehost.io")
		require.NoError(t, err)
		assert.Equal(t, "token-value", creds.Password)
	})
}
