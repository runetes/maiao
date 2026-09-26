package maiao

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/runetes/maiao/pkg/api"
	"github.com/runetes/maiao/pkg/credentials"
	lgit "github.com/runetes/maiao/pkg/git"
	"github.com/runetes/maiao/pkg/log"
	"github.com/runetes/maiao/pkg/provider"
	mssh "github.com/runetes/maiao/pkg/ssh"
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

func Review(ctx context.Context, repo lgit.Repository, options ReviewOptions) (*Result, error) {
	defaultRemoteOption(ctx, repo, &options)
	head, err := repo.Head()
	if err != nil {
		log.ForContext(ctx).WithError(err).Error("failed to retrieve git HEAD")
		return nil, err
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
		return nil, err
	}

	prAPI, err := pullRequesterFor(ctx, remote, options.RepoPath)
	if err != nil {
		return nil, err
	}
	defaultBranchOption(ctx, repo, prAPI, &options)

	remoteRef := plumbing.Revision(fmt.Sprintf("%s/%s", options.Remote, options.Branch))
	ctx = log.WithContextFields(ctx, logrus.Fields{
		"remoteRef": remoteRef,
	})

	if len(remote.Config().URLs) != 1 {
		return nil, errors.New("multiple URLs not supported")
	}

	endpoint, err := transport.NewEndpoint(remote.Config().URLs[0])
	if err != nil {
		return nil, err
	}

	providerType, err := provider.Detect(endpoint.Host, options.RepoPath)
	if err != nil {
		return nil, err
	}
	credGetter := credentials.CredentialGetterForProvider(string(providerType))

	log.ForContext(ctx).Debugf("fetching remote")
	fetchOpts := &git.FetchOptions{
		RemoteName: options.Remote,
		Auth:       &credentials.GitAuth{Credentials: credGetter, Endpoint: endpoint},
	}
	err = retryAfterHostKeyFix(endpoint.Host, func() error { return remote.Fetch(fetchOpts) })
	if err != nil && err != git.NoErrAlreadyUpToDate {
		log.ForContext(ctx).WithError(err).Error("failed to update git repository")
		return nil, err
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
		return nil, err
	}
	remoteCommit, err := repo.ResolveRevision(plumbing.Revision(remoteRef))
	if err != nil {
		return nil, err
	}

	needRebase := remoteCommit.String() != b.String()

	if !needRebase {
		// we also need to rebase if some changeIDs are missing
		changes, err := extractChanges(ctx, repo, b, head.Hash())
		if err != nil {
			return nil, err
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
			return nil, err
		}
		// The pull requests are created by the maiao run that git rebase invokes as
		// its final todo step, so this process has no result of its own.
		return &Result{}, nil
	} else {
		log.ForContext(ctx).WithField("mergeSha", remoteCommit.String()).WithField("baseSha", b.String()).Debug("no rebase needed")
	}

	if b == head.Hash() {
		fmt.Fprintln(os.Stderr, "nothing to review")
		return &Result{}, nil
	}

	return sendPrs(ctx, repo, options, b, head.Hash())
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
		fmt.Fprintln(os.Stderr, "nothing to review")
		return nil
	}

	if err := rebase(ctx, repo, base, remoteHead, rebaseTODO(changes)); err != nil {
		// Reporting success here said the review was done when nothing had been
		// submitted: git stops the rebase where it is, and the step that creates the
		// reviews is the last one in the todo list, so it never ran.
		return fmt.Errorf("%w: resolve it and run `git rebase --continue`: %w", ErrRebaseIncomplete, err)
	}
	return nil
}

// rebase is a seam so that tests do not need to provoke a real conflict.
var rebase = lgit.RebaseCommits

// parkReorderedPullRequests moves every pull request whose base branch is about to
// change onto the branch the stack is based on, and reports which ones it moved.
//
// It has to run before the branches are pushed. A provider marks a pull request
// merged as soon as its head commits are contained in its base branch, and
// reordering commits locally does exactly that: the branch a pull request still
// names as its base is force-pushed to a tip that now contains that pull request's
// own head. The review is closed as merged before anything gets to correct the
// base, and no later run recovers it: a provider refuses to reopen a pull request it
// has called merged, whether or not anything was merged. This pass is the only
// defence, which is why it runs before the push rather than repairing afterwards.
//
// The branch the stack is based on is the one place with no such overlap: it is
// behind every change in the stack, so it can contain no head in it. Pointing at
// it costs a write and leaves the review showing the whole stack as its own diff,
// so only the pull requests whose base actually moves are parked. The base each
// one ends up on is set after the push, by the pass that writes the descriptions.
func parkReorderedPullRequests(ctx context.Context, repo lgit.Repository, prAPI api.PullRequester, options ReviewOptions, changes []*change) ([]string, error) {
	parked := []string{}
	toPark := []*change{}
	for _, change := range changes {
		pr, err := prAPI.Find(ctx, change.branch)
		if err != nil {
			return parked, err
		}
		if pr == nil {
			continue
		}
		// Only an open pull request is parked, and only an open one is carried into the
		// rest of the review.
		//
		// A closed one cannot be closed again by the push, so it needs no protecting.
		// What becomes of it is the provider's to decide, after the push: most reopen it
		// on the base the change belongs behind, and Bitbucket cannot reopen anything at
		// all, so there the change gets a new pull request.
		//
		// A merged one can be neither moved nor reopened on any provider, so the change
		// needs a new pull request — and nothing else in the review would say where the
		// conversation went.
		switch pr.State {
		case api.PullRequestMerged:
			fmt.Fprintf(os.Stderr,
				"%s was closed as merged by the provider and cannot be reopened. Opening a new pull request for this change; the review conversation stays on %s\n",
				pr.URL, pr.URL)
			continue
		case api.PullRequestClosed:
			continue
		}
		// Reused below rather than asking the provider for the same pull request a
		// second time through Ensure.
		change.pr = pr
		// An empty base is a provider that did not report one, which is not the same
		// as one that has not moved. Parking anyway costs a write; not parking costs
		// a review nobody can reopen.
		if pr.Base != "" && pr.Base == baseBranch(options, change) {
			continue
		}
		toPark = append(toPark, change)
	}
	if len(toPark) == 0 {
		return parked, nil
	}
	dissolveStacks(ctx, prAPI, options, toPark)

	for _, change := range toPark {
		i := indexOfChange(changes, change)
		pr := change.pr
		log.ForContext(ctx).
			WithField("pullRequest", pr.ID).
			WithField("from", pr.Base).
			Debug("parking a reordered pull request on the base branch")
		opts := prOptions(repo, prAPI, options, change, changes[:i], changes[i+1:])
		opts.Base = options.Branch
		// The pass after the push marks the review ready. Asking twice asks the
		// provider to take a pull request out of draft that is already out of it.
		opts.Ready = false
		moved, err := prAPI.Update(ctx, pr, opts)
		if err != nil {
			return parked, err
		}
		// A provider can refuse to move a base and still answer the edit as a success:
		// GitHub does exactly that for a pull request its native stacks own. Taking its
		// word for it and pushing anyway is what closes the review, so the base it
		// reports back is what counts. An empty one is a provider that reports no base
		// at all, which says nothing either way.
		if moved != nil && moved.Base != "" && moved.Base != options.Branch {
			return parked, fmt.Errorf("%w: %s still targets %s", ErrBaseNotMoved, pr.URL, moved.Base)
		}
		parked = append(parked, pr.URL)
	}
	return parked, nil
}

// dissolveStacks takes the pull requests that must be parked out of any native
// stack holding them, because a provider that owns a stack owns the base branches
// in it and refuses to move them.
//
// GitHub offers no way to remove one pull request from a stack, and none to
// reorder one, so dissolving the whole stack is the only way out — which is also
// what its own documentation tells people to do before restructuring one. The
// stack is registered again at the end of the review, in the order the commits are
// now in, which is what the reorder was asking for in the first place.
//
// Every failure here is a warning rather than an error: the review can still go
// ahead, and if the base really could not be moved the parking pass says so and
// stops before anything is pushed.
func dissolveStacks(ctx context.Context, prAPI api.PullRequester, options ReviewOptions, toPark []*change) {
	if options.Stack == "false" {
		return
	}
	stackMgr := prAPI.StackManager()
	if stackMgr == nil {
		return
	}
	dissolved := map[string]struct{}{}
	for _, change := range toPark {
		number, err := strconv.Atoi(change.pr.ID)
		if err != nil {
			continue
		}
		stack, err := stackMgr.GetStack(ctx, number)
		if err != nil || stack == nil {
			continue
		}
		if _, done := dissolved[stack.ID]; done {
			continue
		}
		dissolved[stack.ID] = struct{}{}
		log.ForContext(ctx).
			WithField("stackID", stack.ID).
			Info("dissolving the native stack so the reordered pull requests can be moved off their base")
		if err := stackMgr.Unstack(ctx, stack.ID); err != nil {
			log.ForContext(ctx).WithError(err).Warn("failed to dissolve the native stack")
		}
	}
}

// indexOfChange reports where a change sits in the stack, which is what decides
// the parents and futures its description lists.
func indexOfChange(changes []*change, target *change) int {
	for i, c := range changes {
		if c == target {
			return i
		}
	}
	return 0
}

// parkingNote adds to err the pull requests left parked on the base branch.
//
// Nothing else mentions them, and the user did not put the repository in that
// state: those reviews show their whole stack as their own diff until a run gets
// far enough to set their base again.
func parkingNote(err error, parked []string, branch string) error {
	if len(parked) == 0 {
		return err
	}
	return fmt.Errorf("%w\n%s left targeting %s: run `git review` again to restore their base",
		err, strings.Join(parked, ", "), branch)
}

// baseBranch is the branch a change's pull request targets: the one below it in
// the stack, or the branch the whole stack is based on.
func baseBranch(options ReviewOptions, c *change) string {
	if c.parent != nil && c.parent.branch != "" {
		return c.parent.branch
	}
	return options.Branch
}

func sendPrs(ctx context.Context, repo lgit.Repository, options ReviewOptions, base, head plumbing.Hash) (*Result, error) {

	remote, err := repo.Remote(options.Remote)
	if err != nil {
		return nil, err
	}

	changes, err := extractChanges(ctx, repo, base, head)
	if err != nil {
		return nil, err
	}

	for _, change := range changes {
		if len(change.commits) == 0 {
			return nil, errors.New("empty change")
		}
	}

	if len(remote.Config().URLs) != 1 {
		return nil, errors.New("multiple URLs not supported")
	}

	endpoint, err := transport.NewEndpoint(remote.Config().URLs[0])
	if err != nil {
		return nil, err
	}

	providerType, err := provider.Detect(endpoint.Host, options.RepoPath)
	if err != nil {
		return nil, err
	}
	credGetter := credentials.CredentialGetterForProvider(string(providerType))

	prAPI, err := pullRequesterFor(ctx, remote, options.RepoPath)
	if err != nil {
		return nil, err
	}

	var parent *change
	for _, change := range changes {
		change.parent = parent
		parent = change
	}

	parked, err := parkReorderedPullRequests(ctx, repo, prAPI, options, changes)
	if err != nil {
		// Nothing has been pushed, so this run has submitted nothing to report.
		return nil, parkingNote(err, parked, options.Branch)
	}

	refspecs := []config.RefSpec{}
	for _, change := range changes {
		refspecs = append(refspecs, config.RefSpec(change.head.Hash.String()+":refs/heads/"+change.branch))
	}
	log.ForContext(ctx).WithField("refspec", refspecs).Debugf("pushing PR changes")
	pushOpts := &git.PushOptions{
		RemoteName: options.Remote,
		RefSpecs:   refspecs,
		Auth:       &credentials.GitAuth{Credentials: credGetter, Endpoint: endpoint},
		Force:      true,
	}
	err = retryAfterHostKeyFix(endpoint.Host, func() error { return repo.Push(pushOpts) })
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return nil, parkingNote(err, parked, options.Branch)
	}

	for i, change := range changes {
		if change.pr != nil {
			continue
		}
		opts := prOptions(repo, prAPI, options, change, changes[:i], changes[i+1:])
		pr, created, err := prAPI.Ensure(ctx, opts)
		if err != nil {
			// The pull requests earlier in the stack exist on the remote already, so
			// reporting nothing would describe this as a run that did nothing. A caller
			// retrying from there could not tell whether it was resuming or starting over.
			return newResult(changes), err
		}
		if created {
			fmt.Fprintf(os.Stderr, "created PR %s\n", pr.URL)
		}
		change.pr = pr
		change.created = created
	}
	for i, change := range changes {
		opts := prOptions(repo, prAPI, options, change, changes[:i], changes[i+1:])
		_, err := prAPI.Update(ctx, change.pr, opts)
		if err != nil {
			// Every pull request exists by now, so all of them are reported even though
			// the ones after this may still be missing a description or a base branch.
			return newResult(changes), err
		}
		if !change.created {
			fmt.Fprintf(os.Stderr, "updated PR %s\n", change.pr.URL)
		}
		log.ForContext(ctx).WithFields(logrus.Fields{"prOptions": opts, "change": change}).Trace("PR has been updated with parent ")
	}

	result := newResult(changes)
	if len(changes) > 1 {
		result.StackID = registerNativeStack(ctx, prAPI, options, changes)
	}

	return result, nil
}

// registerNativeStack returns the identifier the provider gave the stack, or an
// empty string when there is none to report.
//
// Every failure here is a warning rather than an error: the pull requests are
// already stacked by their base branches, and the native stack is an extra the
// provider may or may not offer.
func registerNativeStack(ctx context.Context, prAPI api.PullRequester, options ReviewOptions, changes []*change) string {
	if options.Stack == "false" {
		return ""
	}

	stackMgr := prAPI.StackManager()
	if stackMgr == nil {
		if options.Stack == "true" {
			log.ForContext(ctx).Warn("native stacks requested but not supported by this GitHub instance")
		}
		return ""
	}

	if !stackAvailable(ctx, stackMgr, options, options.RepoPath) {
		return ""
	}

	prNumbers := make([]int, 0, len(changes))
	for _, change := range changes {
		id, err := strconv.Atoi(change.pr.ID)
		if err != nil {
			log.ForContext(ctx).WithError(err).Warn("failed to parse PR number for stack registration")
			return ""
		}
		prNumbers = append(prNumbers, id)
	}

	stack, err := stackMgr.CreateOrUpdateStack(ctx, prNumbers)
	if err != nil {
		log.ForContext(ctx).WithError(err).Warn("failed to register native stack")
		return ""
	}
	log.ForContext(ctx).WithField("stackID", stack.ID).WithField("prCount", len(stack.PRs)).Debug("registered native stack")
	return stack.ID
}

const stackCacheTTL = 24 * time.Hour

func stackAvailable(ctx context.Context, stackMgr api.StackManager, options ReviewOptions, repoPath string) bool {
	cached, cachedAt := readStackAvailabilityCache(repoPath)
	if cachedAt != nil && time.Since(*cachedAt) < stackCacheTTL {
		log.ForContext(ctx).Debug("using cached stack API availability")
		return cached
	}

	available := stackMgr.Available(ctx)
	writeStackAvailabilityCache(repoPath, available)

	if !available && options.Stack == "true" {
		log.ForContext(ctx).Warn("native stacks requested but not available on this GitHub instance")
	}
	return available
}

func readStackAvailabilityCache(repoPath string) (available bool, checkedAt *time.Time) {
	cmd := exec.Command("git", "config", "--local", "maiao.stackApiCheckedAt")
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	out, err := cmd.Output()
	if err != nil {
		return false, nil
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return false, nil
	}
	t := time.Unix(ts, 0)

	cmd = exec.Command("git", "config", "--local", "maiao.stackApiAvailable")
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	out, err = cmd.Output()
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(string(out)) == "true", &t
}

func writeStackAvailabilityCache(repoPath string, available bool) {
	val := "false"
	if available {
		val = "true"
	}
	cmd := exec.Command("git", "config", "--local", "maiao.stackApiAvailable", val)
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	cmd.Run()
	cmd = exec.Command("git", "config", "--local", "maiao.stackApiCheckedAt", strconv.FormatInt(time.Now().Unix(), 10))
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	cmd.Run()
}

func defaultBranchOption(ctx context.Context, repo lgit.Repository, prAPI api.PullRequester, options *ReviewOptions) {
	if options.Branch == "" {
		cfg, err := repo.Config()
		if prAPI != nil {
			options.Branch = prAPI.DefaultBranch(ctx)
		}
		if options.Branch == "" {
			options.Branch = remoteDefaultBranch(repo, options.Remote)
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

func remoteDefaultBranch(repo lgit.Repository, remoteName string) string {
	if remoteName == "" {
		remoteName = defaultRemote
	}
	wt, err := repo.Worktree()
	if err != nil {
		return ""
	}
	root := wt.Filesystem.Root()
	out, err := exec.Command("git", "-C", root, "symbolic-ref", fmt.Sprintf("refs/remotes/%s/HEAD", remoteName)).Output()
	if err != nil {
		exec.Command("git", "-C", root, "remote", "set-head", remoteName, "--auto").Run()
		out, err = exec.Command("git", "-C", root, "symbolic-ref", fmt.Sprintf("refs/remotes/%s/HEAD", remoteName)).Output()
		if err != nil {
			return ""
		}
	}
	ref := strings.TrimSpace(string(out))
	prefix := fmt.Sprintf("refs/remotes/%s/", remoteName)
	return strings.TrimPrefix(ref, prefix)
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

// retryAfterHostKeyFix runs op, and when it fails over an SSH host key, tries to
// resolve that and runs op once more.
//
// A fix that does not succeed replaces op's error, because it is the actionable
// one: go-git reports only that the key did not match, while the fix attempt says
// what to do about it, and in batch mode that message is the entire diagnostic.
// Dropping it left the caller with nothing to act on.
func retryAfterHostKeyFix(host string, op func() error) error {
	err := op()
	if err == nil || err == git.NoErrAlreadyUpToDate {
		return err
	}
	if fixErr := fixHostKey(err, host); fixErr != nil {
		return fixErr
	}
	return op()
}

// fixHostKey is a seam so that tests can exercise the retry without a host they can
// actually reach.
var fixHostKey = handleSSHHostKeyError

// handleSSHHostKeyError returns err unchanged when it is not about a host key, so
// that only host key problems are given a second attempt.
func handleSSHHostKeyError(err error, endpointHost string) error {
	host, isMismatch, ok := mssh.IsKnownHostsError(err)
	if !ok {
		return err
	}
	if host == "" {
		host = endpointHost
	}
	return mssh.PromptAndFix(host, isMismatch)
}
