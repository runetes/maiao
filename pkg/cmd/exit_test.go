package cmd

import (
	"errors"
	"fmt"
	"testing"

	"github.com/adevinta/maiao/pkg/credentials"
	lgit "github.com/adevinta/maiao/pkg/git"
	"github.com/adevinta/maiao/pkg/maiao"
	mssh "github.com/adevinta/maiao/pkg/ssh"
	"github.com/adevinta/maiao/pkg/testutil"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noCredentials produces the error a credential chain really returns when nothing
// in it can supply a token, so the test is not asserting against a type it made up.
func noCredentials(t testing.TB) error {
	t.Helper()
	_, err := credentials.ChainCredentialGetter{
		&credentials.EnvToken{PasswordKey: "MAIAO_TOKEN_DELIBERATELY_UNSET"},
	}.CredentialForHost("git.example.com")
	require.Error(t, err)
	return err
}

// TestExitCode maps each failure mode to the status a caller branches on.
//
// The wrapping in each case mirrors how the error reaches main: a classification
// that only worked on a bare sentinel would be useless, since nothing returns one.
func TestExitCode(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"success": {err: nil, want: ExitSuccess},
		"an error with no classification": {
			err:  errors.New("something went wrong"),
			want: ExitFailure,
		},
		"no credentials for the host": {
			// The two wraps are the ones in gitlab.go and factory.go.
			err:  fmt.Errorf("failed to create provider client: %w", fmt.Errorf("failed to get credentials for git.example.com: %w", noCredentials(t))),
			want: ExitAuth,
		},
		"credentials the host refused": {
			err:  fmt.Errorf("failed to update git repository: %w", transport.ErrAuthorizationFailed),
			want: ExitAuth,
		},
		"credentials the host demanded": {
			err:  fmt.Errorf("failed to update git repository: %w", transport.ErrAuthenticationRequired),
			want: ExitAuth,
		},
		"a rebase that stopped": {
			err:  fmt.Errorf("%w: %w", maiao.ErrRebaseIncomplete, errors.New("exit status 1")),
			want: ExitRebaseIncomplete,
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, ExitCode(tc.err))
		})
	}
}

// TestFailureIsClassifiedForBothReaders keeps the JSON and the exit status telling
// the same story, so a caller matching on `error.kind` and one matching on the
// status cannot reach different conclusions.
func TestFailureIsClassifiedForBothReaders(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		kind string
		code int
	}{
		"an error with no classification": {err: errors.New("boom"), kind: "error", code: ExitFailure},
		"no credentials for the host":     {err: noCredentials(t), kind: "auth", code: ExitAuth},
		"credentials the host refused":    {err: transport.ErrAuthorizationFailed, kind: "auth", code: ExitAuth},
		"a rebase that stopped":           {err: maiao.ErrRebaseIncomplete, kind: "rebase_incomplete", code: ExitRebaseIncomplete},
	} {
		t.Run(name, func(t *testing.T) {
			failure := newFailure(tc.err)

			require.NotNil(t, failure)
			assert.Equal(t, tc.kind, failure.Kind)
			assert.Equal(t, tc.code, failure.Code)
			assert.Equal(t, ExitCode(tc.err), failure.Code, "the reported code must be the status the process exits with")
			assert.Equal(t, tc.err.Error(), failure.Message)
		})
	}
}

// TestEveryStatusIsNamed guards the pairing itself: a status added without a name
// would be reported as a generic error, which is the one thing `kind` exists to
// avoid.
func TestEveryStatusIsNamed(t *testing.T) {
	for _, code := range []int{ExitFailure, ExitAuth, ExitRebaseIncomplete, ExitInputRequired, ExitHostKeyMismatch} {
		assert.NotEmpty(t, failureKinds[code], "status %d has no name", code)
	}
	assert.Len(t, failureKinds, 5, "a new status needs a name here too")
}

// TestSuccessHasNoFailure is what keeps `error` absent from a successful result,
// which is what makes the key additive for callers already parsing `changes`.
func TestSuccessHasNoFailure(t *testing.T) {
	assert.Nil(t, newFailure(nil))
}

// TestAClassifiedFailureKeepsItsStatus covers the error a nested run hands back
// across the rebase boundary.
//
// It arrives as data rather than as the sentinel it started as, so classifying it
// from its message would demote a known status to the generic one.
func TestAClassifiedFailureKeepsItsStatus(t *testing.T) {
	handedBack := &maiao.Failure{Kind: "auth", Code: ExitAuth, Message: "unable to find token for api.github.com"}

	assert.Equal(t, ExitAuth, ExitCode(handedBack))
	assert.Equal(t, ExitAuth, ExitCode(fmt.Errorf("wrapped: %w", handedBack)))
}

// TestExitCodeForBatchModeFailure classifies the error the real batch-mode path
// produces, rather than a stand-in for it.
func TestExitCodeForBatchModeFailure(t *testing.T) {
	testutil.IsolateHome(t)
	setBatch(t, true)
	repo := testutil.InitRepo(t)
	gitDir, err := lgit.FindGitDir(repo)
	require.NoError(t, err)
	expectNoInstall(t)

	_, err = ensureCommitMsgHook(repo, gitDir)

	require.Error(t, err)
	assert.Equal(t, ExitInputRequired, ExitCode(err))
}

// TestExitCodeForHostKeyMismatch is the one status that must not be reachable by
// configuring the prompt away, so it is checked against the real refusal and
// against the batch-mode status it has to stay distinct from.
func TestExitCodeForHostKeyMismatch(t *testing.T) {
	testutil.IsolateHome(t)
	setBatch(t, true)
	setTrustNewHosts(t, true)

	err := mssh.PromptAndFix("git.example.com", true)

	require.Error(t, err)
	assert.Equal(t, ExitHostKeyMismatch, ExitCode(err),
		"a mismatch must not be reported as merely needing input, even with new hosts trusted")
}
