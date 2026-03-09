package cmd

import (
	"fmt"
	"os"

	"github.com/wow-look-at-my/dats/runner"
	"github.com/spf13/cobra"
)

var (
	keepTemp bool
	coverDir string
)

var testCmd = &cobra.Command{
	Use:   "test [files...]",
	Short: "Run tests from .dats files",
	Long: `Run tests defined in .dats files. If no files are specified,
recursively finds and runs all .dats files in the current directory tree.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTests(args)
	},
}

func runTests(args []string) error {
	files, err := resolveFiles(args)
	if err != nil {
		return err
	}

	r := runner.NewRunner(os.Stdout, verbose, keepTemp, coverDir)

	totalPassed := 0
	totalFailed := 0

	for _, path := range files {
		result, err := r.RunFile(path)
		if err != nil {
			return fmt.Errorf("running %s: %w", path, err)
		}
		totalPassed += result.Passed
		totalFailed += result.Failed
	}

	if len(files) > 1 {
		fmt.Printf("\nTotal: %d/%d passed", totalPassed, totalPassed+totalFailed)
		if totalFailed > 0 {
			fmt.Printf(", %d failed", totalFailed)
		}
		fmt.Println()
	}

	if totalFailed > 0 {
		os.Exit(1)
	}

	return nil
}

func init() {
	testCmd.Flags().BoolVar(&keepTemp, "keep-temp", false, "Keep temp directory for debugging")
	testCmd.Flags().StringVar(&coverDir, "coverdir", "", "Set GOCOVERDIR on executed commands to collect coverage data")
	rootCmd.AddCommand(testCmd)
}
