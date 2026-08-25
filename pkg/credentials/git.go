package credentials

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

var run = realRun

type runOpts struct {
	path  string
	args  []string
	stdin string
}

func realRun(opts runOpts) (string, error) {
	cmd := exec.Command(opts.path, opts.args...)
	b := bytes.Buffer{}
	cmd.Stdout = &b
	if opts.stdin != "" {
		cmd.Stdin = strings.NewReader(opts.stdin)
	}
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return b.String(), nil
}

type GitCredentials struct {
	GitPath string
}

func (c *GitCredentials) CredentialForHost(host string) (*Credentials, error) {
	// this should be better included with the actual git remotes.
	out, err := run(runOpts{path: c.GitPath, args: []string{"credential", "fill"}, stdin: "protocol=https\nhost=" + host})
	if err != nil {
		return nil, err
	}
	if out != "" {
		kv := map[string]string{}
		for _, line := range strings.Split(out, "\n") {
			keyAndValue := strings.SplitN(line, "=", 2)
			if len(keyAndValue) == 2 {
				kv[strings.TrimSpace(keyAndValue[0])] = strings.TrimSuffix(keyAndValue[1], "\n")
			}
		}
		if _, ok := kv["password"]; ok {
			return &Credentials{Username: kv["username"], Password: kv["password"]}, nil
		}
		return nil, errors.New("unable to find username and password from git credential")
	}
	return nil, errors.New("not found")
}
