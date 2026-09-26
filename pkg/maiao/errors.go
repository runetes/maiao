package maiao

import "errors"

// ErrRebaseIncomplete reports that the rebase preceding a review did not finish.
//
// The step that creates the reviews is the last entry in the rebase todo list, so
// whenever the rebase stops early — a conflict, most often — nothing has been
// submitted. Finishing the rebase runs that step and creates the reviews.
var ErrRebaseIncomplete = errors.New("the rebase did not complete, so no review was created")

// ErrBaseNotMoved reports that a pull request could not be moved off the base
// branch the review is about to overwrite.
//
// GitHub refuses to change the base of a pull request its own native stacks own,
// and answers the edit as a success with the base left where it was. Pushing from
// there is what closes the review as merged, and a closed pull request cannot be
// reopened once GitHub has marked it that way — so the review stops instead, with
// every branch still as it was on the remote.
var ErrBaseNotMoved = errors.New("a pull request could not be moved off the base branch about to be overwritten")
