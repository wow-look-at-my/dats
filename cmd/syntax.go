package cmd

import (
	"fmt"
	"os"

	"github.com/mhaynie/bats-declarative/schema"
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

		hasErrors := false
		for _, path := range files {
			testFile, err := schema.ParseFile(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", path, err)
				hasErrors = true
				continue
			}

			if verbose {
				fmt.Printf("ok   %s (%d tests)\n", path, len(testFile.Tests))
			} else {
				fmt.Printf("ok   %s\n", path)
			}
		}

		if hasErrors {
			os.Exit(1)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(syntaxCmd)
}
