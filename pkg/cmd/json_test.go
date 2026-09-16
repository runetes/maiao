package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/adevinta/maiao/pkg/maiao"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noResultFile makes a test start from a process that has no handoff in progress.
//
// The variable is read from the environment, so a leaked value from an earlier
// test would silently turn an owner into a nested run.
func noResultFile(t testing.TB) {
	t.Helper()
	t.Setenv(resultFileEnvVar, "")
	require.NoError(t, os.Unsetenv(resultFileEnvVar))
}

func reviewed(urls ...string) *maiao.Result {
	result := &maiao.Result{}
	for _, url := range urls {
		result.Changes = append(result.Changes, maiao.Change{
			ChangeID: "I0123456789abcdef",
			Branch:   "maiao.I0123456789abcdef",
			URL:      url,
			ID:       "42",
			Status:   maiao.StatusCreated,
		})
	}
	return result
}

func emit(t testing.TB, h *resultHandoff, jsonOutput bool, result *maiao.Result) string {
	t.Helper()
	out := &bytes.Buffer{}
	require.NoError(t, emitResult(out, h, jsonOutput, result, nil))
	return out.String()
}

// emitFailure is emit for a review that failed, returning what was printed and the
// error the caller should exit on.
func emitFailure(t testing.TB, h *resultHandoff, result *maiao.Result, err error) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	reported := emitResult(out, h, true, result, err)
	return out.String(), reported
}

// TestJSONFlagIsOffByDefault guards the human-facing default: prose, not JSON.
func TestJSONFlagIsOffByDefault(t *testing.T) {
	assert.Equal(t, "false", NewCommand().PersistentFlags().Lookup("json").DefValue)
}

// TestWithoutJSONNothingIsWrittenToStdout is what keeps this commit from changing
// existing behaviour. The progress prose a human reads is already on stderr.
func TestWithoutJSONNothingIsWrittenToStdout(t *testing.T) {
	noResultFile(t)
	h, err := newResultHandoff(false)
	require.NoError(t, err)
	defer h.cleanup()

	assert.Empty(t, emit(t, h, false, reviewed("https://example.com/pull/42")))
}

// TestJSONFieldNamesAreTheContract pins the wire format by its literal keys.
//
// Asserting through a round trip into maiao.Result would pass even if a field were
// renamed, since both sides would move together, and the whole point of --json is
// that a caller written against it keeps working.
func TestJSONFieldNamesAreTheContract(t *testing.T) {
	noResultFile(t)
	h, err := newResultHandoff(true)
	require.NoError(t, err)
	defer h.cleanup()

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(emit(t, h, true, reviewed("https://example.com/pull/42"))), &decoded))

	changes, ok := decoded["changes"].([]any)
	require.True(t, ok, "changes must be a list")
	require.Len(t, changes, 1)
	assert.Equal(t, map[string]any{
		"change_id": "I0123456789abcdef",
		"branch":    "maiao.I0123456789abcdef",
		"url":       "https://example.com/pull/42",
		"id":        "42",
		"status":    "created",
	}, changes[0])
}

// TestSuccessHasNoErrorKey is what makes the key additive.
//
// Anything already written against a successful result must not have to learn about
// `error` to keep working, so it has to be absent rather than null or empty.
func TestSuccessHasNoErrorKey(t *testing.T) {
	noResultFile(t)
	h, err := newResultHandoff(true)
	require.NoError(t, err)
	defer h.cleanup()

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(emit(t, h, true, reviewed("https://example.com/pull/42"))), &decoded))

	assert.NotContains(t, decoded, "error")
	assert.NotContains(t, decoded, "stack_id", "a review with no native stack must not claim one")
}

// TestAFailedReviewReportsWhatItSubmitted is the reason a failure is reported at
// all rather than left to stderr.
//
// A review can fail after creating some of its pull requests. Those exist on the
// remote, and printing nothing describes the run as one that did nothing — which is
// the worst possible thing to tell a caller deciding whether to retry.
func TestAFailedReviewReportsWhatItSubmitted(t *testing.T) {
	noResultFile(t)
	h, err := newResultHandoff(true)
	require.NoError(t, err)
	defer h.cleanup()

	out, reported := emitFailure(t, h, reviewed("https://example.com/pull/101"), transport.ErrAuthorizationFailed)

	require.ErrorIs(t, reported, transport.ErrAuthorizationFailed, "reporting must not swallow the failure")
	var decoded struct {
		Changes []maiao.Change `json:"changes"`
		Error   *maiao.Failure `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	require.Len(t, decoded.Changes, 1)
	assert.Equal(t, "https://example.com/pull/101", decoded.Changes[0].URL)
	require.NotNil(t, decoded.Error)
	assert.Equal(t, "auth", decoded.Error.Kind)
	assert.Equal(t, ExitAuth, decoded.Error.Code)
	assert.Equal(t, transport.ErrAuthorizationFailed.Error(), decoded.Error.Message)
}

// TestErrorFieldNamesAreTheContract pins the failure keys literally, for the same
// reason the change keys are pinned: a round trip through maiao.Failure would pass
// even if both sides were renamed together.
func TestErrorFieldNamesAreTheContract(t *testing.T) {
	noResultFile(t)
	h, err := newResultHandoff(true)
	require.NoError(t, err)
	defer h.cleanup()

	out, _ := emitFailure(t, h, nil, errors.New("something went wrong"))

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	assert.Equal(t, map[string]any{
		"kind":    "error",
		"code":    float64(ExitFailure),
		"message": "something went wrong",
	}, decoded["error"])
	assert.Equal(t, []any{}, decoded["changes"], "a failure with nothing submitted still lists no changes rather than null")
}

// TestNoJSONMeansNoJSONEvenOnFailure keeps --json the only thing that puts anything
// on stdout. Without it the report is the exit status and stderr.
//
// The handoff is the owning kind, so that this exercises the process whose stdout
// the caller is actually reading rather than a nested one, which stays silent for a
// different reason.
func TestNoJSONMeansNoJSONEvenOnFailure(t *testing.T) {
	noResultFile(t)
	h, err := newResultHandoff(true)
	require.NoError(t, err)
	defer h.cleanup()
	require.True(t, h.owner)

	out := &bytes.Buffer{}
	reported := emitResult(out, h, false, reviewed("https://example.com/pull/42"), errors.New("boom"))

	assert.Empty(t, out.String())
	assert.EqualError(t, reported, "boom", "the failure is still what the process exits on")
}

// TestANestedFailureIsHandedBack covers a rebase whose final step failed.
//
// The nested run's stdout belongs to git, so without the handoff its failure would
// reach the caller only as whatever git rebase made of it. Both cases matter: a
// failure that got some pull requests open, and the more common one — a bad token —
// that got none, where the failure is the entire report.
func TestANestedFailureIsHandedBack(t *testing.T) {
	for name, submitted := range map[string]*maiao.Result{
		"having submitted nothing":        {},
		"having submitted a pull request": reviewed("https://example.com/pull/101"),
	} {
		t.Run(name, func(t *testing.T) {
			noResultFile(t)
			outer, err := newResultHandoff(true)
			require.NoError(t, err)
			defer outer.cleanup()
			nested, err := newResultHandoff(true)
			require.NoError(t, err)
			require.False(t, nested.owner)

			out, reported := emitFailure(t, nested, submitted, transport.ErrAuthorizationFailed)
			assert.Empty(t, out, "the nested run must not write to a stdout that belongs to git rebase")
			require.ErrorIs(t, reported, transport.ErrAuthorizationFailed)

			// The outer run's own result on the rebase path: empty, and successful as far
			// as it can tell when git rebase itself returned nothing.
			out, reported = emitFailure(t, outer, &maiao.Result{}, nil)

			require.Error(t, reported, "a nested failure must not be reported as success")
			assert.Equal(t, ExitAuth, ExitCode(reported), "the status the nested run would have exited with")
			assert.Contains(t, out, `"kind": "auth"`)
			for _, change := range submitted.Changes {
				assert.Contains(t, out, change.URL)
			}
		})
	}
}

// TestTheOuterFailureWinsOverANestedOne pins which failure the report describes.
//
// `error.code` is the status the process exits with, so when both runs failed the
// outer one's is reported: it is the one that says what state the caller has been
// left in. The nested cause is already on stderr, printed by the run that hit it.
func TestTheOuterFailureWinsOverANestedOne(t *testing.T) {
	noResultFile(t)
	outer, err := newResultHandoff(true)
	require.NoError(t, err)
	defer outer.cleanup()
	nested, err := newResultHandoff(true)
	require.NoError(t, err)

	_, _ = emitFailure(t, nested, reviewed("https://example.com/pull/101"), transport.ErrAuthorizationFailed)
	out, reported := emitFailure(t, outer, &maiao.Result{}, maiao.ErrRebaseIncomplete)

	assert.Equal(t, ExitRebaseIncomplete, ExitCode(reported))
	assert.Contains(t, out, `"kind": "rebase_incomplete"`)
	assert.Contains(t, out, `"code": 3`)
	assert.Contains(t, out, "https://example.com/pull/101", "what the nested run submitted is still reported")
}

// TestEmptyResultIsStillAnObjectWithAList covers the nothing-to-review run.
//
// A nil slice encodes as null, so a caller iterating the result would have to
// special-case it. An empty list needs no special case.
func TestEmptyResultIsStillAnObjectWithAList(t *testing.T) {
	noResultFile(t)
	for name, result := range map[string]*maiao.Result{
		"empty result": {},
		"no result":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			h, err := newResultHandoff(true)
			require.NoError(t, err)
			defer h.cleanup()

			assert.JSONEq(t, `{"changes": []}`, emit(t, h, true, result))
		})
	}
}

// TestNestedRunHandsItsResultBack covers the rebase path, and is the reason the
// handoff exists at all.
//
// When a rebase is needed, the pull requests are created by the maiao that git
// rebase runs as its final todo step. That process's stdout belongs to git, so it
// writes its result to the file the outer run published instead.
func TestNestedRunHandsItsResultBack(t *testing.T) {
	noResultFile(t)
	outer, err := newResultHandoff(true)
	require.NoError(t, err)
	defer outer.cleanup()
	require.True(t, outer.owner)

	// A separate process, which finds the path in the environment it inherited.
	nested, err := newResultHandoff(true)
	require.NoError(t, err)
	require.False(t, nested.owner, "a run that inherits the path is not the one the caller is reading")

	assert.Empty(t, emit(t, nested, true, reviewed("https://example.com/pull/42")),
		"the nested run must not write to a stdout that belongs to git rebase")
	assert.Contains(t, emit(t, outer, true, &maiao.Result{}),
		"https://example.com/pull/42",
		"the outer run reports what the rebase step created")
}

// TestNestedResultDoesNotOverwriteTheOuterOne pins the direction of the fallback.
//
// The outer run only reads the file when it has nothing of its own, so a stale
// file — from a previous rebase in the same run, say — cannot displace a result
// this process actually produced.
func TestNestedResultDoesNotOverwriteTheOuterOne(t *testing.T) {
	noResultFile(t)
	outer, err := newResultHandoff(true)
	require.NoError(t, err)
	defer outer.cleanup()
	require.NoError(t, outer.write(reviewed("https://example.com/pull/stale")))

	out := emit(t, outer, true, reviewed("https://example.com/pull/fresh"))

	assert.Contains(t, out, "pull/fresh")
	assert.NotContains(t, out, "pull/stale")
}

// TestEmptyNestedResultIsNotWritten is what makes the fallback safe.
//
// On the rebase path the outer run's own result is always empty. If an empty
// result were written, the outer run would clobber the nested one before reading
// it back and report no changes at all.
func TestEmptyNestedResultIsNotWritten(t *testing.T) {
	noResultFile(t)
	outer, err := newResultHandoff(true)
	require.NoError(t, err)
	defer outer.cleanup()
	require.NoError(t, outer.write(reviewed("https://example.com/pull/42")))

	require.NoError(t, outer.write(&maiao.Result{}))

	assert.Contains(t, emit(t, outer, true, &maiao.Result{}), "https://example.com/pull/42")
}

// TestHandoffPathIsPublishedToSubprocesses checks the one mechanism available to
// reach a process maiao does not spawn itself.
//
// git rebase builds its subprocess environment from os.Environ, so the path has to
// be in this process's environment, not merely in a struct field.
func TestHandoffPathIsPublishedToSubprocesses(t *testing.T) {
	noResultFile(t)
	h, err := newResultHandoff(true)
	require.NoError(t, err)

	assert.Equal(t, h.path, os.Getenv(resultFileEnvVar))

	h.cleanup()
	assert.Empty(t, os.Getenv(resultFileEnvVar), "the path must not leak past the run that owns it")
	assert.NoFileExists(t, h.path)
}

// TestNoHandoffFileWithoutJSON keeps the default path free of side effects: a run
// nobody asked for a result from creates nothing to clean up.
func TestNoHandoffFileWithoutJSON(t *testing.T) {
	noResultFile(t)
	h, err := newResultHandoff(false)
	require.NoError(t, err)
	defer h.cleanup()

	assert.Empty(t, h.path)
	assert.Empty(t, os.Getenv(resultFileEnvVar))
}
