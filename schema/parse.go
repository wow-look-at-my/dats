package schema

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ParseFile reads and parses a .dats file, returning the parsed TestFile or an error.
func ParseFile(path string) (*TestFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading input file: %w", err)
	}

	var testFile TestFile
	if err := yaml.Unmarshal(data, &testFile); err != nil {
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
