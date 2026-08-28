package runner

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

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

// outputsDirName is the per-instance outputs directory's name.
const outputsDirName = "outputs"

// testDirPath returns the per-instance test directory under baseDir for the
// given expanded-instance index. SetupFixtures builds the fixture tree there
// and NormalizeSnapshotText replaces the same path with {testdir}, so the
// two must always agree.
func testDirPath(baseDir string, testIndex int) string {
	return filepath.Join(baseDir, fmt.Sprintf("test-%d", testIndex))
}

// TestContext holds the paths and context for a single test execution
type TestContext struct {
	BaseDir     string            // Temp directory for this test file
	TestIndex   int               // Index of this test
	InputPaths  map[string]string // input name -> absolute path
	OutputsDir  string            // Directory {outputs.X} placeholders resolve into
	OutputPaths map[string]string // output name -> absolute path
	SharedDir   string            // File-wide directory {shared.X} placeholders resolve into

	// RemoteBase mirrors BaseDir on the ssh target; only expansion rewrites onto it.
	RemoteBase string
}

// commandPath maps a local path under BaseDir onto the ssh target.
func (c *TestContext) commandPath(local string) string {
	if c.RemoteBase == "" || local == "" {
		return local
	}
	rel, err := filepath.Rel(c.BaseDir, local)
	if err != nil || !filepath.IsLocal(rel) {
		return local
	}
	return path.Join(c.RemoteBase, filepath.ToSlash(rel))
}

// joinFixturePath joins name under dir, keeping a remote POSIX root POSIX
// even when dats itself runs on Windows.
func joinFixturePath(dir, name string) string {
	if strings.HasPrefix(dir, "/") {
		return path.Join(dir, filepath.ToSlash(name))
	}
	return filepath.Join(dir, name)
}

// SetupFixtures creates fixture files for a test and returns the context.
// Input file contents go through the same placeholder expansion as the
// command, so a fixture can reference other input paths and output paths.
// sourceDir resolves a relative inputs.copy source: the directory holding the
// .dats file being run, so a copy source is portable regardless of dats'
// working directory.
func SetupFixtures(baseDir string, testIndex int, test *schema.Test, sourceDir string) (*TestContext, error) {
	ctx := &TestContext{
		BaseDir:     baseDir,
		TestIndex:   testIndex,
		InputPaths:  make(map[string]string),
		OutputPaths: make(map[string]string),
	}

	// File names must stay inside the test directory: reject absolute paths
	// and traversal (e.g. "../../evil") before creating anything. Copy
	// destinations follow the same rule and may not collide with a files
	// name -- ParseFile already enforces both; checked again here so a
	// library caller constructing a Test directly gets the same guarantee.
	for name := range test.Inputs.Files {
		if !filepath.IsLocal(name) {
			return nil, fmt.Errorf("input file name %q must be a relative path that stays inside the test directory", name)
		}
	}
	for name, src := range test.Inputs.Copy {
		if !filepath.IsLocal(name) {
			return nil, fmt.Errorf("input copy destination %q must be a relative path that stays inside the test directory", name)
		}
		if _, dup := test.Inputs.Files[name]; dup {
			return nil, fmt.Errorf("input %q is declared under both files and copy", name)
		}
		if src == "" {
			return nil, fmt.Errorf("input copy destination %q must name a non-empty source path", name)
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

	testDir := testDirPath(baseDir, testIndex)

	// The outputs directory always exists so that every {outputs.X}
	// placeholder resolves to a writable path, whether or not a files
	// check references X.
	ctx.OutputsDir = filepath.Join(testDir, outputsDirName)
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
	// reference any declared input, regardless of map iteration order. Files
	// and Copy share one namespace (validated disjoint above) and one
	// directory, so a {inputs.X} placeholder resolves the same way whichever
	// map declared X.
	if len(test.Inputs.Files) > 0 || len(test.Inputs.Copy) > 0 {
		inputDir := filepath.Join(testDir, "inputs")
		if err := os.MkdirAll(inputDir, 0755); err != nil {
			return nil, fmt.Errorf("creating input dir: %w", err)
		}
		for name := range test.Inputs.Files {
			ctx.InputPaths[name] = filepath.Join(inputDir, name)
		}
		for name := range test.Inputs.Copy {
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

	// Copy in the host files declared under inputs.copy: the writable
	// counterpart of the sandbox's read-only bind mount of the working
	// directory, for a test that needs to modify a real file rather than
	// only read it or author its content inline.
	for name, src := range test.Inputs.Copy {
		path := ctx.InputPaths[name]
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, fmt.Errorf("creating input subdir: %w", err)
		}
		if err := copyHostFile(resolveSource(src, sourceDir), path); err != nil {
			return nil, fmt.Errorf("copying input file %s: %w", name, err)
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
		if p, ok := ctx.InputPaths[name]; ok {
			return ctx.commandPath(p)
		}
		return match // Keep original if not found
	})

	// Replace {outputs.X}
	s = outputPlaceholderRe.ReplaceAllStringFunc(s, func(match string) string {
		name := outputPlaceholderRe.FindStringSubmatch(match)[1]
		if p, ok := ctx.OutputPaths[name]; ok {
			return ctx.commandPath(p)
		}
		// Unregistered names resolve into the outputs directory only when
		// they stay inside it; traversal and absolute names stay verbatim.
		if ctx.OutputsDir != "" && filepath.IsLocal(name) {
			return joinFixturePath(ctx.commandPath(ctx.OutputsDir), name)
		}
		return match // No safe outputs path known for this placeholder
	})

	// Replace {shared.X}: like {outputs.X}, the name needs no declaration and
	// resolves into the file-wide shared directory as long as it stays local.
	return ExpandSharedPlaceholders(s, ctx.commandPath(ctx.SharedDir))
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
			return joinFixturePath(sharedDir, name)
		}
		return match // No safe shared path known for this placeholder
	})
}

// SetupSharedFixtures writes the file-level shared fixture files into
// sharedDir, in sorted name order, then copies in the file-level copy
// fixtures. Names must be local relative paths and disjoint between the two
// maps (ParseFile already enforces both; checked again here before anything
// is written, for a library caller constructing a TestFile directly); parent
// directories of nested names are created. Files contents go through
// {shared.X} expansion only -- {inputs.X}/{outputs.X} are per-test
// namespaces and stay verbatim. sourceDir resolves a relative copy source:
// the directory holding the .dats file being run.
func SetupSharedFixtures(sharedDir string, files, copyFiles map[string]string, sourceDir string) error {
	for name := range files {
		if !filepath.IsLocal(name) {
			return fmt.Errorf("shared file name %q must be a relative path that stays inside the shared directory", name)
		}
	}
	for name, src := range copyFiles {
		if !filepath.IsLocal(name) {
			return fmt.Errorf("shared copy destination %q must be a relative path that stays inside the shared directory", name)
		}
		if _, dup := files[name]; dup {
			return fmt.Errorf("shared %q is declared under both files and copy", name)
		}
		if src == "" {
			return fmt.Errorf("shared copy destination %q must name a non-empty source path", name)
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
	for _, name := range sortedStringKeys(copyFiles) {
		path := filepath.Join(sharedDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("creating shared subdir: %w", err)
		}
		if err := copyHostFile(resolveSource(copyFiles[name], sourceDir), path); err != nil {
			return fmt.Errorf("copying shared file %s: %w", name, err)
		}
	}
	return nil
}

// resolveSource resolves a copy fixture's host source path: absolute paths
// are used as-is, and a relative path resolves against sourceDir -- the
// directory holding the .dats file being run -- so a copy source is portable
// regardless of dats' own working directory.
func resolveSource(src, sourceDir string) string {
	if filepath.IsAbs(src) || sourceDir == "" {
		return src
	}
	return filepath.Join(sourceDir, src)
}

// copyHostFile copies src into dest, writable, preserving src's permission
// bits (so a fixture script pulled in via copy keeps its executable bit).
// This is the read-write counterpart of the sandbox's read-only bind mount of
// the working directory: a way to pull an existing host file into the
// sandbox for a test to modify, without inlining its content as YAML text.
func copyHostFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory, not a file", src)
	}
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Cleanup removes the fixture directory
func Cleanup(baseDir string) error {
	return os.RemoveAll(baseDir)
}
