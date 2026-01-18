package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mhaynie/bats-declarative/internal/generator"
)

const usage = `dats - Declarative Automated Testing System

Usage: dats <file.dats> [output_dir] [--runtime-dir=<path>]

Arguments:
  file.dats    Input .dats file to convert
  output_dir   Output directory for generated files (default: same as input)

Options:
  --runtime-dir=<path>  Path to runtime directory containing test_helper.bash
                        (default: ./runtime or alongside the dats binary)

Examples:
  dats tests.dats                    # Generate tests.gen.bats in current dir
  dats tests.dats ./output           # Generate in ./output directory
  dats tests.dats ./output --runtime-dir=/path/to/runtime
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
	if len(os.Args) >= 3 && os.Args[2][0] != '-' {
		outputDir = os.Args[2]
	}

	// Create output directory if needed
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	// Determine runtime directory
	runtimeDir := findRuntimeDir()
	for _, arg := range os.Args[2:] {
		if len(arg) > 14 && arg[:14] == "--runtime-dir=" {
			runtimeDir = arg[14:]
		}
	}

	if runtimeDir == "" {
		fmt.Fprintf(os.Stderr, "Error: could not find runtime directory\n")
		fmt.Fprintf(os.Stderr, "Specify with --runtime-dir=<path>\n")
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

	absRuntime, err := filepath.Abs(runtimeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	gen := &generator.Generator{
		InputPath:  absInput,
		OutputDir:  absOutput,
		RuntimeDir: absRuntime,
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
}

// findRuntimeDir looks for the runtime directory in common locations
func findRuntimeDir() string {
	// Check relative to current directory
	if _, err := os.Stat("runtime/test_helper.bash"); err == nil {
		return "runtime"
	}

	// Check relative to executable
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		runtimePath := filepath.Join(exeDir, "runtime")
		if _, err := os.Stat(filepath.Join(runtimePath, "test_helper.bash")); err == nil {
			return runtimePath
		}
		// Also check one level up (for when binary is in bin/)
		runtimePath = filepath.Join(exeDir, "..", "runtime")
		if _, err := os.Stat(filepath.Join(runtimePath, "test_helper.bash")); err == nil {
			return runtimePath
		}
	}

	return ""
}
