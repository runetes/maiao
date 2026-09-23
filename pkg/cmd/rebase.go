package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/runetes/maiao/pkg/system"
)

func rebaseEditor(cmd *cobra.Command, args []string) error {
	if filepath.Base(args[len(args)-1]) == "git-rebase-todo" {
		out, err := system.DefaultFileSystem.Create(args[len(args)-1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open destination path %s for writing: %v", args[len(args)-1], err)
			return nil
		}
		defer out.Close()
		in, err := system.DefaultFileSystem.Open(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open rebase todo path %s for writing: %v", args[0], err)
			return nil
		}
		defer in.Close()
		_, err = io.Copy(out, in)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to copy rebase todo %s to %s: %v", args[0], args[len(args)-1], err)
			return nil
		}
	}
	return nil
	// repo, err := git.PlainOpen(cmd.Flag("path").Value.String())
	// if err != nil {
	// 	return err
	// }
	// return gerrit.Install(repo.CommonGitDir())
}
