package schema

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"
)

// ParseFile reads and parses a .dats file, returning the parsed TestFile or an error.
// Unknown keys are rejected: a misspelled field (e.g. "stdotu") would otherwise be
// silently dropped, leaving its assertion unenforced.
func ParseFile(path string) (*TestFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading input file: %w", err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var testFile TestFile
	if err := dec.Decode(&testFile); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	// Only the first "---" document is decoded above; anything after it would
	// be silently dropped, so reject it instead.
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("multiple YAML documents are not supported")
	}

	if len(testFile.Tests) == 0 {
		return nil, fmt.Errorf("no tests defined")
	}

	// A shared block must actually declare files, and their names must stay
	// inside the shared directory (the same rule as inputs.files names). The
	// runner enforces locality again when writing the files.
	if testFile.Shared != nil {
		if len(testFile.Shared.Files) == 0 {
			return nil, fmt.Errorf("shared: must declare at least one file under files")
		}
		for name := range testFile.Shared.Files {
			if !filepath.IsLocal(name) {
				return nil, fmt.Errorf("shared file name %q must be a relative path that stays inside the shared directory", name)
			}
		}
	}

	// {matrix.X} belongs to a single test instance; setup and teardown
	// commands and shared file contents run once per file, where no instance
	// exists, so a matrix placeholder there can never resolve.
	for _, hook := range []struct {
		kind string
		cmds []string
	}{
		{"setup", testFile.Setup},
		{"teardown", testFile.Teardown},
	} {
		for i, cmd := range hook.cmds {
			if name, found := findMatrixPlaceholder(cmd); found {
				return nil, fmt.Errorf("%s command %d: {matrix.%s} is not available outside tests", hook.kind, i+1, name)
			}
		}
	}
	if testFile.Shared != nil {
		for _, name := range slices.Sorted(maps.Keys(testFile.Shared.Files)) {
			if ref, found := findMatrixPlaceholder(testFile.Shared.Files[name]); found {
				return nil, fmt.Errorf("shared file %q: {matrix.%s} is not available outside tests", name, ref)
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

	for i, test := range testFile.Tests {
		if test.Cmd == "" {
			return nil, fmt.Errorf("test %d: missing required field 'cmd'", i+1)
		}
		// Fixture file names must stay inside the test directory. The runner
		// enforces this again at fixture-setup time; checking here lets
		// `dats syntax` catch it without running anything.
		for name := range test.Inputs.Files {
			if !filepath.IsLocal(name) {
				return nil, fmt.Errorf("test %d: input file name %q must be a relative path that stays inside the test directory", i+1, name)
			}
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
