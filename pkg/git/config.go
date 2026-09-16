package git

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
)

// ConfigGet returns the value of a git configuration key as git resolves it,
// across every scope and through any include directive, and reports whether it
// is set at all.
//
// repoPath selects the repository whose local configuration takes part in the
// lookup. An empty repoPath consults only the global and system scopes, which is
// what commands that operate outside a repository need.
//
// go-git is not used here for the same reason as in HookPath: it does not merge
// raw configuration sections across scopes, so a value set in the user's global
// configuration would be invisible.
func ConfigGet(repoPath, key string) (string, bool) {
	return configGet(repoPath, key)
}

// ConfigPath behaves like ConfigGet but has git canonicalise the value as a
// path, which expands a leading ~ the way git does everywhere else.
func ConfigPath(repoPath, key string) (string, bool) {
	return configGet(repoPath, key, "--type=path")
}

// ConfigBool reports whether a git configuration key is set to a true value,
// using git's own notion of truth, so true, yes, on, 1 and a bare key all count.
// A key that is unset, or set to something git cannot read as a boolean, is
// reported as false.
func ConfigBool(repoPath, key string) bool {
	value, ok := configGet(repoPath, key, "--type=bool")
	return ok && value == "true"
}

// ConfigSetGlobal sets a git configuration key in the user's global
// configuration.
func ConfigSetGlobal(key, value string) error {
	c := exec.Command("git", "config", "--global", key, value)
	c.Stderr = os.Stderr
	return c.Run()
}

func configGet(repoPath, key string, args ...string) (string, bool) {
	c := exec.Command("git", append(append([]string{"config"}, args...), "--get", key)...)
	if repoPath != "" {
		c.Dir = repoPath
	}
	out := bytes.NewBuffer(nil)
	c.Stdout = out
	// git exits non zero simply because the key is unset, which is not worth
	// reporting to the user.
	c.Stderr = io.Discard
	if err := c.Run(); err != nil {
		return "", false
	}
	return strings.Trim(out.String(), " \n"), true
}
