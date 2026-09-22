package credentials

import (
	"fmt"
	"os/user"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"github.com/adevinta/maiao/pkg/system"
)

const testNetrc = `machine login.example.com
    login this-is-a-login

machine example.com
    login  me
    password a-secure-password

machine password.example.com
    password pass


machine github.company.example.com
    password personalAccessToken
`

func setupFS(t *testing.T) afero.Fs {
	t.Helper()
	fs := afero.NewMemMapFs()
	system.DefaultFileSystem = fs
	t.Cleanup(system.Reset)
	return fs
}

func testCredentialSuccess(t *testing.T, n *Netrc, machine string, expected Credentials) {
	t.Helper()
	c, err := n.CredentialForHost(machine)
	assert.NoError(t, err)
	assert.NotNil(t, c)
	assert.Equal(t, expected, *c)
}

func TestNetrcCredentials(t *testing.T) {
	var n *Netrc
	t.Run("when netrc handler is null, an error is returned", func(t *testing.T) {
		c, err := n.CredentialForHost("some host")
		assert.Error(t, err)
		assert.Nil(t, c)
	})

	t.Run("when failing to resolve the home directory, an error is returned", func(t *testing.T) {
		fs := setupFS(t)
		_ = fs
		t.Setenv("HOME", "")
		system.CurrentUser = func() (*user.User, error) {
			return nil, fmt.Errorf("test error")
		}
		n := &Netrc{}
		c, err := n.CredentialForHost("some host")
		assert.Error(t, err)
		assert.Nil(t, c)
	})

	t.Run("the default path comes from HOME, not the passwd database", func(t *testing.T) {
		fs := setupFS(t)
		home := "/test-home"
		passwd := "/passwd-home"
		system.EnsureTestFileContent(t, fs, home+"/.netrc",
			"machine example.com\n  login me\n  password the-credentials-under-home\n")
		system.EnsureTestFileContent(t, fs, passwd+"/.netrc",
			"machine example.com\n  login someone-else\n  password the-credentials-of-the-passwd-user\n")
		t.Setenv("HOME", home)
		system.CurrentUser = func() (*user.User, error) {
			return &user.User{HomeDir: passwd}, nil
		}

		testCredentialSuccess(t, &Netrc{}, "example.com", Credentials{
			Username: "me", Password: "the-credentials-under-home",
		})
	})

	t.Run("when no path is provided, a default one is created", func(t *testing.T) {
		fs := setupFS(t)
		home := "/default-home"
		system.EnsureTestFileContent(t, fs, home+"/.netrc", testNetrc)
		t.Setenv("HOME", home)

		n := &Netrc{}
		_, _ = n.CredentialForHost("example.com")
		assert.Equal(t, home+"/.netrc", n.Path)
	})

	t.Run("when a path is provided", func(t *testing.T) {
		fs := setupFS(t)
		system.EnsureTestFileContent(t, fs, "/netrc-test/.netrc", testNetrc)
		n := &Netrc{Path: "/netrc-test/.netrc"}

		t.Run("and the machine has only login, credentials is returned", func(t *testing.T) {
			testCredentialSuccess(t, n, "login.example.com", Credentials{Username: "this-is-a-login"})
		})
		t.Run("and the machine has only password, credentials is returned", func(t *testing.T) {
			testCredentialSuccess(t, n, "password.example.com", Credentials{Password: "pass"})
		})
		t.Run("and the machine has login and password, credentials is returned", func(t *testing.T) {
			testCredentialSuccess(t, n, "example.com", Credentials{Username: "me", Password: "a-secure-password"})
		})
	})

	t.Run("when the path does not exist, an error is returned", func(t *testing.T) {
		setupFS(t)
		n := &Netrc{Path: "/nonexistent/.netrc"}
		c, err := n.CredentialForHost("any")
		assert.Error(t, err)
		assert.Nil(t, c)
	})
}
