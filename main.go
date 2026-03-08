package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wow-look-at-my/dats/runner"
)

const usage = `dats - Declarative Automated Testing System

Usage: dats [options] <file.dats>...

Arguments:
  file.dats    Input .dats file(s) to run

Options:
  -v, --verbose        Show verbose output (command details, full output on failure)
  --keep-temp          Keep temp directory for debugging
  --coverdir <path>    Set GOCOVERDIR on executed commands to collect coverage data
  -h, --help           Show this help message

Examples:
  dats tests.dats                    # Run tests from tests.dats
  dats -v tests.dats                 # Run with verbose output
  dats tests/*.dats                  # Run multiple test files
  dats --coverdir ./coverage tests.dats  # Collect coverage data
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	var files []string
	verbose := false
	keepTemp := false
	coverDir := ""

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help":
			fmt.Print(usage)
			os.Exit(0)
		case "-v", "--verbose":
			verbose = true
		case "--keep-temp":
			keepTemp = true
		case "--coverdir":
			if i+1 >= len(args) {
				fmt.Fprint(os.Stderr, "Error: --coverdir requires a path argument\n")
				os.Exit(1)
			}
			i++
			coverDir = args[i]
		default:
			if arg[0] == '-' {
				fmt.Fprintf(os.Stderr, "Error: unknown option %s\n", arg)
				os.Exit(1)
			}
			files = append(files, arg)
		}
	}

	if len(files) == 0 {
		fmt.Fprint(os.Stderr, "Error: no input files specified\n")
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	// Validate all input files exist and have .dats extension
	for _, path := range files {
		if filepath.Ext(path) != ".dats" {
			fmt.Fprintf(os.Stderr, "Error: input file %s must have .dats extension\n", path)
			os.Exit(1)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: input file %s does not exist\n", path)
			os.Exit(1)
		}
	}

	r := runner.NewRunner(os.Stdout, verbose, keepTemp, coverDir)

	totalPassed := 0
	totalFailed := 0

	for _, path := range files {
		result, err := r.RunFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error running %s: %v\n", path, err)
			os.Exit(1)
		}
		totalPassed += result.Passed
		totalFailed += result.Failed
	}

	// Print overall summary if multiple files
	if len(files) > 1 {
		fmt.Printf("\nTotal: %d/%d passed", totalPassed, totalPassed+totalFailed)
		if totalFailed > 0 {
			fmt.Printf(", %d failed", totalFailed)
		}
		fmt.Println()
	}

	// Exit with non-zero if any tests failed
	if totalFailed > 0 {
		os.Exit(1)
	}
}
