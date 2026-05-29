package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/wow-look-at-my/dats/schema"
	"github.com/spf13/cobra"
)

var syntaxCmd = &cobra.Command{
	Use:   "syntax [files...]",
	Short: "Validate .dats file syntax without running tests",
	Long: `Parse and validate .dats files without executing any tests.
If no files are specified, recursively finds and validates all .dats files
in the current directory tree.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		files, err := resolveFiles(args)
		if err != nil {
			return err
		}
		if !runSyntax(files, os.Stdout, os.Stderr) {
			os.Exit(1)
		}
		return nil
	},
}

// runSyntax parses each file, writing "ok" lines to out and "FAIL" lines to
// errw. It returns false if any file failed to parse.
func runSyntax(files []string, out, errw io.Writer) bool {
	ok := true
	for _, path := range files {
		testFile, err := schema.ParseFile(path)
		if err != nil {
			fmt.Fprintf(errw, "FAIL %s: %v\n", path, err)
			ok = false
			continue
		}

		if verbose {
			fmt.Fprintf(out, "ok   %s (%d tests)\n", path, len(testFile.Tests))
		} else {
			fmt.Fprintf(out, "ok   %s\n", path)
		}
	}
	return ok
}

func init() {
	rootCmd.AddCommand(syntaxCmd)
}
