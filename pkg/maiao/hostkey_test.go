package maiao

import (
	"errors"
	"testing"

	"github.com/adevinta/maiao/pkg/prompt"
	mssh "github.com/adevinta/maiao/pkg/ssh"
	"github.com/adevinta/maiao/pkg/testutil"
	"github.com/go-git/go-git/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keyMismatch is the error go-git surfaces when a host presents a key other than
// the one on record. It is matched on its text, so the text is what a test must use.
var keyMismatch = errors.New(`ssh: handshake failed: knownhosts: key mismatch`)

func setBatch(t testing.TB, b bool) {
	t.Helper()
	original := prompt.Batch()
	t.Cleanup(func() { prompt.SetBatch(original) })
	prompt.SetBatch(b)
}

// failingOp counts attempts and returns errs in order, so a test can tell one
// attempt from a retry.
func failingOp(errs ...error) (func() error, *int) {
	attempts := 0
	return func() error {
		attempts++
		if attempts <= len(errs) {
			return errs[attempts-1]
		}
		return nil
	}, &attempts
}

// TestHostKeyFixFailureIsReported is the behaviour this commit needs.
//
// The fix attempt's error used to be discarded in favour of go-git's, which says
// only that the key did not match. Everything actionable — that maiao refuses to
// resolve a mismatch, and the command that resolves it deliberately — lives in the
// discarded error, and so did the sentinel a caller branches on.
func TestHostKeyFixFailureIsReported(t *testing.T) {
	testutil.IsolateHome(t)
	setBatch(t, true)
	op, attempts := failingOp(keyMismatch)

	err := retryAfterHostKeyFix("git.example.com", op)

	require.Error(t, err)
	assert.ErrorIs(t, err, mssh.ErrHostKeyMismatch, "the sentinel a caller branches on must survive")
	assert.Contains(t, err.Error(), "ssh-keygen -R", "the actionable guidance must reach the caller")
	assert.Equal(t, 1, *attempts, "a mismatch that was not resolved must not be retried")
}

// TestOperationIsRetriedAfterASuccessfulFix covers the reason the retry exists: a
// host key maiao has just recorded makes the same operation work.
//
// The fix is stubbed because the real one runs ssh-keyscan against the host.
func TestOperationIsRetriedAfterASuccessfulFix(t *testing.T) {
	original := fixHostKey
	t.Cleanup(func() { fixHostKey = original })
	fixHostKey = func(error, string) error { return nil }
	op, attempts := failingOp(errors.New("ssh: handshake failed: knownhosts: key is unknown"))

	require.NoError(t, retryAfterHostKeyFix("git.example.com", op))
	assert.Equal(t, 2, *attempts, "the operation must be tried again once the key is recorded")
}

// TestErrorsThatAreNotAboutHostKeysPassStraightThrough guards the common case: the
// retry must not swallow, reword or re-run anything else.
func TestErrorsThatAreNotAboutHostKeysPassStraightThrough(t *testing.T) {
	for name, tc := range map[string]struct {
		err      error
		attempts int
	}{
		"success":            {err: nil, attempts: 1},
		"already up to date": {err: git.NoErrAlreadyUpToDate, attempts: 1},
		"an unrelated error": {err: errors.New("remote hung up unexpectedly"), attempts: 1},
	} {
		t.Run(name, func(t *testing.T) {
			op, attempts := failingOp(tc.err)

			assert.Equal(t, tc.err, retryAfterHostKeyFix("git.example.com", op))
			assert.Equal(t, tc.attempts, *attempts)
		})
	}
}

func setTrustNewHosts(t testing.TB, b bool) {
	t.Helper()
	original := mssh.TrustNewHosts()
	t.Cleanup(func() { mssh.SetTrustNewHosts(original) })
	mssh.SetTrustNewHosts(b)
}
