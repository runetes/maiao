package system

import (
	"os"
	"os/user"
	"strings"
)

var CurrentUser = user.Current

// HomeDir returns the directory a user's own files live in.
//
// HOME decides, which is what git, ssh and curl all do, and what makes a
// container, a CI job or an agent sandbox that overrides it read the files it
// meant to rather than the ones belonging to whoever the process happens to run
// as. The passwd database is consulted only when HOME says nothing, where there
// is nothing else to go on.
func HomeDir() (string, error) {
	// os.UserHomeDir reads HOME, and the platform's own equivalent elsewhere.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home, nil
	}
	usr, err := CurrentUser()
	if err != nil {
		return "", err
	}
	return usr.HomeDir, nil
}

var (
	originalFS = DefaultFileSystem

	originalEnv     = Env()
	originalWorkDir = workdir()
)

// Env returns the current environment variables as a usable map
func Env() map[string]string {
	r := map[string]string{}
	for _, value := range os.Environ() {
		key := strings.Split(value, "=")[0]
		r[key] = os.Getenv(key)
	}
	return r
}

func GetenvOrDefault(key, defaultValue string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return defaultValue
}

func workdir() string {
	wd, err := os.Getwd()
	if err != nil {
		// explicitly ignore this error, just have a condition to please linters
	}
	return wd
}

// Reset restores the system environment as it was when the package was imported
func Reset() {
	CurrentUser = user.Current
	e := Env()
	for key, old := range originalEnv {
		if new, ok := e[key]; !ok || new != old {
			os.Setenv(key, old)
		}
	}
	for key := range e {
		if _, ok := originalEnv[key]; !ok {
			os.Unsetenv(key)
		}
	}
	DefaultFileSystem = originalFS
	if originalWorkDir != "" {
		err := os.Chdir(originalWorkDir)
		if err != nil {
			// explicitly ignore this error, just have a condition to please linters
		}
	}
}
