package cmd

import (
	"bytes"
	"testing"

	"github.com/adevinta/maiao/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// execute runs the root command with args and returns its error and everything it
// printed. Cobra writes usage to its own output writer, so the buffer is where it
// lands.
func execute(t testing.TB, args ...string) (error, string) {
	t.Helper()
	c := NewCommand()
	out := &bytes.Buffer{}
	c.SetOut(out)
	c.SetErr(out)
	c.SetArgs(args)
	return c.Execute(), out.String()
}

// TestUsageIsNotPrintedForRuntimeFailures is about the actionable messages being
// readable.
//
// A batch-mode failure explains which setting removes the question. Following it
// with the full flag list pushed it off the top of the output, which defeats the
// point of writing it.
func TestUsageIsNotPrintedForRuntimeFailures(t *testing.T) {
	testutil.IsolateHome(t)
	setBatch(t, true)
	repo := testutil.InitRepo(t)
	expectNoInstall(t)

	err, out := execute(t, "--batch", "-C", repo)

	require.Error(t, err)
	assert.Equal(t, ExitInputRequired, ExitCode(err))
	assert.Contains(t, out, "git review install --global", "the actionable message must be there")
	assert.NotContains(t, out, "Available Commands:", "the flag list says nothing about a missing hook")
	assert.NotContains(t, out, "--trust-new-ssh-hosts", "the flag list says nothing about a missing hook")
}

// TestUsageIsPrintedForMisuse is the other half: when the command line itself is
// wrong, the flag list is exactly what the user needs.
func TestUsageIsPrintedForMisuse(t *testing.T) {
	for name, args := range map[string][]string{
		"an unknown flag":       {"--no-such-flag"},
		"too many arguments":    {"main", "extra"},
		"an invalid flag value": {"--verbose", "9"},
	} {
		t.Run(name, func(t *testing.T) {
			err, out := execute(t, args...)

			require.Error(t, err)
			assert.Contains(t, out, "Available Commands:")
		})
	}
}
