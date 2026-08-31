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

// ExitCode classifies a failure so a caller can branch on it without parsing text.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return ExitSuccess
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
