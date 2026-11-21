package provider

import (
	"fmt"
	"os/exec"
	"strings"

	lgit "github.com/runetes/maiao/pkg/git"
	"github.com/runetes/maiao/pkg/prompt"
	"github.com/sirupsen/logrus"
)

const providerOption = "maiao.provider"

// Types lists the providers a user can choose from, in the order they are
// offered.
var Types = []Type{GitLab, GitHub, Gitea, Forgejo, Bitbucket, Origin}

// Detect returns the provider behind a remote whose URL names host.
//
// A setting for that one host wins over everything else, since it is the most
// specific thing the user has said. Known hosts are then matched against the
// forge host rather than the remote's own, so an SSH alias for github.com is
// recognised as GitHub.
func Detect(host string, repoPath string) (Type, error) {
	t, ok, err := readHostProviderFromConfig(repoPath, host)
	if err != nil {
		return "", err
	}
	if ok {
		return t, nil
	}

	if t, ok := KnownHosts[ForgeHost(host, repoPath)]; ok {
		return t, nil
	}

	t, err = readProviderFromConfig(repoPath)
	if err == nil {
		return t, nil
	}

	t, err = promptForProvider(host)
	if err != nil {
		return "", err
	}

	if err := writeProviderToConfig(repoPath, t); err != nil {
		logrus.WithError(err).Warn("failed to save provider to git config")
	}
	return t, nil
}

// ForgeHost returns the host that serves the forge's API and web pages for a
// remote whose URL names host. The two differ when the remote names an SSH alias
// from ~/.ssh/config, typically one per account so that each gets its own key:
// ssh resolves the alias, but an HTTPS client cannot.
func ForgeHost(host, repoPath string) string {
	if forge, ok := lgit.ConfigGet(repoPath, hostOption(host, "host")); ok && forge != "" {
		return forge
	}
	return host
}

// hostOption names a setting that applies to one remote host only, written as a
// git config subsection the way git's own http.<url>.* settings are.
func hostOption(host, name string) string {
	return "maiao." + host + "." + name
}

// readHostProviderFromConfig reads the provider configured for one host. Unlike
// the host-agnostic setting, an unrecognised value is an error rather than a
// reason to prompt: the answer to the prompt is saved host-agnostically, so this
// setting would keep overriding it and the prompt would return on each run.
func readHostProviderFromConfig(repoPath, host string) (Type, bool, error) {
	key := hostOption(host, "provider")
	val, ok := lgit.ConfigGet(repoPath, key)
	if !ok {
		return "", false, nil
	}
	t, parsed := ParseType(val)
	if !parsed {
		return "", false, fmt.Errorf("%s: unknown provider type: %s", key, val)
	}
	return t, true, nil
}

// readProviderFromConfig reads the configured provider from any config scope, so
// that one global setting can cover every repository on a self-hosted host
// rather than needing to be repeated in each one.
func readProviderFromConfig(repoPath string) (Type, error) {
	val, ok := lgit.ConfigGet(repoPath, providerOption)
	if !ok {
		return "", fmt.Errorf("%s is not configured", providerOption)
	}
	t, parsed := ParseType(val)
	if !parsed {
		return "", fmt.Errorf("unknown provider type: %s", val)
	}
	return t, nil
}

// writeProviderToConfig records the answer against the repository rather than
// globally, because a prompted answer was given about one host and guessing that
// it applies to every other host would be wrong.
func writeProviderToConfig(repoPath string, t Type) error {
	cmd := exec.Command("git", "config", "--local", providerOption, string(t))
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	return cmd.Run()
}

func promptForProvider(host string) (Type, error) {
	items := make([]string, 0, len(Types))
	for _, t := range Types {
		items = append(items, string(t))
	}
	if prompt.Batch() {
		return "", fmt.Errorf("%w: cannot tell which provider %s is.\nRun `git config --global %s <%s>` to configure it,\n"+
			"or `git config --global %s <host>` if it is an SSH alias for another host",
			prompt.ErrNoInput, host, providerOption, strings.Join(items, "|"), hostOption(host, "host"))
	}
	label := fmt.Sprintf("Detected remote host: %s. Which provider is this?", host)
	idx, err := prompt.Select(label, items)
	if err != nil {
		return "", err
	}
	t, _ := ParseType(items[idx])
	return t, nil
}
