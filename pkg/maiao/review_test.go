package maiao

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/adevinta/maiao/pkg/api"
	"github.com/adevinta/maiao/pkg/log"
	"github.com/adevinta/maiao/pkg/system"
	"github.com/spf13/afero"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testRepository struct {
	remote          func(name string) (*git.Remote, error)
	branches        func() (storer.ReferenceIter, error)
	config          func() (*config.Config, error)
	log             func(o *git.LogOptions) (object.CommitIter, error)
	resolveRevision func(rev plumbing.Revision) (*plumbing.Hash, error)
	fetch           func(o *git.FetchOptions) error
	push            func(o *git.PushOptions) error
	worktree        func() (*git.Worktree, error)
}

func (r *testRepository) Head() (*plumbing.Reference, error) {
	return nil, errors.New("not implemented")
}

func (r *testRepository) Remote(name string) (*git.Remote, error) {
	if r.config == nil {
		return nil, errors.New("not implemented")
	}
	return r.remote(name)
}

func (r *testRepository) Push(o *git.PushOptions) error {
	if r.config == nil {
		return errors.New("not implemented")
	}
	return r.push(o)
}

func (r *testRepository) Branches() (storer.ReferenceIter, error) {
	if r.config == nil {
		return nil, errors.New("not implemented")
	}
	return r.branches()
}

func (r *testRepository) Config() (*config.Config, error) {
	if r.config == nil {
		return nil, errors.New("not implemented")
	}
	return r.config()
}

func (r *testRepository) Fetch(o *git.FetchOptions) error {
	if r.fetch == nil {
		return errors.New("not implemented")
	}
	return r.fetch(o)
}

func (r *testRepository) Log(o *git.LogOptions) (object.CommitIter, error) {
	if r.log == nil {
		return nil, errors.New("not implemented")
	}
	return r.log(o)
}

func (r *testRepository) ResolveRevision(rev plumbing.Revision) (*plumbing.Hash, error) {
	if r.resolveRevision == nil {
		return nil, errors.New("not implemented")
	}
	return r.resolveRevision(rev)
}

func (r *testRepository) Worktree() (*git.Worktree, error) {
	if r.worktree == nil {
		return nil, errors.New("not implemented")
	}
	return r.worktree()
}

type testAPI struct {
	UpdateFunc              func(context.Context, *api.PullRequest, api.PullRequestOptions) (*api.PullRequest, error)
	EnsureFunc              func(context.Context, api.PullRequestOptions) (*api.PullRequest, bool, error)
	LinkedTopicIssuesFunc   func(topic string) string
	DefaultBranchFunc       func(context.Context) string
	UpdateCalled            int
	EnsureCalled            int
	LinkedTopicIssuesCalled int
	DefaultBranchCalled     int
}

// Update defines the interface to create or update a pull request to match options
func (a *testAPI) Update(ctx context.Context, pr *api.PullRequest, opts api.PullRequestOptions) (*api.PullRequest, error) {
	a.UpdateCalled++
	if a.UpdateFunc != nil {
		return a.UpdateFunc(ctx, pr, opts)
	}
	return nil, errors.New("Update not implemented")
}

// Ensure ensures one and only one pull request exists for the given head
func (a *testAPI) Ensure(ctx context.Context, opts api.PullRequestOptions) (*api.PullRequest, bool, error) {
	a.EnsureCalled++
	if a.EnsureFunc != nil {
		return a.EnsureFunc(ctx, opts)
	}
	return nil, false, errors.New("Ensure not implemented")
}
func (a *testAPI) LinkedTopicIssues(topic string) string {
	a.LinkedTopicIssuesCalled++
	if a.LinkedTopicIssuesFunc != nil {
		return a.LinkedTopicIssuesFunc(topic)
	}
	return "LinkedTopicIssues not implemented"
}
func (a *testAPI) DefaultBranch(ctx context.Context) string {
	a.DefaultBranchCalled++
	if a.DefaultBranchFunc != nil {
		return a.DefaultBranchFunc(ctx)
	}
	return "DefaultBranch not implemented"
}

func (a *testAPI) StackManager() api.StackManager {
	return nil
}

func (a *testAPI) BodyFormatter() api.BodyFormatter {
	return api.HTMLBodyFormatter{}
}

func TestDefaultOptionsUsesGitDefaults(t *testing.T) {
	opts := ReviewOptions{}
	repo := &testRepository{}
	defaultBranchOption(context.Background(), repo, nil, &opts)
	defaultRemoteOption(context.Background(), repo, &opts)
	assert.Equal(t, "master", opts.Branch)
	assert.Equal(t, "origin", opts.Remote)
}

func TestDefaultOptionsUsesGitRemoteDefaults(t *testing.T) {
	opts := ReviewOptions{}
	repo := &testRepository{}
	prAPI := testAPI{
		DefaultBranchFunc: func(ctx context.Context) string {
			return "maiao.main"
		},
	}
	defaultBranchOption(context.Background(), repo, &prAPI, &opts)
	defaultRemoteOption(context.Background(), repo, &opts)
	assert.Equal(t, "maiao.main", opts.Branch)
	assert.Equal(t, "origin", opts.Remote)

	assert.Equal(t, 1, prAPI.DefaultBranchCalled)
	assert.Equal(t, 0, prAPI.EnsureCalled)
	assert.Equal(t, 0, prAPI.UpdateCalled)
	assert.Equal(t, 0, prAPI.LinkedTopicIssuesCalled)
}

func TestDefaultOptionsUsesTrackingRemote(t *testing.T) {
	branch := uuid.New().String()
	remoteName := uuid.New().String()
	opts := ReviewOptions{
		Branch: branch,
	}
	repo := &testRepository{
		config: func() (*config.Config, error) {
			return &config.Config{
				Branches: map[string]*config.Branch{
					branch: {Remote: remoteName},
				},
			}, nil
		},
	}
	defaultBranchOption(context.Background(), repo, nil, &opts)
	defaultRemoteOption(context.Background(), repo, &opts)
	assert.Equal(t, branch, opts.Branch)
	assert.Equal(t, remoteName, opts.Remote)
}

func TestDefaultOptionsUsesGitDefaultRemoteNameWhenNotTracked(t *testing.T) {
	branch := uuid.New().String()
	opts := ReviewOptions{
		Branch: branch,
	}
	repo := &testRepository{
		config: func() (*config.Config, error) {
			return &config.Config{
				Branches: map[string]*config.Branch{
					branch: {},
				},
			}, nil
		},
	}
	defaultBranchOption(context.Background(), repo, nil, &opts)
	defaultRemoteOption(context.Background(), repo, &opts)
	assert.Equal(t, branch, opts.Branch)
	assert.Equal(t, "origin", opts.Remote)
}

func TestDefaultOptionsUsesGitDefaultRemoteNameWhenTheBranchIsNotFound(t *testing.T) {
	branch := uuid.New().String()
	opts := ReviewOptions{
		Branch: branch,
	}
	repo := &testRepository{
		config: func() (*config.Config, error) {
			return &config.Config{
				Branches: map[string]*config.Branch{},
			}, nil
		},
	}
	defaultBranchOption(context.Background(), repo, nil, &opts)
	defaultRemoteOption(context.Background(), repo, &opts)
	assert.Equal(t, branch, opts.Branch)
	assert.Equal(t, "origin", opts.Remote)
}

func TestDefaultOptionsUsesGitDefaultRemoteNameWhenTheBranchConfigIsNull(t *testing.T) {
	branch := uuid.New().String()
	opts := ReviewOptions{
		Branch: branch,
	}
	repo := &testRepository{
		config: func() (*config.Config, error) {
			return &config.Config{
				Branches: map[string]*config.Branch{
					branch: nil,
				},
			}, nil
		},
	}
	defaultBranchOption(context.Background(), repo, nil, &opts)
	defaultRemoteOption(context.Background(), repo, &opts)
	assert.Equal(t, branch, opts.Branch)
	assert.Equal(t, "origin", opts.Remote)
}

func TestRebaseTODO(t *testing.T) {
	assert.Equal(
		t,
		strings.Join([]string{}, "\n"),
		rebaseTODO([]*change{}),
	)
	assert.Equal(
		t,
		strings.Join([]string{
			"reword b34ccd81a342e155b8382992cddb116c56bee95c other-change",
			"pick c30a2f070b4f3d00c26679186345ea506e664056 fixup! other-change",
			"pick 943c8d8469c2800e361cea0f37a3e38cc7e90fd6 add hello world",
		}, "\n"),
		rebaseTODO([]*change{
			{
				commits: []*object.Commit{
					{Hash: plumbing.NewHash("b34ccd81a342e155b8382992cddb116c56bee95c"), Message: "other-change"},
					{Hash: plumbing.NewHash("c30a2f070b4f3d00c26679186345ea506e664056"), Message: "fixup! other-change"},
				},
			},
			{
				changeID: "1234",
				commits: []*object.Commit{
					{Hash: plumbing.NewHash("943c8d8469c2800e361cea0f37a3e38cc7e90fd6"), Message: "add hello world"},
				},
			},
			{},
		}),
	)
}

func TestRemoveMergedChangeIDs(t *testing.T) {
	assert.Equal(
		t,
		[]*change{
			{
				changeID: "5678",
				commits: []*object.Commit{
					{Hash: plumbing.NewHash("b34ccd81a342e155b8382992cddb116c56bee95c"), Message: "other-change"},
					{Hash: plumbing.NewHash("c30a2f070b4f3d00c26679186345ea506e664056"), Message: "fixup! other-change"},
				},
			},
			{
				commits: []*object.Commit{
					{Hash: plumbing.NewHash("943c8d8469c2800e361cea0f37a3e38cc7e90fd6"), Message: "add hello world"},
				},
			},
			{},
		},
		removeMergedChangeIDs(
			[]*change{
				{
					changeID: "1234",
					commits: []*object.Commit{
						{Hash: plumbing.NewHash("5b380f1b4081a7b64b72954a6ad58f12131749ed"), Message: "merged-change"},
					},
				},
				{
					changeID: "5678",
					commits: []*object.Commit{
						{Hash: plumbing.NewHash("b34ccd81a342e155b8382992cddb116c56bee95c"), Message: "other-change"},
						{Hash: plumbing.NewHash("c30a2f070b4f3d00c26679186345ea506e664056"), Message: "fixup! other-change"},
					},
				},
				{
					commits: []*object.Commit{
						{Hash: plumbing.NewHash("943c8d8469c2800e361cea0f37a3e38cc7e90fd6"), Message: "add hello world"},
					},
				},
				{},
			},
			map[string]struct{}{
				"1234": {},
			},
		),
	)
}

func gitCommand(t *testing.T, path string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = path
	out, err := cmd.CombinedOutput()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func TestExtractChanges(t *testing.T) {
	d, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	t.Cleanup(func() {
		os.RemoveAll(d)
	})
	fs := afero.NewBasePathFs(afero.NewOsFs(), d)

	gitCommand(t, d, "init")
	gitCommand(t, d, "config", "user.email", "john.doe@example.com")
	gitCommand(t, d, "config", "user.name", "John Doe")
	gitCommand(t, d, "commit", "--allow-empty", "-m", "Initial commit")
	initialCommit := gitCommand(t, d, "rev-parse", "HEAD")
	system.EnsureTestFileContent(t, fs, "code.txt", `
	func removeMergedChangeIDs(changes []*change, knownChangeIDs map[string]struct{}) []*change {
		filtered := []*change{}
		for _, change := range changes {
			if _, ok := knownChangeIDs[change.changeID]; !ok {
				filtered = append(filtered, change)
			}
		}
		return filtered
	}`)
	gitCommand(t, d, "add", "code.txt")
	gitCommand(t, d, "commit", "-m", "Code number one")
	system.EnsureTestFileContent(t, fs, "code.txt", `
	func removeMergedChangeIDs(changes []*change, knownChangeIDs map[string]struct{}) []*change {
		filtered := []*change{}
		for _, change := range changes {
		}
		return filtered
	}`)
	gitCommand(t, d, "add", "code.txt")
	gitCommand(t, d, "commit", "-m", "Code number two")
	system.EnsureTestFileContent(t, fs, "code.txt", `
	func removeMergedChangeIDs(changes []*change, knownChangeIDs map[string]struct{}) []*change {
		filtered := []*change{}
		for _, change := range changes {
			fmt.Println(change)
		}
		return filtered
	}`)
	gitCommand(t, d, "add", "code.txt")
	gitCommand(t, d, "commit", "-m", "Code number three", "-m", "ChangeId: I1234")
	fmt.Println(d)

	repo, err := git.PlainOpen(d)
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	b, err := repo.ResolveRevision(plumbing.Revision(initialCommit))
	require.NoError(t, err)
	changes, err := extractChanges(context.Background(), repo, *b, head.Hash())
	require.NoError(t, err)
	require.Len(t, changes, 3)
	assert.Equal(t, "Code number one", strings.TrimSpace(changes[0].commits[0].Message))
	assert.Equal(t, "Code number two", strings.TrimSpace(changes[1].commits[0].Message))
	assert.Equal(t, "Code number three\n\nChangeId: I1234", strings.TrimSpace(changes[2].commits[0].Message))
}

func TestNeedReview(t *testing.T) {
	storage := memory.NewStorage()
	rootParent := "bdc945b1bc57b3938f7223c7adb8bc2db58b838f"
	newTestCommit(t, storage, rootParent)
	assert.False(t, changesNeedRebase(context.Background(), []*change{}))
	t.Run("When change ID is missing", func(t *testing.T) {
		assert.True(t, changesNeedRebase(context.Background(), []*change{
			{},
		}))
	})
	t.Run("When change ID exists", func(t *testing.T) {
		assert.False(t, changesNeedRebase(context.Background(), []*change{
			{
				changeID: "changeID",
			},
		}))
	})
	t.Run("When fixups are in order", func(t *testing.T) {
		assert.False(t, changesNeedRebase(context.Background(), []*change{
			{
				changeID: "changeID",
				commits: []*object.Commit{
					newTestCommit(t, storage, "fc73d3a47b5864a8668eb826d506deb2bb54c1b5", rootParent),
					newTestCommit(t, storage, "2d885b9b60dd70bb5c9b66ac72d21da894787fd7", "fc73d3a47b5864a8668eb826d506deb2bb54c1b5"),
				},
			},
			{
				changeID: "changeID2",
				commits: []*object.Commit{
					newTestCommit(t, storage, "832e80c48ef021d64a6d38450584f0e7f6f333a2", "2d885b9b60dd70bb5c9b66ac72d21da894787fd7"),
					newTestCommit(t, storage, "b1b5506d096e9697ca5ad8fee28ce0415ff4bc0d", "832e80c48ef021d64a6d38450584f0e7f6f333a2"),
				},
			},
		}))
	})
	t.Run("When fixups are out of order", func(t *testing.T) {
		assert.True(t, changesNeedRebase(context.Background(), []*change{
			{
				changeID: "changeID",
				commits: []*object.Commit{
					newTestCommit(t, storage, "fc73d3a47b5864a8668eb826d506deb2bb54c1b5", rootParent),
					newTestCommit(t, storage, "2d885b9b60dd70bb5c9b66ac72d21da894787fd7", "fc73d3a47b5864a8668eb826d506deb2bb54c1b5"),
					newTestCommit(t, storage, "61809666bdb341715a6df144a1f84a6709975816", "b1b5506d096e9697ca5ad8fee28ce0415ff4bc0d"),
				},
			},
			{
				changeID: "changeID2",
				commits: []*object.Commit{
					newTestCommit(t, storage, "832e80c48ef021d64a6d38450584f0e7f6f333a2", "2d885b9b60dd70bb5c9b66ac72d21da894787fd7"),
					newTestCommit(t, storage, "b1b5506d096e9697ca5ad8fee28ce0415ff4bc0d", "832e80c48ef021d64a6d38450584f0e7f6f333a2"),
				},
			},
		}))
	})
	t.Run("When main commits are out of order", func(t *testing.T) {
		assert.True(t, changesNeedRebase(context.Background(), []*change{
			{
				changeID: "changeID",
				commits: []*object.Commit{
					newTestCommit(t, storage, "fc73d3a47b5864a8668eb826d506deb2bb54c1b5", "b1b5506d096e9697ca5ad8fee28ce0415ff4bc0d"),
					newTestCommit(t, storage, "2d885b9b60dd70bb5c9b66ac72d21da894787fd7", "fc73d3a47b5864a8668eb826d506deb2bb54c1b5"),
					newTestCommit(t, storage, "61809666bdb341715a6df144a1f84a6709975816", "2d885b9b60dd70bb5c9b66ac72d21da894787fd7"),
				},
			},
			{
				changeID: "changeID2",
				commits: []*object.Commit{
					newTestCommit(t, storage, "832e80c48ef021d64a6d38450584f0e7f6f333a2", rootParent),
					newTestCommit(t, storage, "b1b5506d096e9697ca5ad8fee28ce0415ff4bc0d", "832e80c48ef021d64a6d38450584f0e7f6f333a2"),
				},
			},
		}))
	})
	t.Run("When one parent is not found", func(t *testing.T) {
		storage := memory.NewStorage()
		assert.True(t, changesNeedRebase(context.Background(), []*change{
			{
				changeID: "changeID",
				commits: []*object.Commit{
					newTestCommit(t, storage, "832e80c48ef021d64a6d38450584f0e7f6f333a2", rootParent),
					newTestCommit(t, storage, "fc73d3a47b5864a8668eb826d506deb2bb54c1b5", "b1b5506d096e9697ca5ad8fee28ce0415ff4bc0d"),
				},
			},
		}))
	})
}

func newTestCommit(t *testing.T, storer storer.EncodedObjectStorer, sha string, parents ...string) *object.Commit {
	t.Helper()
	c := *&object.Commit{
		Hash: plumbing.NewHash(sha),
	}
	for _, parent := range parents {
		c.ParentHashes = append(c.ParentHashes, plumbing.NewHash(parent))
	}
	o := &testEncodedObject{
		sha:  sha,
		data: &bytes.Buffer{},
	}
	require.NoError(t, c.EncodeWithoutSignature(o))
	storer.SetEncodedObject(o)
	out, err := object.GetCommit(storer, plumbing.NewHash(sha))
	require.NoError(t, err)
	return out
}

type testEncodedObject struct {
	sha        string
	objectType plumbing.ObjectType
	data       *bytes.Buffer
	size       int64
}

func (t *testEncodedObject) Hash() plumbing.Hash {
	return plumbing.NewHash(t.sha)
}
func (t *testEncodedObject) Type() plumbing.ObjectType {
	return t.objectType
}
func (t *testEncodedObject) SetType(objectType plumbing.ObjectType) {
	t.objectType = objectType
}
func (t *testEncodedObject) Size() int64 {
	return t.size
}
func (t *testEncodedObject) SetSize(size int64) {
	t.size = size
}
func (t *testEncodedObject) Reader() (io.ReadCloser, error) {
	return ioutil.NopCloser(t.data), nil
}
func (t *testEncodedObject) Writer() (io.WriteCloser, error) {
	return &nopWriteCloser{Writer: t.data}, nil
}

type nopWriteCloser struct {
	io.Writer
}

func (n *nopWriteCloser) Close() error {
	return nil
}

type testStackManager struct {
	available            func(context.Context) bool
	createOrUpdateStack  func(context.Context, []int) (*api.Stack, error)
	getStack             func(context.Context, int) (*api.Stack, error)
	createOrUpdateCalled int
	lastPRNumbers        []int
}

func (m *testStackManager) Available(ctx context.Context) bool {
	if m.available == nil {
		return false
	}
	return m.available(ctx)
}

func (m *testStackManager) CreateOrUpdateStack(ctx context.Context, prNumbers []int) (*api.Stack, error) {
	m.createOrUpdateCalled++
	m.lastPRNumbers = prNumbers
	if m.createOrUpdateStack == nil {
		return nil, errors.New("CreateOrUpdateStack not implemented")
	}
	return m.createOrUpdateStack(ctx, prNumbers)
}

func (m *testStackManager) GetStack(ctx context.Context, prNumber int) (*api.Stack, error) {
	if m.getStack == nil {
		return nil, errors.New("GetStack not implemented")
	}
	return m.getStack(ctx, prNumber)
}

func (m *testStackManager) DeleteStack(ctx context.Context, stackID string) error {
	return nil
}

type testAPIWithStack struct {
	testAPI
	stackMgr api.StackManager
}

func (a *testAPIWithStack) StackManager() api.StackManager {
	return a.stackMgr
}

func stackTestRepo(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "stack-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(d) })
	gitCommand(t, d, "init")
	gitCommand(t, d, "config", "user.email", "test@test.com")
	gitCommand(t, d, "config", "user.name", "Test")
	gitCommand(t, d, "commit", "--allow-empty", "-m", "init")
	return d
}

func TestRegisterNativeStack_SkippedWhenStackFalse(t *testing.T) {
	mgr := &testStackManager{
		available: func(context.Context) bool { return true },
		createOrUpdateStack: func(_ context.Context, _ []int) (*api.Stack, error) {
			return &api.Stack{ID: "1", PRs: []int{1, 2}}, nil
		},
	}
	prAPI := &testAPIWithStack{stackMgr: mgr}
	changes := []*change{
		{pr: &api.PullRequest{ID: "1"}},
		{pr: &api.PullRequest{ID: "2"}},
	}
	registerNativeStack(context.Background(), prAPI, ReviewOptions{Stack: "false", RepoPath: stackTestRepo(t)}, changes)
	assert.Equal(t, 0, mgr.createOrUpdateCalled)
}

func TestRegisterNativeStack_SkippedWhenNotAvailableAndAuto(t *testing.T) {
	mgr := &testStackManager{
		available: func(context.Context) bool { return false },
	}
	prAPI := &testAPIWithStack{stackMgr: mgr}
	changes := []*change{
		{pr: &api.PullRequest{ID: "1"}},
		{pr: &api.PullRequest{ID: "2"}},
	}
	registerNativeStack(context.Background(), prAPI, ReviewOptions{Stack: "auto", RepoPath: stackTestRepo(t)}, changes)
	assert.Equal(t, 0, mgr.createOrUpdateCalled)
}

func TestRegisterNativeStack_CallsCreateOrUpdateWhenAvailable(t *testing.T) {
	mgr := &testStackManager{
		available: func(context.Context) bool { return true },
		createOrUpdateStack: func(_ context.Context, _ []int) (*api.Stack, error) {
			return &api.Stack{ID: "42", PRs: []int{10, 20, 30}}, nil
		},
	}
	prAPI := &testAPIWithStack{stackMgr: mgr}
	changes := []*change{
		{pr: &api.PullRequest{ID: "10"}},
		{pr: &api.PullRequest{ID: "20"}},
		{pr: &api.PullRequest{ID: "30"}},
	}
	registerNativeStack(context.Background(), prAPI, ReviewOptions{Stack: "auto", RepoPath: stackTestRepo(t)}, changes)
	assert.Equal(t, 1, mgr.createOrUpdateCalled)
	assert.Equal(t, []int{10, 20, 30}, mgr.lastPRNumbers)
}

func TestRegisterNativeStack_GracefulOnError(t *testing.T) {
	mgr := &testStackManager{
		available: func(context.Context) bool { return true },
		createOrUpdateStack: func(_ context.Context, _ []int) (*api.Stack, error) {
			return nil, errors.New("API error")
		},
	}
	prAPI := &testAPIWithStack{stackMgr: mgr}
	changes := []*change{
		{pr: &api.PullRequest{ID: "1"}},
		{pr: &api.PullRequest{ID: "2"}},
	}
	registerNativeStack(context.Background(), prAPI, ReviewOptions{Stack: "true", RepoPath: stackTestRepo(t)}, changes)
	assert.Equal(t, 1, mgr.createOrUpdateCalled)
}

func TestRegisterNativeStack_NilStackManagerWithAutoIsNoop(t *testing.T) {
	prAPI := &testAPI{}
	changes := []*change{
		{pr: &api.PullRequest{ID: "1"}},
		{pr: &api.PullRequest{ID: "2"}},
	}
	registerNativeStack(context.Background(), prAPI, ReviewOptions{Stack: "auto", RepoPath: stackTestRepo(t)}, changes)
}

func TestRegisterNativeStack_UsesCachedAvailability(t *testing.T) {
	d := stackTestRepo(t)
	gitCommand(t, d, "config", "maiao.stackApiAvailable", "true")
	gitCommand(t, d, "config", "maiao.stackApiCheckedAt", strconv.FormatInt(time.Now().Unix(), 10))

	availableCalled := false
	mgr := &testStackManager{
		available: func(context.Context) bool {
			availableCalled = true
			return true
		},
		createOrUpdateStack: func(_ context.Context, _ []int) (*api.Stack, error) {
			return &api.Stack{ID: "1", PRs: []int{1}}, nil
		},
	}
	prAPI := &testAPIWithStack{stackMgr: mgr}
	changes := []*change{
		{pr: &api.PullRequest{ID: "1"}},
	}
	registerNativeStack(context.Background(), prAPI, ReviewOptions{Stack: "auto", RepoPath: d}, changes)
	assert.False(t, availableCalled, "should use cached value, not call Available()")
	assert.Equal(t, 1, mgr.createOrUpdateCalled)
}

func init() {
	log.Logger.SetLevel(logrus.TraceLevel)
}
