package ssh

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/adevinta/maiao/pkg/prompt"
)

func IsKnownHostsError(err error) (host string, isMismatch bool, ok bool) {
	if err == nil {
		return "", false, false
	}
	msg := err.Error()
	if strings.Contains(msg, "knownhosts: key mismatch") {
		return extractHostFromError(msg), true, true
	}
	if strings.Contains(msg, "knownhosts: key is unknown") {
		return extractHostFromError(msg), false, true
	}
	return "", false, false
}

func PromptAndFix(host string, isMismatch bool) error {
	var question string
	if isMismatch {
		question = fmt.Sprintf("SSH host key mismatch for %s (the server's key has changed). Update it?", host)
	} else {
		question = fmt.Sprintf("SSH host key not found for %s. Add it automatically?", host)
	}

	if !prompt.YesNo(question) {
		return fmt.Errorf("SSH host key issue for %s not resolved", host)
	}

	if isMismatch {
		if err := removeHostKeys(host); err != nil {
			return fmt.Errorf("failed to remove old host keys: %w", err)
		}
	}

	if err := addHostKeys(host); err != nil {
		return fmt.Errorf("failed to add host keys: %w", err)
	}

	return nil
}

// knownHostsPath is the file both removeHostKeys and addHostKeys operate on.
//
// It has to be resolved once and passed explicitly: ssh-keygen derives the home
// directory from the passwd database and ignores HOME, so leaving it implicit
// makes the two functions disagree about which file they are editing whenever
// HOME is overridden, as it is in containers, CI and agent sandboxes.
func knownHostsPath() string {
	return filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts")
}

func removeHostKeys(host string) error {
	path := knownHostsPath()
	// ssh-keygen exits non-zero when handed a path it cannot stat, so skip it
	// rather than report a failure: no file means there is no key to remove.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	cmd := exec.Command("ssh-keygen", "-R", host, "-f", path)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func addHostKeys(host string) error {
	knownHostsPath := knownHostsPath()

	if err := os.MkdirAll(filepath.Dir(knownHostsPath), 0700); err != nil {
		return err
	}

	cmd := exec.Command("ssh-keyscan", host)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("ssh-keyscan failed: %w", err)
	}
	if len(out) == 0 {
		return fmt.Errorf("ssh-keyscan returned no keys for %s", host)
	}

	f, err := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(out)
	return err
}

func extractHostFromError(msg string) string {
	// go-git wraps errors like:
	// "ssh: handshake failed: knownhosts: key mismatch"
	// The host isn't always in the error message itself, but we can check
	// for common patterns. We'll rely on the caller passing the host from
	// the endpoint when the error message doesn't contain it.
	//
	// Some versions include the host in brackets: [host]:port
	// Try to extract from common patterns
	for _, prefix := range []string{"dial tcp ", "connect to "} {
		if idx := strings.Index(msg, prefix); idx >= 0 {
			rest := msg[idx+len(prefix):]
			if colonIdx := strings.Index(rest, ":"); colonIdx > 0 {
				host := rest[:colonIdx]
				host = strings.TrimPrefix(host, "[")
				host = strings.TrimSuffix(host, "]")
				return host
			}
		}
	}
	return ""
}
