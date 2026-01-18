package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mhaynie/bats-declarative/internal/generator"
	"github.com/mhaynie/bats-declarative/internal/runtime"
)

const usage = `dats - Declarative Automated Testing System

Usage: dats <file.dats> [output_dir]

Arguments:
  file.dats    Input .dats file to convert
  output_dir   Output directory for generated files (default: same as input)

Examples:
  dats tests.dats                    # Generate and run tests.gen.bats
  dats tests.dats ./output           # Generate in ./output and run tests
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	inputPath := os.Args[1]

	if inputPath == "-h" || inputPath == "--help" {
		fmt.Print(usage)
		os.Exit(0)
	}

	// Validate input file exists and has .dats extension
	if filepath.Ext(inputPath) != ".dats" {
		fmt.Fprintf(os.Stderr, "Error: input file must have .dats extension\n")
		os.Exit(1)
	}

	if _, err := os.Stat(inputPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: input file %s does not exist\n", inputPath)
		os.Exit(1)
	}

	// Determine output directory
	outputDir := filepath.Dir(inputPath)
	if len(os.Args) >= 3 {
		outputDir = os.Args[2]
	}

	// Create output directory if needed
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	// Make paths absolute
	absInput, err := filepath.Abs(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	gen := &generator.Generator{
		InputPath: absInput,
		OutputDir: absOutput,
	}

	result, err := gen.Generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating tests: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated: %s\n", result.BatsFile)
	if len(result.InputFiles) > 0 {
		fmt.Printf("Created %d fixture file(s)\n", len(result.InputFiles))
	}

	// Run the tests
	exitCode := runTests(result.BatsFile)
	os.Exit(exitCode)
}

// runTests extracts the embedded runtime to a temp directory and runs bats
func runTests(batsFile string) int {
	// Create temp directory for runtime files
	tmpDir, err := os.MkdirTemp("", "dats-runtime-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp directory: %v\n", err)
		return 1
	}
	defer os.RemoveAll(tmpDir)

	// Extract embedded runtime files
	_, err = runtime.ExtractTo(tmpDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error extracting runtime: %v\n", err)
		return 1
	}

	// Run bats with DATS_RUNTIME_DIR set
	cmd := exec.Command("bats", batsFile)
	cmd.Env = append(os.Environ(), "DATS_RUNTIME_DIR="+tmpDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("Running: bats %s\n", batsFile)
	err = cmd.Run()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "Error running bats: %v\n", err)
		return 1
	}

	return 0
}
