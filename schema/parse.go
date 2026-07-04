package schema

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

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

	if len(testFile.Tests) == 0 {
		return nil, fmt.Errorf("no tests defined")
	}

	for i, test := range testFile.Tests {
		if test.Cmd == "" {
			return nil, fmt.Errorf("test %d: missing required field 'cmd'", i+1)
		}
	}

	return &testFile, nil
}
