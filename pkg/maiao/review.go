package maiao

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/adevinta/maiao/pkg/api"
	"github.com/adevinta/maiao/pkg/credentials"
	lgit "github.com/adevinta/maiao/pkg/git"
	gh "github.com/adevinta/maiao/pkg/github"
	"github.com/adevinta/maiao/pkg/log"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/sirupsen/logrus"
)

const (
	defaultRemote = "origin"
)

type ReviewOptions struct {
	RepoPath       string
	Remote         string
	Branch         string
	SkipRebase     bool
	Topic          string
	WorkInProgress bool
	Ready          bool
	Stack          string
}

type change struct {
	created  bool
	commits  []*object.Commit
	head     *object.Commit
	branch   string
	message  *lgit.Message
	changeID string
	pr       *api.PullRequest
	parent   *change
}

func Review(ctx context.Context, repo lgit.Repository, options ReviewOptions) error {
	defaultRemoteOption(ctx, repo, &options)
	head, err := repo.Head()
	if err != nil {
		log.ForContext(ctx).WithError(err).Error("failed to retrieve git HEAD")
		return err
	}

	ctx = log.WithContextFields(ctx, logrus.Fields{
		"remote":     options.Remote,
		"topic":      options.Topic,
		"branch":     options.Branch,
		"skipRebase": options.SkipRebase,
		"headRef":    head.Name().String(),
		"headSHA":    head.Hash().String(),
	})

	log.ForContext(ctx).Debugf("finding remote")
	remote, err := repo.Remote(options.Remote)
	if err != nil {
		log.ForContext(ctx).WithError(err).Error("failed to find remote")
		return err
	}

	prAPI, err := api.NewPullRequester(ctx, remote)
	if err != nil {
		return err
	}
	defaultBranchOption(ctx, repo, prAPI, &options)

	remoteRef := plumbing.Revision(fmt.Sprintf("%s/%s", options.Remote, options.Branch))
	ctx = log.WithContextFields(ctx, logrus.Fields{
		"remoteRef": remoteRef,
	})

	if len(remote.Config().URLs) != 1 {
		return errors.New("multiple URLs not supported")
	}

	endpoint, err := transport.NewEndpoint(remote.Config().URLs[0])
	if err != nil {
		return err
	}

	log.ForContext(ctx).Debugf("fetching remote")
	err = remote.Fetch(&git.FetchOptions{
		RemoteName: options.Remote,
		Auth:       &credentials.GitAuth{Credentials: gh.DefaultCredentialGetter, Endpoint: endpoint},
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		log.ForContext(ctx).WithError(err).Error("failed to update git repository")
		return err
	}
	headRef := plumbing.Revision(plumbing.HEAD)
	ctx = log.WithContextFields(ctx, logrus.Fields{
		"remoteRef": remoteRef,
		"headRef":   headRef,
	})
	log.ForContext(ctx).Debugf("finding first common ancestor")
	b, err := lgit.MergeBase(ctx, repo, remoteRef, headRef)
	if err != nil {
		log.ForContext(ctx).WithError(err).Errorf("unable to find common ancestor")
		return err
	}
	remoteCommit, err := repo.ResolveRevision(plumbing.Revision(remoteRef))
	if err != nil {
		return err
	}

	needRebase := remoteCommit.String() != b.String()

	if !needRebase {
		// we also need to rebase if some changeIDs are missing
		changes, err := extractChanges(ctx, repo, b, head.Hash())
		if err != nil {
			return err
		}
		needRebase = changesNeedRebase(ctx, changes)
	}

	if !options.SkipRebase && needRebase {
		ctx := log.WithContextFields(ctx, logrus.Fields{
			"remoteSha": remoteCommit.String(),
			"baseSha":   b.String(),
		})
		log.ForContext(ctx).Debug("local branch is not up to date, needs rebasing")
		err := rebaseCommits(ctx, repo, options, b, *remoteCommit, head.Hash())
		if err != nil {
			return err
		}
		return nil
	} else {
		log.ForContext(ctx).WithField("mergeSha", remoteCommit.String()).WithField("baseSha", b.String()).Debug("no rebase needed")
	}

	if b == head.Hash() {
		fmt.Println("nothing to review")
		return nil
	}

	err = sendPrs(ctx, repo, options, b, head.Hash())
	if err != nil {
		return err
	}

	return nil
}

func changesNeedRebase(ctx context.Context, changes []*change) bool {
	var parent *object.Commit
	for _, change := range changes {
		if change.changeID == "" {
			log.ForContext(ctx).Debugf("missing change ID")
			return true
		}
		for _, commit := range change.commits {
			if parent != nil {
				commitParent, err := commit.Parent(0)
				if err != nil {
					log.ForContext(ctx).
						WithField("parent", parent.Hash.String()).
						WithField("commit", commit.Hash.String()).
						WithError(err).
						Debugf("unable to get first commit parent")
					return true
				}
				if commitParent.Hash.String() != parent.Hash.String() {
					log.ForContext(ctx).
						WithField("parent", parent.Hash.String()).
						WithField("candidate", commitParent.Hash.String()).
						WithField("commit", commit.Hash.String()).
						Debugf("commit history change detected")
					return true
				} else {
					log.ForContext(ctx).
						WithField("parent", parent.Hash.String()).
						WithField("candidate", commitParent.Hash.String()).
						WithField("commit", commit.Hash.String()).
						Debugf("commit parent matches expected order")
				}
			}
			parent = commit
		}
	}
	log.ForContext(ctx).Debugf("no change needing rebase detected")
	return false
}

func rebaseCommits(ctx context.Context, repo lgit.Repository, options ReviewOptions, base, remoteHead, head plumbing.Hash) error {

	changes, err := extractChanges(ctx, repo, base, head)
	if err != nil {
		return err
	}
	knownChangeIDs, err := extractChangeIDs(ctx, repo, base, remoteHead)
	if err != nil {
		return err
	}
	changes = removeMergedChangeIDs(changes, knownChangeIDs)

	if len(changes) == 0 {
		fmt.Println("nothing to review")
		return nil
	}

	err = lgit.RebaseCommits(ctx, repo, base, remoteHead, rebaseTODO(changes))
	if err != nil {
		return nil
	}
	return nil
}

func sendPrs(ctx context.Context, repo lgit.Repository, options ReviewOptions, base, head plumbing.Hash) error {

	remote, err := repo.Remote(options.Remote)
	if err != nil {
		return err
	}

	changes, err := extractChanges(ctx, repo, base, head)
	if err != nil {
		return err
	}

	refspecs := []config.RefSpec{}
	for _, change := range changes {
		if len(change.commits) == 0 {
			return errors.New("empty change")
		}
		refspecs = append(refspecs, config.RefSpec(change.head.Hash.String()+":refs/heads/"+change.branch))
	}

	if len(remote.Config().URLs) != 1 {
		return errors.New("multiple URLs not supported")
	}

	endpoint, err := transport.NewEndpoint(remote.Config().URLs[0])
	if err != nil {
		return err
	}

	log.ForContext(ctx).WithField("refspec", refspecs).Debugf("pushing PR changes")
	err = repo.Push(&git.PushOptions{
		RemoteName: options.Remote,
		RefSpecs:   refspecs,
		Auth:       &credentials.GitAuth{Credentials: gh.DefaultCredentialGetter, Endpoint: endpoint},
		Force:      true,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return err
	}

	prAPI, err := api.NewPullRequester(ctx, remote)
	if err != nil {
		return err
	}

	var parent *change
	for i, change := range changes {
		change.parent = parent
		opts := prOptions(repo, prAPI, options, change, changes[:i], changes[i+1:])
		pr, created, err := prAPI.Ensure(ctx, opts)
		if err != nil {
			return err
		}
		if created {
			fmt.Println(fmt.Sprintf("created PR %s", pr.URL))
		}
		change.pr = pr
		change.created = created
		parent = change
	}
	for i, change := range changes {
		opts := prOptions(repo, prAPI, options, change, changes[:i], changes[i+1:])
		_, err := prAPI.Update(ctx, change.pr, opts)
		if err != nil {
			return err
		}
		if !change.created {
			fmt.Println(fmt.Sprintf("updated PR %s", change.pr.URL))
		}
		log.ForContext(ctx).WithFields(logrus.Fields{"prOptions": opts, "change": change}).Trace("PR has been updated with parent ")
	}

	if len(changes) > 1 {
		registerNativeStack(ctx, prAPI, options, changes)
	}

	return nil
}

func registerNativeStack(ctx context.Context, prAPI api.PullRequester, options ReviewOptions, changes []*change) {
	if options.Stack == "false" {
		return
	}

	stackMgr := prAPI.StackManager()
	if stackMgr == nil {
		if options.Stack == "true" {
			log.ForContext(ctx).Warn("native stacks requested but not supported by this GitHub instance")
		}
		return
	}

	if !stackMgr.Available(ctx) {
		if options.Stack == "true" {
			log.ForContext(ctx).Warn("native stacks requested but not available on this GitHub instance")
		}
		return
	}

	prNumbers := make([]int, 0, len(changes))
	for _, change := range changes {
		id, err := strconv.Atoi(change.pr.ID)
		if err != nil {
			log.ForContext(ctx).WithError(err).Warn("failed to parse PR number for stack registration")
			return
		}
		prNumbers = append(prNumbers, id)
	}

	stack, err := stackMgr.CreateOrUpdateStack(ctx, prNumbers)
	if err != nil {
		log.ForContext(ctx).WithError(err).Warn("failed to register native stack")
		return
	}
	log.ForContext(ctx).WithField("stackID", stack.ID).WithField("prCount", len(stack.PRs)).Debug("registered native stack")
}

func defaultBranchOption(ctx context.Context, repo lgit.Repository, prAPI api.PullRequester, options *ReviewOptions) {
	if options.Branch == "" {
		cfg, err := repo.Config()
		if prAPI != nil {
			options.Branch = prAPI.DefaultBranch(ctx)
		}
		if options.Branch == "" {
			options.Branch = "master"
		}
		if err != nil {
			log.ForContext(ctx).WithError(err).Infof(`unable to load git config, using "%s" default branch`, options.Branch)
		} else {
			if cfg.Init.DefaultBranch != "" {
				log.ForContext(ctx).Debugf(`using default "%s" branch from git confguration`, cfg.Init.DefaultBranch)
				options.Branch = cfg.Init.DefaultBranch
			} else {

				log.ForContext(ctx).Debugf(`using default "%s" branch`, options.Branch)
			}
		}
	}
}

func defaultRemoteOption(ctx context.Context, repo lgit.Repository, options *ReviewOptions) {
	if options.Remote == "" {
		log.ForContext(ctx).Debugf("finding relevant remote")
		options.Remote = "origin"
		cfg, err := repo.Config()
		if err == nil {
			branchConfig, ok := cfg.Branches[options.Branch]
			if ok && branchConfig != nil && branchConfig.Remote != "" {
				log.ForContext(ctx).WithField("branch", branchConfig.Name).WithField("remote", branchConfig.Remote).Debugf("found relevant remote")
				options.Remote = branchConfig.Remote
			} else {
				log.ForContext(ctx).WithField("branch", options.Branch).Debugf(`dig not find tracking remote. Using default "origin"`)
			}
		} else {
			log.ForContext(ctx).WithError(err).Debugf(`failed to load config, using default "origin"`)
		}
	}
}

func rebaseTODO(changes []*change) string {
	lines := []string{}

	for _, change := range changes {
		for i, commit := range change.commits {
			action := "pick"
			if i == 0 && change.changeID == "" {
				action = "reword"
			}
			lines = append(lines, fmt.Sprint(action, " ", commit.Hash.String(), " ", strings.Split(commit.Message, "\n")[0]))
		}
	}
	return strings.Join(lines, "\n")
}

func removeMergedChangeIDs(changes []*change, knownChangeIDs map[string]struct{}) []*change {
	filtered := []*change{}
	for _, change := range changes {
		if _, ok := knownChangeIDs[change.changeID]; !ok {
			filtered = append(filtered, change)
		}
	}
	return filtered
}

func extractChangeIDs(ctx context.Context, repo lgit.Repository, base, head plumbing.Hash) (map[string]struct{}, error) {
	changeIDs := map[string]struct{}{}
	commitIter, err := repo.Log(&git.LogOptions{
		From: head,
	})
	if err != nil {
		return nil, err
	}
	for {
		c, err := commitIter.Next()
		if err != nil {
			return nil, err
		}
		if c.Hash.String() == base.String() {
			return changeIDs, nil
		}
		if changeID, ok := lgit.Parse(c.Message).GetChangeID(); ok {
			changeIDs[changeID] = struct{}{}
		}
	}
}

func extractChanges(ctx context.Context, repo lgit.Repository, base, head plumbing.Hash) ([]*change, error) {
	commitIter, err := repo.Log(&git.LogOptions{
		From: head,
	})
	if err != nil {
		return nil, err
	}
	fixupCommits := map[string][]*object.Commit{}

	changes := []*change{}

	for {
		c, err := commitIter.Next()
		if err != nil {
			return nil, err
		}
		if c.Hash.String() == base.String() {
			if len(fixupCommits) != 0 {
				return nil, errors.New("unmatched fixups")
			}
			return changes, nil
		}
		if len(c.ParentHashes) > 1 {
			// multiple parents not supported
			return nil, errors.New("merge commits are not supported in the review workflow")
		}
		message := lgit.Parse(c.Message)
		if message.IsFixup() {
			fixupCommits[message.GetTitle()] = append([]*object.Commit{c}, fixupCommits[message.GetTitle()]...)
		} else {
			changeCommits := []*object.Commit{c}
			if fixups, ok := fixupCommits[message.GetTitle()]; ok {
				delete(fixupCommits, message.GetTitle())
				changeCommits = append(changeCommits, fixups...)
			}
			changeID, ok := message.GetChangeID()
			if !ok {
				changes = append([]*change{{
					commits: changeCommits,
					head:    changeCommits[len(changeCommits)-1],
					message: message,
				}}, changes...)

			} else {
				changes = append([]*change{{
					commits:  changeCommits,
					head:     changeCommits[len(changeCommits)-1],
					changeID: changeID,
					message:  message,
					branch:   "maiao." + changeID,
				}}, changes...)
			}
		}
	}
}
