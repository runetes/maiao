package provider

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/adevinta/maiao/pkg/prompt"
)

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

	writeProviderToConfig(repoPath, t)
	return t, nil
}

func readProviderFromConfig(repoPath string) (Type, error) {
	cmd := exec.Command("git", "config", "--local", "maiao.provider")
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	val := strings.TrimSpace(string(out))
	t, ok := ParseType(val)
	if !ok {
		return "", fmt.Errorf("unknown provider type: %s", val)
	}
	return t, nil
}

func writeProviderToConfig(repoPath string, t Type) {
	cmd := exec.Command("git", "config", "--local", "maiao.provider", string(t))
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	cmd.Run()
}

func promptForProvider(host string) (Type, error) {
	items := []string{
		string(GitLab),
		string(GitHub),
		string(Gitea),
		string(Forgejo),
		string(Bitbucket),
	}
	label := fmt.Sprintf("Detected remote host: %s. Which provider is this?", host)
	idx, err := prompt.Select(label, items)
	if err != nil {
		return "", err
	}
	t, _ := ParseType(items[idx])
	return t, nil
}
