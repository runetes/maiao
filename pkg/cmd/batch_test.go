package cmd

import (
	"testing"

	lgit "github.com/adevinta/maiao/pkg/git"
	"github.com/adevinta/maiao/pkg/prompt"
	mssh "github.com/adevinta/maiao/pkg/ssh"
	"github.com/adevinta/maiao/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureCommitMsgHookInBatchModeFails is the behaviour this commit exists to
// change.
//
// Previously the unanswerable question resolved to "no", so maiao printed
// instructions and returned success without reviewing anything. A caller checking
// the exit status could not tell that apart from a run with nothing to submit,
// which is the worst possible outcome: silent and plausible.
func TestEnsureCommitMsgHookInBatchModeFails(t *testing.T) {
	testutil.IsolateHome(t)
	setBatch(t, true)
	repo := testutil.InitRepo(t)
	gitDir, err := lgit.FindGitDir(repo)
	require.NoError(t, err)
	stubConfirm(t, func(string) bool {
		t.Error("must not prompt in batch mode")
		return false
	})
	expectNoInstall(t)

	proceed, err := ensureCommitMsgHook(repo, gitDir)

	require.Error(t, err, "batch mode must fail rather than report success having done nothing")
	assert.ErrorIs(t, err, prompt.ErrNoInput)
	assert.False(t, proceed)
	assert.Contains(t, err.Error(), "git review install --global", "the message must name the setting that avoids the question")
	assert.Contains(t, err.Error(), lgit.HookPath(gitDir, lgit.CommitMsgHook), "the message must say where the hook was expected")
}

// TestEnsureCommitMsgHookInBatchModeSucceedsWhenOptedIn checks the pairing that
// makes batch mode usable: opting in globally removes the failure entirely.
func TestEnsureCommitMsgHookInBatchModeSucceedsWhenOptedIn(t *testing.T) {
	testutil.IsolateHome(t)
	setBatch(t, true)
	repo := testutil.InitRepo(t)
	gitDir, err := lgit.FindGitDir(repo)
	require.NoError(t, err)
	stubHookInstall(t)
	testutil.Cmd(t, "git", "config", "--global", autoInstallHookOption, "true")

	proceed, err := ensureCommitMsgHook(repo, gitDir)

	require.NoError(t, err)
	assert.True(t, proceed)
	assert.FileExists(t, lgit.HookPath(gitDir, lgit.CommitMsgHook))
}

// TestBatchFlagDefaultsToStdinNotBeingATerminal documents where the default comes
// from. Under `go test` stdin is not a terminal, which is the same situation a CI
// job or an agent is in.
func TestBatchFlagDefaultsToStdinNotBeingATerminal(t *testing.T) {
	c := NewCommand()
	assert.Equal(t, "true", c.PersistentFlags().Lookup("batch").DefValue,
		"without a terminal maiao must default to not prompting")
}

// run executes the version subcommand, which exercises the same persistent hook
// as a review without needing a repository or a network.
func run(t testing.TB, args ...string) {
	t.Helper()
	c := NewCommand()
	c.SetArgs(append([]string{"version"}, args...))
	require.NoError(t, c.Execute())
}

func setTrustNewHosts(t testing.TB, b bool) {
	t.Helper()
	original := mssh.TrustNewHosts()
	t.Cleanup(func() { mssh.SetTrustNewHosts(original) })
	mssh.SetTrustNewHosts(b)
}

// TestFlagsApplyToPromptAndSSH checks the flags are wired to the packages that
// act on them, rather than only being parsed.
//
// Each flag is set from the opposite starting value, so a no-op wiring would show
// up. The two are asserted separately because both flags default to the current
// value of what they control: starting one from its opposite would change the
// other's default too, and the assertion would be about the setup rather than the
// code.
func TestFlagsApplyToPromptAndSSH(t *testing.T) {
	for _, tc := range []struct {
		arg  string
		want bool
	}{
		{arg: "--batch=false", want: false},
		{arg: "--batch=true", want: true},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			setBatch(t, !tc.want)
			run(t, tc.arg)
			assert.Equal(t, tc.want, prompt.Batch())
		})
	}

	for _, tc := range []struct {
		arg  string
		want bool
	}{
		{arg: "--trust-new-ssh-hosts=true", want: true},
		{arg: "--trust-new-ssh-hosts=false", want: false},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			setTrustNewHosts(t, !tc.want)
			run(t, tc.arg)
			assert.Equal(t, tc.want, mssh.TrustNewHosts())
		})
	}

	t.Run("omitting the flags leaves the detected values alone", func(t *testing.T) {
		setBatch(t, true)
		setTrustNewHosts(t, true)
		run(t)
		assert.True(t, prompt.Batch())
		assert.True(t, mssh.TrustNewHosts(), "an opt-in already in force must not be dropped")
	})
}
