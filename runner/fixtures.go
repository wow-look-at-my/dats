package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/wow-look-at-my/dats/schema"
)

// Placeholder patterns are compiled once and reused across expansions.
var (
	inputPlaceholderRe  = regexp.MustCompile(`\{inputs\.([^}]+)\}`)
	outputPlaceholderRe = regexp.MustCompile(`\{outputs\.([^}]+)\}`)
)

// TestContext holds the paths and context for a single test execution
type TestContext struct {
	BaseDir     string            // Temp directory for this test file
	TestIndex   int               // Index of this test
	InputPaths  map[string]string // input name -> absolute path
	OutputsDir  string            // Directory {outputs.X} placeholders resolve into
	OutputPaths map[string]string // output name -> absolute path
}

// SetupFixtures creates fixture files for a test and returns the context.
// Input file contents go through the same placeholder expansion as the
// command, so a fixture can reference other input paths and output paths.
func SetupFixtures(baseDir string, testIndex int, test *schema.Test) (*TestContext, error) {
	ctx := &TestContext{
		BaseDir:     baseDir,
		TestIndex:   testIndex,
		InputPaths:  make(map[string]string),
		OutputPaths: make(map[string]string),
	}

	// File names must stay inside the test directory: reject absolute paths
	// and traversal (e.g. "../../evil") before creating anything.
	for name := range test.Inputs.Files {
		if !filepath.IsLocal(name) {
			return nil, fmt.Errorf("input file name %q must be a relative path that stays inside the test directory", name)
		}
	}
	for name := range test.Outputs.Files {
		if !filepath.IsLocal(name) {
			return nil, fmt.Errorf("output file name %q must be a relative path that stays inside the test directory", name)
		}
	}
	for name := range test.Outputs.NotFiles {
		if !filepath.IsLocal(name) {
			return nil, fmt.Errorf("output file name %q must be a relative path that stays inside the test directory", name)
		}
	}

	testDir := filepath.Join(baseDir, fmt.Sprintf("test-%d", testIndex))

	// The outputs directory always exists so that every {outputs.X}
	// placeholder resolves to a writable path, whether or not a files
	// check references X.
	ctx.OutputsDir = filepath.Join(testDir, "outputs")
	if err := os.MkdirAll(ctx.OutputsDir, 0755); err != nil {
		return nil, fmt.Errorf("creating output dir: %w", err)
	}

	// Pre-register the output paths named by `files` and `!files` checks so
	// their assertions resolve consistently with the placeholders.
	for name := range test.Outputs.Files {
		ctx.OutputPaths[name] = filepath.Join(ctx.OutputsDir, name)
	}
	for name := range test.Outputs.NotFiles {
		ctx.OutputPaths[name] = filepath.Join(ctx.OutputsDir, name)
	}

	// Create parent directories of registered outputs (e.g. sub/out.txt) so
	// commands can write nested output files directly.
	for _, path := range ctx.OutputPaths {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, fmt.Errorf("creating output subdir: %w", err)
		}
	}

	// Register every input path before writing any file so that contents can
	// reference any declared input, regardless of map iteration order.
	if len(test.Inputs.Files) > 0 {
		inputDir := filepath.Join(testDir, "inputs")
		if err := os.MkdirAll(inputDir, 0755); err != nil {
			return nil, fmt.Errorf("creating input dir: %w", err)
		}
		for name := range test.Inputs.Files {
			ctx.InputPaths[name] = filepath.Join(inputDir, name)
		}
	}

	// Write the input files with placeholders expanded in their contents.
	for name, content := range test.Inputs.Files {
		path := ctx.InputPaths[name]
		// Create parent directories if needed (for nested file paths)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, fmt.Errorf("creating input subdir: %w", err)
		}
		if err := os.WriteFile(path, []byte(ExpandPlaceholders(content, ctx)), 0644); err != nil {
			return nil, fmt.Errorf("writing input file %s: %w", name, err)
		}
	}

	return ctx, nil
}

// ExpandPlaceholders replaces {inputs.X} and {outputs.X} with actual paths.
// It is applied to the command and to input file contents. {inputs.X} for an
// undeclared input X is left untouched; {outputs.X} resolves to a path under
// the test's outputs directory as long as X stays inside it (non-local names
// are left untouched). Text that is not a placeholder (any other brace
// construct) passes through unchanged.
func ExpandPlaceholders(s string, ctx *TestContext) string {
	// Replace {inputs.X}
	s = inputPlaceholderRe.ReplaceAllStringFunc(s, func(match string) string {
		name := inputPlaceholderRe.FindStringSubmatch(match)[1]
		if path, ok := ctx.InputPaths[name]; ok {
			return path
		}
		return match // Keep original if not found
	})

	// Replace {outputs.X}
	s = outputPlaceholderRe.ReplaceAllStringFunc(s, func(match string) string {
		name := outputPlaceholderRe.FindStringSubmatch(match)[1]
		if path, ok := ctx.OutputPaths[name]; ok {
			return path
		}
		// Unregistered names resolve into the outputs directory only when
		// they stay inside it; traversal and absolute names stay verbatim.
		if ctx.OutputsDir != "" && filepath.IsLocal(name) {
			return filepath.Join(ctx.OutputsDir, name)
		}
		return match // No safe outputs path known for this placeholder
	})

	return s
}

// Cleanup removes the fixture directory
func Cleanup(baseDir string) error {
	return os.RemoveAll(baseDir)
}
