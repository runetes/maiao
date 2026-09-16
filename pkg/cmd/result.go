package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/adevinta/maiao/pkg/maiao"
)

// resultFileEnvVar names a file a nested maiao run writes its result to.
//
// When a rebase is needed, git rebase is asked to run maiao again as its final
// todo step, and it is that nested run which creates the pull requests. Its
// standard output belongs to git, not to the caller, so the result cannot simply
// be printed: it is written here instead, and the process that started the rebase
// reads it and emits it as its own.
const resultFileEnvVar = "MAIAO_RESULT_FILE"

// resultHandoff carries a review's result across the rebase subprocess boundary.
type resultHandoff struct {
	path string
	// owner is true for the process that created the file, which is the one whose
	// standard output the caller is reading.
	owner bool
}

// newResultHandoff prepares the handoff for this process.
//
// The path is published through the environment because git rebase's subprocess
// inherits it, which is the only channel to a process maiao does not spawn itself.
func newResultHandoff(wanted bool) (*resultHandoff, error) {
	if path := os.Getenv(resultFileEnvVar); path != "" {
		return &resultHandoff{path: path}, nil
	}
	if !wanted {
		return &resultHandoff{}, nil
	}
	f, err := os.CreateTemp("", "maiao-result-")
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := os.Setenv(resultFileEnvVar, f.Name()); err != nil {
		return nil, err
	}
	return &resultHandoff{path: f.Name(), owner: true}, nil
}

func (h *resultHandoff) cleanup() {
	if h.owner && h.path != "" {
		os.Remove(h.path)
		os.Unsetenv(resultFileEnvVar)
	}
}

// write records a nested run's result for the process that started the rebase.
//
// A result with nothing in it is not written, so that the outer run, whose own
// result is always empty on the rebase path, cannot overwrite the inner run's. A
// failure counts as something: it is the whole point of writing on that path.
func (h *resultHandoff) write(result *maiao.Result) error {
	if h.path == "" || result == nil || (len(result.Changes) == 0 && result.Error == nil) {
		return nil
	}
	content, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return os.WriteFile(h.path, content, 0600)
}

// read returns the result a nested run handed back, or nil if there was none.
func (h *resultHandoff) read() *maiao.Result {
	if h.path == "" {
		return nil
	}
	content, err := os.ReadFile(h.path)
	if err != nil || len(content) == 0 {
		return nil
	}
	result := &maiao.Result{}
	if err := json.Unmarshal(content, result); err != nil {
		return nil
	}
	return result
}

// reconcile settles what this process is going to report.
//
// The result of a rebasing run comes from the nested run, so it is read back here,
// and its failure becomes this process's failure when this process has none of its
// own — otherwise a nested run that failed while the rebase reported success would
// exit zero. When this process does have a failure, that one is kept, because it
// describes the state the caller has been left in.
func (h *resultHandoff) reconcile(result *maiao.Result, err error) (*maiao.Result, error) {
	if result == nil {
		result = &maiao.Result{}
	}
	// Only when there is nothing of this run's own, so that a stale file cannot
	// displace a result this process actually produced.
	if len(result.Changes) == 0 {
		if nested := h.read(); nested != nil {
			nestedErr := nested.Error
			result = nested
			if err == nil && nestedErr != nil {
				err = nestedErr
			}
		}
	}
	// Set last and unconditionally, so the reported status is always this process's
	// own and can never disagree with the status it exits with.
	result.Error = newFailure(err)
	if result.Changes == nil {
		result.Changes = []maiao.Change{}
	}
	return result, err
}

// emitResult reports the outcome of a review, and returns the failure to exit on.
//
// Only the process the caller is watching writes to standard output. A nested run
// hands its result back through the file instead. A failed review still reports the
// changes it managed to submit, since those pull requests exist whether or not the
// run finished.
func emitResult(out io.Writer, h *resultHandoff, jsonOutput bool, result *maiao.Result, err error) error {
	result, err = h.reconcile(result, err)
	if !h.owner {
		if writeErr := h.write(result); writeErr != nil && err == nil {
			return writeErr
		}
		return err
	}
	if !jsonOutput {
		return err
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if encodeErr := encoder.Encode(result); encodeErr != nil && err == nil {
		return fmt.Errorf("failed to write result: %w", encodeErr)
	}
	return err
}
