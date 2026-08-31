package cmd

import (
	"errors"

	"github.com/adevinta/maiao/pkg/credentials"
	"github.com/adevinta/maiao/pkg/maiao"
	"github.com/adevinta/maiao/pkg/prompt"
	mssh "github.com/adevinta/maiao/pkg/ssh"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

// The exit statuses maiao reports.
//
// Only zero and non-zero were meaningful before, so a caller had to match on error
// prose to tell a condition it could resolve from one that needs a person. These
// statuses are additive: non-zero still means failure.
const (
	// ExitSuccess means the review completed. It says nothing about how many pull
	// requests were involved, including none.
	ExitSuccess = 0
	// ExitFailure is every failure with no more specific status, and the status any
	// new failure mode gets until it is classified.
	ExitFailure = 1
	// ExitAuth means maiao has no usable credentials for the host, or the host
	// rejected them. Retrying changes nothing until a token is provided.
	ExitAuth = 2
	// ExitRebaseIncomplete means the rebase stopped before the step that creates the
	// reviews. The working tree is mid-rebase and a caller able to resolve conflicts
	// can carry on from there.
	ExitRebaseIncomplete = 3
	// ExitInputRequired means maiao needed an answer and had no terminal to ask on.
	// The message names the setting that removes the question, so this is resolved by
	// configuring, not by retrying.
	ExitInputRequired = 4
	// ExitHostKeyMismatch means the host presented a different key than the one on
	// record. It is deliberately distinct from ExitInputRequired: it can indicate an
	// interception, so it should stop a caller rather than invite it to configure the
	// question away.
	ExitHostKeyMismatch = 5
)

// failureKinds names each status, so a caller matching on the word in the JSON
// result and one matching on the exit status are branching on the same thing.
//
// Kept next to the statuses rather than beside the schema, so that adding a status
// and forgetting to name it is a one-line change in one place.
var failureKinds = map[int]string{
	ExitFailure:          "error",
	ExitAuth:             "auth",
	ExitRebaseIncomplete: "rebase_incomplete",
	ExitInputRequired:    "input_required",
	ExitHostKeyMismatch:  "host_key_mismatch",
}

// newFailure describes err the way the JSON result reports it, or nil when there is
// no failure to report.
//
// Code is taken from ExitCode rather than chosen here, so the status in the payload
// is by construction the status the process exits with.
func newFailure(err error) *maiao.Failure {
	if err == nil {
		return nil
	}
	code := ExitCode(err)
	kind, ok := failureKinds[code]
	if !ok {
		// A status nobody named is still a failure, and reporting it as the generic one
		// is better than reporting it as an empty string.
		kind = failureKinds[ExitFailure]
	}
	return &maiao.Failure{Kind: kind, Code: code, Message: err.Error()}
}

// ExitCode classifies a failure so a caller can branch on it without parsing text.
func ExitCode(err error) int {
	var classified *maiao.Failure
	switch {
	case err == nil:
		return ExitSuccess
	// First, because a failure handed back from the run git rebase invoked has already
	// been classified by that run. Reclassifying it from its message would turn a
	// known status into a generic one.
	case errors.As(err, &classified):
		return classified.Code
	// Ahead of ExitInputRequired, because a mismatch is the one prompt maiao refuses
	// to let a caller configure away.
	case errors.Is(err, mssh.ErrHostKeyMismatch):
		return ExitHostKeyMismatch
	case errors.Is(err, prompt.ErrNoInput):
		return ExitInputRequired
	case errors.Is(err, maiao.ErrRebaseIncomplete):
		return ExitRebaseIncomplete
	case isAuthFailure(err):
		return ExitAuth
	default:
		return ExitFailure
	}
}

// isAuthFailure covers both ends of authentication: no credentials to offer, and
// credentials the host would not accept.
func isAuthFailure(err error) bool {
	var noCredentials credentials.Errors
	return errors.As(err, &noCredentials) ||
		errors.Is(err, transport.ErrAuthenticationRequired) ||
		errors.Is(err, transport.ErrAuthorizationFailed)
}
