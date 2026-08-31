package maiao

// Status is what a review did to one pull request.
type Status string

const (
	// StatusCreated means the pull request did not exist before this review.
	StatusCreated Status = "created"
	// StatusUpdated means an existing pull request was brought up to date.
	StatusUpdated Status = "updated"
)

// Result describes the outcome of a review.
//
// It is what `--json` serialises, so its field names and values are a contract
// with callers and should be treated as such when changing them.
type Result struct {
	// Changes lists one entry per reviewed commit, in the order they are stacked,
	// so the first entry is the one nearest the target branch. A change appears here
	// once its pull request exists, so a review that failed part way still reports
	// the ones it did submit.
	Changes []Change `json:"changes"`
	// StackID identifies the stack the provider recorded, for the providers that
	// model one natively. Absent when the provider has no stack API, when stacking
	// was turned off, or when there was only one change to stack.
	StackID string `json:"stack_id,omitempty"`
	// Error describes why the review did not complete, and is absent entirely when
	// it did. It is populated by the command layer rather than here, since what
	// classifies a failure is the same thing that chooses the exit status.
	Error *Failure `json:"error,omitempty"`
}

// Failure is why a review did not complete.
//
// It exists so that a caller reading stdout learns the same thing as one reading
// the exit status, without having to interpret prose meant for a person.
type Failure struct {
	// Kind names the failure class. It is the exit status by another name, for a
	// caller that would rather match on a word than on a number.
	Kind string `json:"kind"`
	// Code is the exit status of the process that printed this, so the two can never
	// disagree.
	Code int `json:"code"`
	// Message is the diagnostic, the same text that appears on stderr.
	Message string `json:"message"`
}

// Error makes a Failure usable as the error it describes, so that one handed back
// across the rebase boundary keeps its classification instead of being reclassified
// from its text.
func (f *Failure) Error() string {
	return f.Message
}

// Change is the pull request for a single commit.
type Change struct {
	// ChangeID is the Change-Id trailer, which identifies the change across
	// rebases and amends. It is the key to correlate entries between runs.
	ChangeID string `json:"change_id"`
	// Branch is the remote branch the commit was pushed to. It derives from
	// ChangeID, so it is stable across runs.
	Branch string `json:"branch"`
	// URL is the pull request as a person would open it.
	URL string `json:"url"`
	// ID is the provider's identifier for the pull request, the number in its web
	// interface for every provider maiao supports. It is a string because that is
	// how maiao models it, and providers are not obliged to use integers.
	ID string `json:"id"`
	// Status is whether this run created the pull request or updated it.
	Status Status `json:"status"`
}

// newResult describes the changes a review just pushed.
//
// The order of changes is preserved, since it is the order they are stacked in and
// part of what the result reports.
func newResult(changes []*change) *Result {
	result := &Result{Changes: make([]Change, 0, len(changes))}
	for _, change := range changes {
		// A change with no pull request is one the review never reached, which is how a
		// failure part way through the stack looks. Reporting it with an empty URL would
		// claim a review that does not exist.
		if change.pr == nil {
			continue
		}
		status := StatusUpdated
		if change.created {
			status = StatusCreated
		}
		result.Changes = append(result.Changes, Change{
			ChangeID: change.changeID,
			Branch:   change.branch,
			URL:      change.pr.URL,
			ID:       change.pr.ID,
			Status:   status,
		})
	}
	return result
}
