package prompt

import (
	"errors"
	"os"

	"golang.org/x/term"
)

// ErrNoInput reports that maiao needed to ask the user something while running
// with nobody available to answer. Callers wrap it so that the message can name
// the setting that would have avoided the question.
var ErrNoInput = errors.New("input required, but maiao is running in batch mode")

var batch = !term.IsTerminal(int(os.Stdin.Fd()))

// SetBatch turns prompting off or back on, overriding the terminal detection.
func SetBatch(b bool) {
	batch = b
}

// Batch reports whether maiao must not prompt.
//
// It defaults to true when standard input is not a terminal. Without a terminal
// the prompts do not produce anything a caller can act on: the hook question
// resolves to "no" and maiao exits successfully having done nothing, and the
// provider question fails with the end of input marker as its message. Both are
// worse than refusing to ask and saying what to configure instead.
func Batch() bool {
	return batch
}
