package system_test

import (
	"errors"
	"os"
	"os/user"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/adevinta/maiao/pkg/system"
)

func TestGetEnvOrDefault(t *testing.T) {
	t.Cleanup(system.Reset)
	os.Setenv("some-key", "some-value")

	assert.Equal(t, system.GetenvOrDefault("other-key", "default-value"), "default-value")
	assert.Equal(t, system.GetenvOrDefault("some-key", "default-value"), "some-value")
}

func TestEnvConsidersCurrentEnvironmentVariables(t *testing.T) {
	t.Cleanup(system.Reset)
	_, ok := system.Env()["some-key"]
	assert.False(t, ok)
	os.Setenv("some-key", "some-value")
	assert.Equal(t, "some-value", system.Env()["some-key"])
}

func TestResetRecoversDeletedEnvironmentVariables(t *testing.T) {
	env := system.Env()

	os.Unsetenv("PATH")
	require.NotEqual(t, env, system.Env())
	system.Reset()
	assert.Equal(t, env, system.Env())
}

func TestResetRestoresModifiedEnvironmentVariables(t *testing.T) {
	env := system.Env()

	os.Setenv("PATH", "alternative-value")
	require.NotEqual(t, env, system.Env())
	system.Reset()
	assert.Equal(t, env, system.Env())
}

func TestResetRemovesAddedEnvironmentVariables(t *testing.T) {
	env := system.Env()

	os.Setenv("some-key", "some-value")
	require.NotEqual(t, env, system.Env())
	system.Reset()
	assert.Equal(t, env, system.Env())
}

func TestResetRestoresDefaultFileSystem(t *testing.T) {
	fs := system.DefaultFileSystem
	system.DefaultFileSystem = afero.NewMemMapFs()
	require.NotEqual(t, fs, system.DefaultFileSystem)
	system.Reset()
	assert.Equal(t, fs, system.DefaultFileSystem)
}

func TestResetRestoresWorkingDirectory(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir("../"))
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NotEqual(t, wd, cwd)
	system.Reset()
	cwd, err = os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, wd, cwd)
}

func TestHomeDir(t *testing.T) {
	// The two answers have to differ for the test to say anything: whichever one
	// the code picks would otherwise look right.
	passwd := "/passwd/database/home"
	stubPasswdHome := func(t *testing.T) {
		t.Cleanup(system.Reset)
		system.CurrentUser = func() (*user.User, error) {
			return &user.User{HomeDir: passwd}, nil
		}
	}

	t.Run("HOME decides", func(t *testing.T) {
		stubPasswdHome(t)
		t.Setenv("HOME", "/from/home")
		home, err := system.HomeDir()
		require.NoError(t, err)
		assert.Equal(t, "/from/home", home,
			"a container, CI job or sandbox that overrides HOME means the files under it")
	})

	// Only then: there is nothing else to go on.
	t.Run("without HOME, the passwd database answers", func(t *testing.T) {
		stubPasswdHome(t)
		t.Setenv("HOME", "")
		home, err := system.HomeDir()
		require.NoError(t, err)
		assert.Equal(t, passwd, home)
	})

	t.Run("with neither, it fails rather than guessing", func(t *testing.T) {
		t.Cleanup(system.Reset)
		t.Setenv("HOME", "")
		system.CurrentUser = func() (*user.User, error) {
			return nil, errors.New("no passwd entry")
		}
		_, err := system.HomeDir()
		assert.Error(t, err, "an empty home would name a path relative to the working directory")
	})
}
