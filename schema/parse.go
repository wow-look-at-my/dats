package schema

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	yamlfixed "github.com/wow-look-at-my/yaml-fixed/yaml"
)

// ParseFile reads and parses a .dats file, returning the parsed TestFile or an error.
func ParseFile(path string) (*TestFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading input file: %w", err)
	}

	var testFile TestFile
	if err := yamlfixed.UnmarshalStrict(data, &testFile); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	if len(testFile.Tests) == 0 {
		return nil, fmt.Errorf("no tests defined")
	}

	if testFile.Shared != nil {
		if len(testFile.Shared.Files) == 0 && len(testFile.Shared.Copy) == 0 {
			return nil, fmt.Errorf("shared: must declare at least one file under files or copy")
		}
		for name := range testFile.Shared.Files {
			if !filepath.IsLocal(name) {
				return nil, fmt.Errorf("shared file name %q must be a relative path that stays inside the shared directory", name)
			}
		}
		if err := validateCopyBlock(testFile.Shared.Copy, testFile.Shared.Files, "shared"); err != nil {
			return nil, err
		}
	}

	for _, hook := range []struct {
		kind string
		cmds []HookCommand
	}{
		{"setup", testFile.Setup},
		{"teardown", testFile.Teardown},
	} {
		for i, hc := range hook.cmds {
			if name, found := findMatrixPlaceholder(hc.Cmd); found {
				return nil, fmt.Errorf("%s command %d: {matrix.%s} is not available outside tests", hook.kind, i+1, name)
			}
			for _, envName := range slices.Sorted(maps.Keys(hc.Env)) {
				if name, found := findMatrixPlaceholder(hc.Env[envName]); found {
					return nil, fmt.Errorf("%s command %d: env %q: {matrix.%s} is not available outside tests", hook.kind, i+1, envName, name)
				}
			}
			if name, found := findMatrixPlaceholder(hc.StdinFile); found {
				return nil, fmt.Errorf("%s command %d: stdin_file: {matrix.%s} is not available outside tests", hook.kind, i+1, name)
			}
		}
	}
	if testFile.Shared != nil {
		for _, name := range slices.Sorted(maps.Keys(testFile.Shared.Files)) {
			if ref, found := findMatrixPlaceholder(testFile.Shared.Files[name]); found {
				return nil, fmt.Errorf("shared file %q: {matrix.%s} is not available outside tests", name, ref)
			}
		}
		for _, name := range slices.Sorted(maps.Keys(testFile.Shared.Copy)) {
			if ref, found := findMatrixPlaceholder(testFile.Shared.Copy[name]); found {
				return nil, fmt.Errorf("shared copy %q: {matrix.%s} is not available outside tests", name, ref)
			}
		}
	}
	if testFile.Sandbox != nil {
		if name, found := findMatrixPlaceholder(testFile.Sandbox.Image); found {
			return nil, fmt.Errorf("sandbox image: {matrix.%s} is not available outside tests", name)
		}
	}
	// The ssh target is file-level for the same reason: it is resolved a single time, before any instance exists.
	if testFile.SSH != nil {
		if name, found := findMatrixPlaceholder(testFile.SSH.Target); found {
			return nil, fmt.Errorf("ssh target: {matrix.%s} is not available outside tests", name)
		}
	}

	for i, test := range testFile.Tests {
		if test.Cmd == "" {
			return nil, fmt.Errorf("test %d: missing required field 'cmd'", i+1)
		}
		// A per-test target only moves a command between remote hosts.
		if test.SSH != nil && testFile.SSH == nil {
			return nil, fmt.Errorf("test %d: ssh: a per-test target needs a file-level ssh: target too (setup, teardown and shared/ always run on the file's target)", i+1)
		}
		if msg := bannedRedirect(test.Cmd); msg != "" {
			return nil, fmt.Errorf("test %d: cmd: %s", i+1, msg)
		}
		// Fixture file names must stay inside the test directory.
		for name := range test.Inputs.Files {
			if !filepath.IsLocal(name) {
				return nil, fmt.Errorf("test %d: input file name %q must be a relative path that stays inside the test directory", i+1, name)
			}
		}
		if err := validateCopyBlock(test.Inputs.Copy, test.Inputs.Files, fmt.Sprintf("test %d", i+1)); err != nil {
			return nil, err
		}
		for name := range test.Outputs.Files {
			if !filepath.IsLocal(name) {
				return nil, fmt.Errorf("test %d: output file name %q must be a relative path that stays inside the test directory", i+1, name)
			}
		}
		for name := range test.Outputs.NotFiles {
			if !filepath.IsLocal(name) {
				return nil, fmt.Errorf("test %d: output file name %q must be a relative path that stays inside the test directory", i+1, name)
			}
		}
		if err := validateOutputAssertions(&test.Outputs); err != nil {
			return nil, fmt.Errorf("test %d: %w", i+1, err)
		}
		if err := validateMatrixRefs(&test); err != nil {
			return nil, fmt.Errorf("test %d: %w", i+1, err)
		}
	}

	return &testFile, nil
}

// validateOutputAssertions rejects an outputs key that names a stream or a file
// set and then asserts nothing about it.
//
// dats reports such a test as ok, and the reader sees a key that says the output
// was checked. Omitting the key runs the same assertions and claims nothing, so
// the empty form only ever misleads. Either way the test still checks the exit code.
func validateOutputAssertions(out *OutputBlock) error {
	for _, check := range []struct {
		key   string
		check OutputCheck
	}{
		{"stdout", out.Stdout},
		{"stderr", out.Stderr},
		{"!stdout", out.NotStdout},
		{"!stderr", out.NotStderr},
	} {
		if check.check.Stated && check.check.IsEmpty() {
			return fmt.Errorf("outputs.%s: an empty check asserts nothing -- write a pattern, or drop the key", check.key)
		}
	}
	for _, files := range []struct {
		key   string
		files map[string]FileCheck
	}{
		{"files", out.Files},
		{"!files", out.NotFiles},
	} {
		if files.files != nil && len(files.files) == 0 {
			return fmt.Errorf("outputs.%s: an empty mapping asserts nothing -- name a file, or drop the key", files.key)
		}
	}
	return nil
}

func validateCopyBlock(copyMap, filesMap map[string]string, context string) error {
	for name, src := range copyMap {
		if !filepath.IsLocal(name) {
			return fmt.Errorf("%s: copy destination %q must be a relative path that stays inside the fixture directory", context, name)
		}
		if strings.TrimSpace(src) == "" {
			return fmt.Errorf("%s: copy destination %q must name a non-empty source path", context, name)
		}
		if _, dup := filesMap[name]; dup {
			return fmt.Errorf("%s: %q is declared under both files and copy", context, name)
		}
	}
	return nil
}
