package credentials

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/jdxcode/netrc"
	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"

	"github.com/adevinta/maiao/pkg/log"
	"github.com/adevinta/maiao/pkg/system"
)

// Netrc implements the CredentialGetter interface,
// getting the credentials from a netrc formatted file.
// When path is empty, the default ~/.netrc path is used
type Netrc struct {
	Path string
}

// CredentialForHost retrieves the credentials for a given host in the netrc file
func (n *Netrc) CredentialForHost(host string) (*Credentials, error) {
	if n == nil {
		return nil, fmt.Errorf("failed to find credentials for machine %s, nil handler", host)
	}
	ctx := log.WithContextFields(context.Background(), logrus.Fields{"context": "parsing netrc", "host": host})
	logger := log.ForContext(ctx)
	if n.Path == "" {
		// From HOME, not from the passwd database: the two differ in a container,
		// under sudo, in CI and in an agent sandbox, and the file the user wrote is
		// the one under HOME.
		home, err := system.HomeDir()
		if err != nil {
			logger.WithError(err).Infof("failed to retrieve the home directory")
			return nil, err
		}
		n.Path = filepath.Join(home, ".netrc")
		logger.Debugf("using default netrc path")
	}
	logger = logger.WithContext(log.WithContextFields(ctx, logrus.Fields{"path": n.Path}))
	content, err := afero.ReadFile(system.DefaultFileSystem, n.Path)
	if err != nil {
		logger.WithError(err).Infof("failed to read netrc file")
		return nil, err
	}
	parsed, err := netrc.ParseString(string(content))
	if err != nil {
		logger.WithError(err).Infof("failed to parse netrc file")
		return nil, err
	}
	machine := parsed.Machine(host)
	if machine == nil {
		return nil, fmt.Errorf("failed to find credentials for host %s in netRC %s", host, n.Path)
	}
	password := machine.Get("password")
	if password == "" {
		// Reported rather than passed on empty: a machine block written with
		// `username:` and `password:` instead of netrc's `login` and `password`
		// parses without error into exactly this state, and the user has no other
		// clue that the file they wrote is being ignored.
		logger.Infof("netrc entry has no password")
		return nil, fmt.Errorf("machine %s in netRC %s has no password: entries read `machine <host> login <user> password <token>`, with no colons", host, n.Path)
	}
	logger.Debugf("found credentials")
	return &Credentials{
		Username: machine.Get("login"),
		Password: password,
	}, nil
}
