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
	sharedPlaceholderRe = regexp.MustCompile(`\{shared\.([^}]+)\}`)
)

// sharedDirName is the file-wide shared directory's name under the temp
// directory: RunFile creates it there and {shared.X} placeholders resolve
// into it, so the two must always agree.
const sharedDirName = "shared"

// TestContext holds the paths and context for a single test execution
type TestContext struct {
	BaseDir     string            // Temp directory for this test file
	TestIndex   int               // Index of this test
	InputPaths  map[string]string // input name -> absolute path
	OutputsDir  string            // Directory {outputs.X} placeholders resolve into
	OutputPaths map[string]string // output name -> absolute path
	SharedDir   string            // File-wide directory {shared.X} placeholders resolve into
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

	// The file-wide shared directory sits alongside the per-test directories;
	// {shared.X} placeholders resolve into it. RunFile creates it before
	// setup runs, but it is ensured here too so the placeholders resolve to a
	// writable path even when RunTest is driven directly.
	ctx.SharedDir = filepath.Join(baseDir, sharedDirName)
	if err := os.MkdirAll(ctx.SharedDir, 0755); err != nil {
		return nil, fmt.Errorf("creating shared dir: %w", err)
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

// ExpandPlaceholders replaces {inputs.X}, {outputs.X}, and {shared.X} with
// actual paths. It is applied to the command, to input file contents, and to
// inputs.env values. {inputs.X} for an undeclared input X is left untouched;
// {outputs.X} resolves to a path under the test's outputs directory as long
// as X stays inside it (non-local names are left untouched); {shared.X}
// resolves into the file-wide shared directory under the same rule. Text that
// is not a placeholder (any other brace construct) passes through unchanged.
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

	// Replace {shared.X}: like {outputs.X}, the name needs no declaration and
	// resolves into the file-wide shared directory as long as it stays local.
	return ExpandSharedPlaceholders(s, ctx.SharedDir)
}

// ExpandSharedPlaceholders replaces only {shared.X} placeholders, resolving X
// into sharedDir. It is the sole expansion applied to setup and teardown
// commands and to shared file contents, where the per-test
// {inputs.X}/{outputs.X} namespaces do not exist (those placeholders pass
// through verbatim there). Like {outputs.X}, X needs no declaration but must
// be a local relative name: traversal and absolute names are left untouched,
// so a placeholder can never address a path outside the shared directory. An
// empty sharedDir leaves every {shared.X} untouched.
func ExpandSharedPlaceholders(s, sharedDir string) string {
	return sharedPlaceholderRe.ReplaceAllStringFunc(s, func(match string) string {
		name := sharedPlaceholderRe.FindStringSubmatch(match)[1]
		if sharedDir != "" && filepath.IsLocal(name) {
			return filepath.Join(sharedDir, name)
		}
		return match // No safe shared path known for this placeholder
	})
}

// SetupSharedFixtures writes the file-level shared fixture files into
// sharedDir, in sorted name order. File names must be local relative paths
// (ParseFile already enforces this; it is checked again here before anything
// is written); parent directories of nested names are created. Contents go
// through {shared.X} expansion only -- {inputs.X}/{outputs.X} are per-test
// namespaces and stay verbatim.
func SetupSharedFixtures(sharedDir string, files map[string]string) error {
	for name := range files {
		if !filepath.IsLocal(name) {
			return fmt.Errorf("shared file name %q must be a relative path that stays inside the shared directory", name)
		}
	}
	for _, name := range sortedStringKeys(files) {
		path := filepath.Join(sharedDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("creating shared subdir: %w", err)
		}
		if err := os.WriteFile(path, []byte(ExpandSharedPlaceholders(files[name], sharedDir)), 0644); err != nil {
			return fmt.Errorf("writing shared file %s: %w", name, err)
		}
	}
	return nil
}

// Cleanup removes the fixture directory
func Cleanup(baseDir string) error {
	return os.RemoveAll(baseDir)
}
