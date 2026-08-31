package ssh

import (
	"errors"
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

// TrustNewHostsEnvVar opts in to accepting the key of a host that is not yet in
// known_hosts without asking. It has no effect on a key mismatch.
const TrustNewHostsEnvVar = "MAIAO_TRUST_NEW_SSH_HOSTS"

// ErrHostKeyMismatch reports that the key a host presented differs from the one
// recorded in known_hosts.
var ErrHostKeyMismatch = errors.New("SSH host key mismatch")

var trustNewHosts = envIsTrue(TrustNewHostsEnvVar)

// SetTrustNewHosts opts in to trusting the key of an unknown host on first use,
// overriding the environment variable.
func SetTrustNewHosts(b bool) {
	trustNewHosts = b
}

// TrustNewHosts reports whether the key of an unknown host is accepted without
// asking.
func TrustNewHosts() bool {
	return trustNewHosts
}

func envIsTrue(name string) bool {
	switch strings.ToLower(os.Getenv(name)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// PromptAndFix resolves an SSH host key problem, asking the user first.
//
// The two problems it is given are not equally safe to resolve, and are handled
// separately on purpose.
//
// A key that is merely unknown is trust on first use: the same exposure as any
// first connection to a host, which SSH itself resolves by asking. It can be
// accepted without a question, but only when the user has explicitly opted in.
//
// A key mismatch means the recorded key and the presented key disagree. That
// happens when a server is legitimately rekeyed, and equally when a connection
// is being intercepted, and the two are indistinguishable from here. Resolving
// it means deleting a key that was known to be good, so it is never done without
// a person saying so, whatever the opt-in says.
func PromptAndFix(host string, isMismatch bool) error {
	if isMismatch {
		if prompt.Batch() {
			// The path is spelled out with -f because ssh-keygen resolves the home
			// directory from the passwd database, so a bare -R can edit a different
			// file than the one maiao reads.
			return fmt.Errorf(`%w for %s.
The key on record is not the key the server presented. The server may have been
rekeyed, or the connection may be intercepted, and maiao cannot tell which.
Verify the new key through a channel you trust, then forget the old one with
`+"`ssh-keygen -R %s -f %s`"+`.
%s has been left untouched`, ErrHostKeyMismatch, host, host, knownHostsPath(), knownHostsPath())
		}
		if !prompt.YesNo(fmt.Sprintf("SSH host key mismatch for %s (the server's key has changed). Update it?", host)) {
			return fmt.Errorf("%w for %s, not resolved", ErrHostKeyMismatch, host)
		}
		if err := removeHostKeys(host); err != nil {
			return fmt.Errorf("failed to remove old host keys: %w", err)
		}
	} else if !trustNewHosts {
		if prompt.Batch() {
			return fmt.Errorf(`%w: no key for %s on record.
Record it when building the environment, with
`+"`ssh-keyscan %s >> %s`"+`,
or set %s=1 to trust whichever key %s offers on first use`,
				prompt.ErrNoInput, host, host, knownHostsPath(), TrustNewHostsEnvVar, host)
		}
		if !prompt.YesNo(fmt.Sprintf("SSH host key not found for %s. Add it automatically?", host)) {
			return fmt.Errorf("SSH host key issue for %s not resolved", host)
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
