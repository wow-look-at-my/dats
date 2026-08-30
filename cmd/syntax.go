package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/dats/schema"

	dats "github.com/wow-look-at-my/dats"
)

// errSyntaxFailed signals that a file failed validation.
var errSyntaxFailed = errors.New("syntax validation failed")

var syntaxCmd = &cobra.Command{
	Use:   "syntax [files-or-dirs...]",
	Short: "Validate .dats file syntax without running tests",
	Long: `Parse and validate .dats files without executing any test, hook, or command.
Arguments resolve exactly as they do for "dats test"; with none, every .dats
file under the working directory is validated.

Validation is the parser's full strictness: tab-only indentation, unknown or
duplicate keys, non-local fixture names, undeclared {matrix.X} references,
heredocs and herestrings in a command, and a file that tries to switch its own
sandbox off are all reported here.

Each valid file prints "ok   <path>" on stdout; each invalid one prints
"FAIL <path>: <error>" on stderr. The exit status is 0 when every file parsed
and 1 otherwise. Nothing runs, so no sandbox backend has to be installed.

Depth: "dats docs format".`,
	Example: `  dats syntax                # validate every .dats file in the tree
  dats syntax tests/         # validate a directory
  dats syntax -v one.dats    # also print each file's test count`,
	RunE: func(cmd *cobra.Command, args []string) error {
		files, err := dats.FindFiles(args)
		if err != nil {
			return err
		}
		if !runSyntax(files, os.Stdout, os.Stderr) {
			return errSyntaxFailed
		}
		return nil
	},
}

// runSyntax parses each file, writing "ok" lines to out and "FAIL" lines to errw.
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
