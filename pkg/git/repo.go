package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/afero"

	"github.com/adevinta/maiao/pkg/system"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

const (
	RebaseArgsEnvVar = "MAIAO_REBASE_ARGUMENTS"
)

type Repository interface {
	Head() (*plumbing.Reference, error)
	Remote(name string) (*git.Remote, error)
	Push(o *git.PushOptions) error
	Branches() (storer.ReferenceIter, error)
	Config() (*config.Config, error)
	Fetch(o *git.FetchOptions) error
	Log(o *git.LogOptions) (object.CommitIter, error)
	ResolveRevision(rev plumbing.Revision) (*plumbing.Hash, error)
	Worktree() (*git.Worktree, error)
}

func MergeBase(ctx context.Context, repo Repository, base, head plumbing.Revision) (plumbing.Hash, error) {
	wt, err := repo.Worktree()
	if err != nil {
		return plumbing.Hash{}, err
	}
	b, err := repo.ResolveRevision(base)
	if err != nil {
		return plumbing.Hash{}, err
	}
	h, err := repo.ResolveRevision(head)
	if err != nil {
		return plumbing.Hash{}, err
	}
	c := exec.Command("git", "-C", wt.Filesystem.Root(), "merge-base", b.String(), h.String())
	out := bytes.Buffer{}
	c.Stdout = &out
	c.Stderr = os.Stderr
	err = c.Run()
	if err != nil {
		return plumbing.Hash{}, err
	}
	return plumbing.NewHash(strings.Trim(out.String(), " \n")), nil
}

func RebaseCommits(ctx context.Context, repo Repository, base, onto plumbing.Hash, todo string) error {
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}

	fd, err := ioutil.TempFile("", "rebase-todo-")
	if err != nil {
		return err
	}
	defer func() {
		os.Remove(fd.Name())
	}()
	todo = todo + "\n" + "exec " + os.Args[0]
	_, err = fd.Write([]byte(todo + "\n"))
	if err != nil {
		fd.Close()
		return err
	}
	err = fd.Close()
	if err != nil {
		return err
	}

	c := exec.Command("git", "-C", wt.Filesystem.Root(), "rebase", "-i", "--onto", onto.String(), base.String())
	// Rebase progress is diagnostics, and this command re-invokes maiao as its last
	// todo step, so whatever stream it gets is also the nested run's stdout.
	c.Stdout = os.Stderr
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	args, err := json.Marshal(os.Args[1:])
	if err != nil {
		return err
	}
	c.Env = append(os.Environ(), "GIT_EDITOR="+os.Args[0]+" add-change-id-editor "+fd.Name(), RebaseArgsEnvVar+"="+string(args))
	err = c.Run()
	if err != nil {
		return err
	}
	return nil
}

func resolveGitDir(gitDir string) (string, error) {
	for {
		stat, err := system.DefaultFileSystem.Stat(gitDir)
		if err != nil {
			return "", err
		}
		if stat.IsDir() {
			return gitDir, nil
		}
		// gitdir may be a file containing the path to the git directory
		// with git specific formatting.
		// In case gitdir is a file, parse it to resolve the actual gitDir
		data, err := afero.ReadFile(system.DefaultFileSystem, gitDir)
		if err != nil {
			return "", errors.Wrap(err, fmt.Sprintf("failed to open gitdir %s", gitDir))
		}
		target := string(data)
		target = strings.Trim(target, " \n\r")
		target = strings.TrimPrefix(target, "gitdir: ")
		if target == gitDir {
			return "", errors.New("gitdir file points to itself")
		}
		if target == "" {
			return "", errors.New("invalid gitdir format. gitdir points nowhere")
		}
		if filepath.IsAbs(target) {
			gitDir = target
		} else {
			gitDir = filepath.Join(gitDir, target)
		}
	}
}

func FindGitDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	path = abs
	for {
		gitDir, err := resolveGitDir(filepath.Join(path, ".git"))
		if os.IsNotExist(err) {
			path = filepath.Dir(path)
			if path == "/" || path == "" {
				return "", errors.New("unable to find git directory")
			}
			continue
		}
		if err != nil {
			return "", err
		}
		return gitDir, nil

	}
}
