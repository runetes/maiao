package maiao

import "errors"

// ErrRebaseIncomplete reports that the rebase preceding a review did not finish.
//
// The step that creates the reviews is the last entry in the rebase todo list, so
// whenever the rebase stops early — a conflict, most often — nothing has been
// submitted. Finishing the rebase runs that step and creates the reviews.
var ErrRebaseIncomplete = errors.New("the rebase did not complete, so no review was created")
