package gerrit

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/adevinta/maiao/pkg/git"
	"github.com/adevinta/maiao/pkg/log"
	"github.com/adevinta/maiao/pkg/system"
	"github.com/sirupsen/logrus"
)

//go:embed commit-msg.sh
var commitMsgHook []byte

// EmbeddedHook returns a copy of the embedded commit-msg hook script.
func EmbeddedHook() []byte {
	out := make([]byte, len(commitMsgHook))
	copy(out, commitMsgHook)
	return out
}

type Interface interface {
	Installed() bool
	Install() error
}

type Gerrit struct {
	gitDir string
}

// Installed reports whether commits in this repository will get a Change-Id from
// the commit message hook.
//
// A hook belonging to something else does not count, even though it sits exactly
// where maiao's belongs. See StateAt.
func (g *Gerrit) Installed() bool {
	path := git.HookPath(g.gitDir, git.CommitMsgHook)
	state := StateAt(path)
	log.Logger.WithFields(logrus.Fields{
		"gitDir":               g.gitDir,
		"commit-hook path":     path,
		"commit-msg installed": state == MaiaoHook,
		"commit-msg state":     state,
	}).Debug("looked for the commit message hook")
	return state == MaiaoHook
}

// Install installs the gerrit commit message hook in a repository
func (g *Gerrit) Install() error {
	return InstallAt(git.HookPath(g.gitDir, git.CommitMsgHook))
}

// InstallAt writes the embedded gerrit commit message hook, executable, at
// path, creating the parent directory if needed.
//
// Unlike Install it does not resolve where the hook belongs, so it can also
// write to locations that are not a repository's hooks directory, such as a git
// template directory.
//
// It overwrites whatever is at path. Callers decide what may be overwritten, by
// asking StateAt first: a ForeignHook belongs to someone else and replacing it
// takes away what it did.
func InstallAt(path string) error {
	l := log.Logger.WithFields(logrus.Fields{
		"commit-hook path": path,
	})
	l.Debug("installing commit message hook")
	d := filepath.Dir(path)
	s, err := system.DefaultFileSystem.Stat(d)
	if err != nil {
		if os.IsNotExist(err) {
			l.Debugf("created hooks directory %s", d)
			err = system.DefaultFileSystem.MkdirAll(d, 0777)
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("failed to create hooks directory %s", d))
			}
		} else {
			return errors.Wrap(err, fmt.Sprintf("failed to create hooks directory %s", d))
		}
	} else {
		if !s.IsDir() {
			return fmt.Errorf("could not create commit message hook, %s is not a directory", d)
		}
	}
	fd, err := system.DefaultFileSystem.Create(path)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to create commit message hook file %s", path))
	}
	defer fd.Close()
	_, err = fd.Write(commitMsgHook)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to write commit message hook file %s", path))
	}

	err = system.DefaultFileSystem.Chmod(path, 0755)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to set execution rights to message hook file %s", path))
	}
	return nil
}

// Installed returned wether the gerrit hook message is installed
func Installed(gitDir string) bool {
	g := &Gerrit{gitDir}
	return g.Installed()
}

// Install installs the gerrit commit message hook in a repository
func Install(gitDir string) error {
	g := &Gerrit{gitDir}
	return g.Install()
}
