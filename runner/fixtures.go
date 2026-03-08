package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/mhaynie/bats-declarative/schema"
)

// TestContext holds the paths and context for a single test execution
type TestContext struct {
	BaseDir     string            // Temp directory for this test file
	TestIndex   int               // Index of this test
	InputPaths  map[string]string // input name -> absolute path
	OutputPaths map[string]string // output name -> absolute path
}

// SetupFixtures creates fixture files for a test and returns the context
func SetupFixtures(baseDir string, testIndex int, test *schema.Test) (*TestContext, error) {
	ctx := &TestContext{
		BaseDir:     baseDir,
		TestIndex:   testIndex,
		InputPaths:  make(map[string]string),
		OutputPaths: make(map[string]string),
	}

	testDir := filepath.Join(baseDir, fmt.Sprintf("test-%d", testIndex))

	// Create input files
	if len(test.Inputs.Files) > 0 {
		inputDir := filepath.Join(testDir, "inputs")
		if err := os.MkdirAll(inputDir, 0755); err != nil {
			return nil, fmt.Errorf("creating input dir: %w", err)
		}

		for name, content := range test.Inputs.Files {
			path := filepath.Join(inputDir, name)
			// Create parent directories if needed (for nested file paths)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return nil, fmt.Errorf("creating input subdir: %w", err)
			}
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return nil, fmt.Errorf("writing input file %s: %w", name, err)
			}
			ctx.InputPaths[name] = path
		}
	}

	// Set up output file paths (create directories but not files)
	if len(test.Outputs.Files) > 0 {
		outputDir := filepath.Join(testDir, "outputs")
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return nil, fmt.Errorf("creating output dir: %w", err)
		}

		for name := range test.Outputs.Files {
			ctx.OutputPaths[name] = filepath.Join(outputDir, name)
		}
	}

	return ctx, nil
}

// ExpandPlaceholders replaces {inputs.X} and {outputs.X} with actual paths
func ExpandPlaceholders(cmd string, ctx *TestContext) string {
	// Replace {inputs.X}
	inputRe := regexp.MustCompile(`\{inputs\.([^}]+)\}`)
	cmd = inputRe.ReplaceAllStringFunc(cmd, func(match string) string {
		name := inputRe.FindStringSubmatch(match)[1]
		if path, ok := ctx.InputPaths[name]; ok {
			return path
		}
		return match // Keep original if not found
	})

	// Replace {outputs.X}
	outputRe := regexp.MustCompile(`\{outputs\.([^}]+)\}`)
	cmd = outputRe.ReplaceAllStringFunc(cmd, func(match string) string {
		name := outputRe.FindStringSubmatch(match)[1]
		if path, ok := ctx.OutputPaths[name]; ok {
			return path
		}
		return match // Keep original if not found
	})

	return cmd
}

// Cleanup removes the fixture directory
func Cleanup(baseDir string) error {
	return os.RemoveAll(baseDir)
}
