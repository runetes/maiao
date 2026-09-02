package gerrit

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/adevinta/maiao/pkg/git"
	"github.com/adevinta/maiao/pkg/log"
	"github.com/adevinta/maiao/pkg/system"
	"github.com/sirupsen/logrus"
)

const (
	gitHubURL         = "https://raw.githubusercontent.com"
	repo              = "GerritCodeReview/gerrit"
	commitHash        = "43d985a2a15a7d59d42e19ffd60d41c0de6c3e59"
	commitMsgHookPath = "gerrit-server/src/main/resources/com/google/gerrit/server/tools/root/hooks/commit-msg"
)

var commitMsgHookURL = fmt.Sprintf("%s/%s/%s/%s", gitHubURL, repo, commitHash, commitMsgHookPath)

type Interface interface {
	Installed() bool
	Install() error
}

type Gerrit struct {
	gitDir string
}

func HookURL() string {
	return commitMsgHookURL
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

// InstallAt downloads the gerrit commit message hook and writes it, executable,
// at path, creating the parent directory if needed.
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
		"download-url":     commitMsgHookURL,
	})
	l.Debug("downloading commit message hook")
	r, err := http.Get(commitMsgHookURL)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to download commit message hook from %s", commitMsgHookURL))
	}
	defer r.Body.Close()
	l.Debugf("downloaded commit message hook")
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
	_, err = io.Copy(fd, r.Body)
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
