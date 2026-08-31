package main

import (
	"os"

	"github.com/adevinta/maiao/pkg/cmd"
)

func main() {
	// Execute has already reported the error on stderr. Printing it again here, on
	// stdout, showed every failure twice and put it on the stream reserved for
	// output a caller parses.
	if err := cmd.NewCommand().Execute(); err != nil {
		os.Exit(cmd.ExitCode(err))
	}
}
