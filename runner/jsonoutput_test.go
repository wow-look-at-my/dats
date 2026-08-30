package runner

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssertJSONOutputMatch(t *testing.T) {
	expected := map[string]any{
		"name":  "dats",
		"count": 2,
		"tags":  []any{"a", "b"},
		"meta":  map[string]any{"ok": true, "note": nil},
	}
	stdout := `{"meta": {"note": null, "ok": true}, "tags": ["a", "b"], "count": 2, "name": "dats"}` + "\n"

	// Object key order differs between expected and actual: still equal.
	assert.Empty(t, AssertJSONOutput(stdout, expected))
}

func TestAssertJSONOutputNumbersByValue(t *testing.T) {
	// 2 and 2.0 are the same JSON number
	assert.Empty(t, AssertJSONOutput("2.0", 2))
	assert.Empty(t, AssertJSONOutput("2", 2.0))
	assert.NotEmpty(t, AssertJSONOutput("2.5", 2))
}

func TestAssertJSONOutputScalarAndNull(t *testing.T) {
	assert.Empty(t, AssertJSONOutput(`"hello"`, "hello"))
	assert.Empty(t, AssertJSONOutput("true", true))
	assert.Empty(t, AssertJSONOutput("null", nil))
	assert.NotEmpty(t, AssertJSONOutput(`"hello"`, "world"))
	assert.NotEmpty(t, AssertJSONOutput(`"null"`, nil))
}

func TestAssertJSONOutputArrayOrderSensitive(t *testing.T) {
	errs := AssertJSONOutput(`[2, 1]`, []any{1, 2})
	require.Equal(t, 2, len(errs))
	assert.Contains(t, errs[0].Error(), "at $[0]: expected 1, got 2")
	assert.Contains(t, errs[1].Error(), "at $[1]: expected 2, got 1")
}

func TestAssertJSONOutputStructuralDiff(t *testing.T) {
	expected := map[string]any{
		"kind":  "Ident",
		"value": 3,
		"tags":  []any{"x"},
	}
	stdout := `{"kind": "Keyword", "value": 3, "tags": ["x", "y"], "extra": 1}`

	errs := AssertJSONOutput(stdout, expected)
	require.NotEmpty(t, errs)
	var msgs []string
	for _, err := range errs {
		msgs = append(msgs, err.Error())
	}
	joined := ""
	for _, m := range msgs {
		joined += m + "\n"
	}
	assert.Contains(t, joined, `at $.kind: expected "Ident", got "Keyword"`)
	assert.Contains(t, joined, "at $.tags: expected array of 1 elements, got 2")
	assert.Contains(t, joined, `unexpected key "extra"`)
}

func TestAssertJSONOutputMissingKey(t *testing.T) {
	errs := AssertJSONOutput(`{}`, map[string]any{"needed": 1})
	require.Equal(t, 1, len(errs))
	assert.Contains(t, errs[0].Error(), `missing key "needed"`)
	assert.Contains(t, errs[0].Error(), "expected 1")
}

func TestAssertJSONOutputTypeMismatch(t *testing.T) {
	errs := AssertJSONOutput(`{"a": "1"}`, map[string]any{"a": 1})
	require.Equal(t, 1, len(errs))
	assert.Contains(t, errs[0].Error(), "at $.a: expected number 1, got string \"1\"")
}

func TestAssertJSONOutputInvalidJSON(t *testing.T) {
	errs := AssertJSONOutput("not json at all", map[string]any{"a": 1})
	require.Equal(t, 1, len(errs))
	assert.Contains(t, errs[0].Error(), "stdout is not valid JSON")
}

func TestAssertJSONOutputTrailingData(t *testing.T) {
	// A trailing newline is fine
	assert.Empty(t, AssertJSONOutput("{\"a\": 1}\n", map[string]any{"a": 1}))
	// A second JSON value is not
	errs := AssertJSONOutput(`{"a": 1} {"b": 2}`, map[string]any{"a": 1})
	require.Equal(t, 1, len(errs))
	assert.Contains(t, errs[0].Error(), "trailing data")
}

func TestAssertJSONOutputDiffCap(t *testing.T) {
	expected := make(map[string]any)
	differing := make(map[string]any)
	for i := 0; i < 15; i++ {
		key := string(rune('a'+i)) + "key"
		expected[key] = 1
		differing[key] = 2
	}
	actual, err := json.Marshal(differing)
	require.NoError(t, err)

	errs := AssertJSONOutput(string(actual), expected)
	// 10 shown + 1 summary line for the remaining 5
	require.Equal(t, maxJSONDiffs+1, len(errs))
	assert.Contains(t, errs[maxJSONDiffs].Error(), "(5 more differences)")
}

func TestAssertJSONOutputUnrepresentableExpected(t *testing.T) {
	// A channel cannot be marshaled as JSON. An interface-keyed map used to
	// serve here, but the toolchain now resolves such a key to its dynamic
	// type and marshals it, so that fixture stopped proving anything.
	errs := AssertJSONOutput("{}", map[string]any{"x": make(chan int)})
	require.Equal(t, 1, len(errs))
	assert.Contains(t, errs[0].Error(), "cannot be represented as JSON")
}

func TestJSONNumbersEqualPrecision(t *testing.T) {
	// Distinct big integers beyond float64 precision must NOT compare equal.
	assert.False(t, jsonNumbersEqual(json.Number("9007199254740993"), json.Number("9007199254740992")))
	assert.True(t, jsonNumbersEqual(json.Number("9007199254740993"), json.Number("9007199254740993")))
	// Numeric-value equality still holds across representations.
	assert.True(t, jsonNumbersEqual(json.Number("2"), json.Number("2.0")))
	assert.True(t, jsonNumbersEqual(json.Number("1e3"), json.Number("1000")))
	assert.False(t, jsonNumbersEqual(json.Number("2"), json.Number("2.5")))
}

func TestAssertJSONOutputBigIntegerPrecision(t *testing.T) {
	errs := AssertJSONOutput("9007199254740992", 9007199254740993)
	require.Len(t, errs, 1, "big integers differing by 1 must be reported as a mismatch")
	assert.Contains(t, errs[0].Error(), "expected 9007199254740993, got 9007199254740992")

	// 2 vs 2.0 must still compare equal end-to-end.
	assert.Empty(t, AssertJSONOutput("2.0", 2))
}

func TestRenderJSONTruncatesAtRuneBoundary(t *testing.T) {
	s := renderJSON(strings.Repeat("é", 60))
	assert.True(t, strings.HasSuffix(s, "..."))
	assert.True(t, utf8.ValidString(s), "truncated JSON rendering must remain valid UTF-8: %q", s)
	assert.LessOrEqual(t, len(s), 63)

	// Short values are untouched.
	assert.Equal(t, `"ab"`, renderJSON("ab"))
}

func TestChildPath(t *testing.T) {
	assert.Equal(t, "$.plain", childPath("$", "plain"))
	assert.Equal(t, "$.a.b", childPath("$.a", "b"))
	assert.Equal(t, `$["weird key"]`, childPath("$", "weird key"))
	assert.Equal(t, `$["0start"]`, childPath("$", "0start"))
}

func TestJSONTypeName(t *testing.T) {
	assert.Equal(t, "null", jsonTypeName(nil))
	assert.Equal(t, "bool", jsonTypeName(true))
	assert.Equal(t, "string", jsonTypeName("s"))
	assert.Equal(t, "array", jsonTypeName([]any{}))
	assert.Equal(t, "object", jsonTypeName(map[string]any{}))
}
