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
// Unknown keys are rejected: a misspelled field (e.g. "stdotu") would otherwise be
// silently dropped, leaving its assertion unenforced. A second "---" document is a
// parse error too (yaml-fixed's Parse rejects a multi-document stream outright) rather
// than being silently dropped.
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

	// A shared block must actually declare a fixture (files or copy), and
	// every name must stay inside the shared directory (the same rule as
	// inputs.files names) and appear in exactly one of the two maps. The
	// runner enforces locality again when writing/copying the files.
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

	// {matrix.X} belongs to a single test instance; setup and teardown
	// commands and shared file contents run once per file, where no instance
	// exists, so a matrix placeholder there can never resolve. A hook
	// command's cmd, its env values, and its stdin_file are all file-level
	// too, so all three are scanned.
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
	// The sandbox block is file-level too: it is resolved once, before any
	// instance exists, so a {matrix.X} in its image could never resolve
	// either.
	if testFile.Sandbox != nil {
		if name, found := findMatrixPlaceholder(testFile.Sandbox.Image); found {
			return nil, fmt.Errorf("sandbox image: {matrix.%s} is not available outside tests", name)
		}
	}
	// The ssh target is file-level for the same reason: it is resolved once,
	// before any instance exists.
	if testFile.SSH != nil {
		if name, found := findMatrixPlaceholder(testFile.SSH.Target); found {
			return nil, fmt.Errorf("ssh target: {matrix.%s} is not available outside tests", name)
		}
	}

	for i, test := range testFile.Tests {
		if test.Cmd == "" {
			return nil, fmt.Errorf("test %d: missing required field 'cmd'", i+1)
		}
		if msg := bannedRedirect(test.Cmd); msg != "" {
			return nil, fmt.Errorf("test %d: cmd: %s", i+1, msg)
		}
		// Fixture file names must stay inside the test directory. The runner
		// enforces this again at fixture-setup time; checking here lets
		// `dats syntax` catch it without running anything.
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
		// Every {matrix.X} reference in the test's substitution scope must
		// name a variable declared by THIS test's matrix, so `dats syntax`
		// catches typos without running anything. (Fixture file names and env
		// var names are outside the scope and are not scanned.)
		if err := validateMatrixRefs(&test); err != nil {
			return nil, fmt.Errorf("test %d: %w", i+1, err)
		}
	}

	return &testFile, nil
}

// validateCopyBlock validates a copy fixture map (inputs.copy or
// shared.copy): every destination name must be local (the same rule as a
// files name -- no ".." or absolute paths) and must not also appear in
// filesMap, since a name can have only one source of content. Source paths
// are not checked for existence here: a per-test copy source may still
// contain an unexpanded {matrix.X} at this point, and the runner is where a
// missing source fails loudly. context names the block ("shared" or
// "test N") in every error.
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
