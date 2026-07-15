package schema

// Regression tests for parsing fixes: duplicate and negative line-check keys,
// quoted integer exit codes, quoted bare-integer timeouts, and empty file
// checks.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestOutputCheck_UnmarshalYAML_DuplicateLineKeys(t *testing.T) {
	// The custom unmarshaler iterates the mapping node directly, bypassing
	// yaml.v3's duplicate-key detection; without an explicit check the later
	// entry silently wins.
	var o OutputCheck
	err := yaml.Unmarshal([]byte("0: \"a\"\n0: \"b\"\n"), &o)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "duplicate line number 0")
}

func TestOutputCheck_UnmarshalYAML_DuplicateLineKeysDifferentSpelling(t *testing.T) {
	// "0" and "00" are the same line number.
	var o OutputCheck
	err := yaml.Unmarshal([]byte("0: \"a\"\n00: \"b\"\n"), &o)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "duplicate line number 0")
}

func TestOutputCheck_UnmarshalYAML_NegativeLineKey(t *testing.T) {
	// Negative line numbers are nonsense: a positive check on line -1 always
	// fails and a negated check silently passes. schema.json forbids them too.
	var o OutputCheck
	err := yaml.Unmarshal([]byte("-1: \"never\"\n"), &o)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "line number must be >= 0, got -1")
}

func TestExitCode_UnmarshalYAML_QuotedInt(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{`"0"`, 0},
		{`"3"`, 3},
		{`"255"`, 255},
		// Atoi treats a leading zero as decimal, not octal.
		{`"012"`, 12},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			var e ExitCode
			err := yaml.Unmarshal([]byte(tt.input), &e)
			require.Nil(t, err)
			assert.Equal(t, tt.want, e.Value)
			assert.Equal(t, "", e.Variable)
		})
	}
}

func TestExitCode_UnmarshalYAML_QuotedIntOutOfRange(t *testing.T) {
	// Quoted integers get the same range validation as bare integers.
	for _, input := range []string{`"256"`, `"-1"`, `"1000"`} {
		t.Run(input, func(t *testing.T) {
			var e ExitCode
			err := yaml.Unmarshal([]byte(input), &e)
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), "must be in range 0-255")
		})
	}
}

func TestExitCode_UnmarshalYAML_NonNumericStringKeepsError(t *testing.T) {
	// Unknown non-numeric strings keep the exit-code-name error.
	var e ExitCode
	err := yaml.Unmarshal([]byte(`"EXIT_BOGUS"`), &e)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "not a recognized exit code name")
}

func TestDuration_UnmarshalYAML_QuotedInt(t *testing.T) {
	cases := []struct {
		input string
		want  time.Duration
	}{
		{`"5"`, 5 * time.Second},
		{`"0"`, 0},
		{`"90"`, 90 * time.Second},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			var d Duration
			err := yaml.Unmarshal([]byte(tt.input), &d)
			require.Nil(t, err)
			assert.Equal(t, tt.want, d.Value)
		})
	}
}

func TestDuration_UnmarshalYAML_QuotedNegativeInt(t *testing.T) {
	var d Duration
	err := yaml.Unmarshal([]byte(`"-5"`), &d)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "must not be negative")
}

func TestDuration_UnmarshalYAML_NonNumericStringKeepsError(t *testing.T) {
	var d Duration
	err := yaml.Unmarshal([]byte(`"banana"`), &d)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), `invalid timeout "banana"`)
}

func TestFileCheck_IsEmpty(t *testing.T) {
	boolTrue := true
	cases := []struct {
		name  string
		check FileCheck
		want  bool
	}{
		{"empty", FileCheck{}, true},
		{"with exists", FileCheck{Exists: &boolTrue}, false},
		{"with match", FileCheck{Match: []string{"a"}}, false},
		{"with notMatch", FileCheck{NotMatch: []string{"a"}}, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.check.IsEmpty())
		})
	}
}

func TestFileCheck_UnmarshalYAML_NullValueIsEmpty(t *testing.T) {
	// A null file check ("out.txt:" with no value) decodes to the zero
	// FileCheck, which the runner treats as an implicit existence assertion.
	var o OutputBlock
	err := yaml.Unmarshal([]byte("files:\n  out.txt:\n"), &o)
	require.Nil(t, err)
	check, ok := o.Files["out.txt"]
	require.True(t, ok)
	assert.True(t, check.IsEmpty())
}
