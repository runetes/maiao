package provider

import (
	"fmt"
	"os/exec"
	"strings"

	lgit "github.com/adevinta/maiao/pkg/git"
	"github.com/adevinta/maiao/pkg/prompt"
	"github.com/sirupsen/logrus"
)

const providerOption = "maiao.provider"

// Types lists the providers a user can choose from, in the order they are
// offered.
var Types = []Type{GitLab, GitHub, Gitea, Forgejo, Bitbucket, Origin}

func Detect(host string, repoPath string) (Type, error) {
	if t, ok := KnownHosts[host]; ok {
		return t, nil
	}

	t, err := readProviderFromConfig(repoPath)
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
		return "", fmt.Errorf("%w: cannot tell which provider %s is.\nRun `git config --global %s <%s>` to configure it",
			prompt.ErrNoInput, host, providerOption, strings.Join(items, "|"))
	}
	label := fmt.Sprintf("Detected remote host: %s. Which provider is this?", host)
	idx, err := prompt.Select(label, items)
	if err != nil {
		return "", err
	}
	t, _ := ParseType(items[idx])
	return t, nil
}
