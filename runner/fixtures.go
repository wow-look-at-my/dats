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

const sharedDirName = "shared"

// outputsDirName is the per-instance outputs directory's name.
const outputsDirName = "outputs"

// testDirPath returns the per-instance test directory under baseDir for the given expanded-instance index.
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

func joinFixturePath(dir, name string) string {
	if strings.HasPrefix(dir, "/") {
		return path.Join(dir, filepath.ToSlash(name))
	}
	return filepath.Join(dir, name)
}

// SetupFixtures creates fixture files for a test and returns the context.
func SetupFixtures(baseDir string, testIndex int, test *schema.Test, sourceDir string) (*TestContext, error) {
	ctx := &TestContext{
		BaseDir:     baseDir,
		TestIndex:   testIndex,
		InputPaths:  make(map[string]string),
		OutputPaths: make(map[string]string),
	}

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

	ctx.OutputsDir = filepath.Join(testDir, outputsDirName)
	if err := os.MkdirAll(ctx.OutputsDir, 0755); err != nil {
		return nil, fmt.Errorf("creating output dir: %w", err)
	}

	ctx.SharedDir = filepath.Join(baseDir, sharedDirName)
	if err := os.MkdirAll(ctx.SharedDir, 0755); err != nil {
		return nil, fmt.Errorf("creating shared dir: %w", err)
	}

	for name := range test.Outputs.Files {
		ctx.OutputPaths[name] = filepath.Join(ctx.OutputsDir, name)
	}
	for name := range test.Outputs.NotFiles {
		ctx.OutputPaths[name] = filepath.Join(ctx.OutputsDir, name)
	}

	for _, path := range ctx.OutputPaths {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, fmt.Errorf("creating output subdir: %w", err)
		}
	}

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

// ExpandPlaceholders replaces {inputs.X}, {outputs.X}, and {shared.X} with actual paths.
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
		if ctx.OutputsDir != "" && filepath.IsLocal(name) {
			return joinFixturePath(ctx.commandPath(ctx.OutputsDir), name)
		}
		return match // No safe outputs path known for this placeholder
	})

	return ExpandSharedPlaceholders(s, ctx.commandPath(ctx.SharedDir))
}

// ExpandSharedPlaceholders replaces only {shared.X} placeholders, resolving X into sharedDir.
func ExpandSharedPlaceholders(s, sharedDir string) string {
	return sharedPlaceholderRe.ReplaceAllStringFunc(s, func(match string) string {
		name := sharedPlaceholderRe.FindStringSubmatch(match)[1]
		if sharedDir != "" && filepath.IsLocal(name) {
			return joinFixturePath(sharedDir, name)
		}
		return match // No safe shared path known for this placeholder
	})
}

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

func resolveSource(src, sourceDir string) string {
	if filepath.IsAbs(src) || sourceDir == "" {
		return src
	}
	return filepath.Join(sourceDir, src)
}

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
