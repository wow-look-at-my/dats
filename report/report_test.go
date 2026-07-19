package report

// Unit tests for the report writers, on hand-built results: document shape,
// the counts contract (JUnit totals include synthetic hook cases; JSON
// summary counts instances only), canonical ordering, presence rules for
// captured output, and the XML control-character sanitizer.

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/dats/runner"
)

// mixedResults builds two file results exercising every report feature: a
// file with a passing and a failing instance plus two teardown failures, and
// a file whose setup failed (both instances reported failed, unrun).
func mixedResults() []*runner.FileResult {
	return []*runner.FileResult{
		{
			Path: "mixed.dats",
			Results: []runner.TestResult{
				{
					Name:     "passes [k=1]",
					Index:    0,
					Passed:   true,
					Duration: 100 * time.Millisecond,
					Command:  "echo ok",
					Stdout:   "ok\n",
				},
				{
					Name:     "fails [k=2]",
					Index:    1,
					Duration: 200 * time.Millisecond,
					Failures: []string{"first message", "second message"},
					Command:  "echo bad",
					Stdout:   "bad-out",
					Stderr:   "bad-err",
				},
			},
			Passed: 1,
			Failed: 1,
			TeardownFailures: []runner.CommandFailure{
				{Command: "exit 4", Detail: "exit code 4", Stdout: "t-out\n"},
				{Command: "exit 5", Detail: "exit code 5", Stderr: "t-err\n"},
			},
		},
		{
			Path: "setupfail.dats",
			Results: []runner.TestResult{
				{Name: "never ran", Index: 0, Failures: []string{"file setup failed"}},
				{Name: "also never ran", Index: 1, Failures: []string{"file setup failed"}},
			},
			Failed:       2,
			SetupFailure: &runner.CommandFailure{Command: "exit 7", Detail: "exit code 7", Stdout: "s-out\n", Stderr: "s-err\n"},
		},
	}
}

// parsed* mirror the emitted JUnit shape for round-tripping in assertions.
type parsedSuites struct {
	XMLName  xml.Name      `xml:"testsuites"`
	Tests    int           `xml:"tests,attr"`
	Failures int           `xml:"failures,attr"`
	Time     string        `xml:"time,attr"`
	Suites   []parsedSuite `xml:"testsuite"`
}

type parsedSuite struct {
	Name     string       `xml:"name,attr"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Time     string       `xml:"time,attr"`
	Cases    []parsedCase `xml:"testcase"`
}

type parsedCase struct {
	ClassName string         `xml:"classname,attr"`
	Name      string         `xml:"name,attr"`
	Time      string         `xml:"time,attr"`
	Failure   *parsedFailure `xml:"failure"`
	SystemOut *string        `xml:"system-out"`
	SystemErr *string        `xml:"system-err"`
}

type parsedFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func TestWriteJUnitShape(t *testing.T) {
	var buf bytes.Buffer
	require.Nil(t, WriteJUnit(&buf, mixedResults(), 2*time.Second))
	assert.True(t, bytes.HasPrefix(buf.Bytes(), []byte(xml.Header)))
	assert.True(t, bytes.HasSuffix(buf.Bytes(), []byte("\n")))

	var root parsedSuites
	require.Nil(t, xml.Unmarshal(buf.Bytes(), &root))

	// Root totals include the synthetic hook cases: 4 instances + 1 setup +
	// 2 teardowns; failures: 3 failed instances + the same 3 synthetics.
	assert.Equal(t, 7, root.Tests)
	assert.Equal(t, 6, root.Failures)
	assert.Equal(t, "2.000", root.Time)
	require.Len(t, root.Suites, 2)

	mixed := root.Suites[0]
	assert.Equal(t, "mixed.dats", mixed.Name)
	assert.Equal(t, 4, mixed.Tests)    // 2 instances + 2 synthetic teardowns
	assert.Equal(t, 3, mixed.Failures) // 1 failed instance + 2 teardowns
	assert.Equal(t, "0.300", mixed.Time)
	require.Len(t, mixed.Cases, 4)

	pass := mixed.Cases[0]
	assert.Equal(t, "passes [k=1]", pass.Name)
	assert.Equal(t, "mixed.dats", pass.ClassName)
	assert.Equal(t, "0.100", pass.Time)
	assert.Nil(t, pass.Failure, "passing case must have no failure element")
	assert.Nil(t, pass.SystemOut, "passing case must not carry captured output")
	assert.Nil(t, pass.SystemErr)

	fail := mixed.Cases[1]
	assert.Equal(t, "fails [k=2]", fail.Name)
	require.NotNil(t, fail.Failure)
	assert.Equal(t, "first message", fail.Failure.Message)
	assert.Equal(t, "first message\nsecond message", fail.Failure.Text)
	require.NotNil(t, fail.SystemOut)
	assert.Equal(t, "bad-out", *fail.SystemOut)
	require.NotNil(t, fail.SystemErr)
	assert.Equal(t, "bad-err", *fail.SystemErr)

	// Multiple teardown failures get trailing cases with #N suffixes.
	assert.Equal(t, "[teardown] #1", mixed.Cases[2].Name)
	assert.Equal(t, "[teardown] #2", mixed.Cases[3].Name)
	assert.Equal(t, "", mixed.Cases[2].Time, "synthetic cases carry no time attribute")
	require.NotNil(t, mixed.Cases[2].Failure)
	assert.Equal(t, "exit code 4", mixed.Cases[2].Failure.Message)
	assert.Equal(t, "command: exit 4\nexit code 4", mixed.Cases[2].Failure.Text)
	require.NotNil(t, mixed.Cases[2].SystemOut)
	assert.Equal(t, "t-out\n", *mixed.Cases[2].SystemOut)

	setup := root.Suites[1]
	assert.Equal(t, 3, setup.Tests)    // 2 instances + 1 synthetic setup
	assert.Equal(t, 3, setup.Failures) // both instances + the synthetic
	require.Len(t, setup.Cases, 3)
	// The synthetic [setup] case comes FIRST, before the instance cases.
	assert.Equal(t, "[setup]", setup.Cases[0].Name)
	require.NotNil(t, setup.Cases[0].Failure)
	assert.Equal(t, "exit code 7", setup.Cases[0].Failure.Message)
	assert.Equal(t, "never ran", setup.Cases[1].Name)
	assert.Equal(t, "also never ran", setup.Cases[2].Name)
	require.NotNil(t, setup.Cases[1].Failure)
	assert.Equal(t, "file setup failed", setup.Cases[1].Failure.Message)
}

func TestWriteJUnitSingleTeardownHasNoSuffix(t *testing.T) {
	results := []*runner.FileResult{{
		Path:             "one.dats",
		Results:          []runner.TestResult{{Name: "fine", Passed: true}},
		Passed:           1,
		TeardownFailures: []runner.CommandFailure{{Command: "exit 1", Detail: "exit code 1"}},
	}}
	var buf bytes.Buffer
	require.Nil(t, WriteJUnit(&buf, results, time.Second))

	var root parsedSuites
	require.Nil(t, xml.Unmarshal(buf.Bytes(), &root))
	require.Len(t, root.Suites, 1)
	require.Len(t, root.Suites[0].Cases, 2)
	assert.Equal(t, "[teardown]", root.Suites[0].Cases[1].Name)
}

func TestWriteJSONShape(t *testing.T) {
	var buf bytes.Buffer
	require.Nil(t, WriteJSON(&buf, mixedResults(), 2*time.Second))
	assert.True(t, bytes.HasSuffix(buf.Bytes(), []byte("\n")))

	var doc map[string]any
	require.Nil(t, json.Unmarshal(buf.Bytes(), &doc))

	assert.Equal(t, float64(FormatVersion), doc["format_version"])
	assert.Equal(t, false, doc["ok"])

	// Summary counts are instance-only -- the CLI summary numbers; the
	// synthetic hook entries live in setup_failure/teardown_failures.
	summary := doc["summary"].(map[string]any)
	assert.Equal(t, float64(2), summary["files"])
	assert.Equal(t, float64(4), summary["tests"])
	assert.Equal(t, float64(1), summary["passed"])
	assert.Equal(t, float64(3), summary["failed"])
	assert.Equal(t, float64(2), summary["wall_seconds"])

	files := doc["files"].([]any)
	require.Len(t, files, 2)

	mixed := files[0].(map[string]any)
	assert.Equal(t, "mixed.dats", mixed["path"])
	assert.Equal(t, false, mixed["ok"])
	assert.InDelta(t, 0.3, mixed["duration_seconds"], 1e-9)
	assert.Nil(t, mixed["setup_failure"], "no setup failure must be null")
	teardowns := mixed["teardown_failures"].([]any)
	require.Len(t, teardowns, 2)
	first := teardowns[0].(map[string]any)
	assert.Equal(t, "exit 4", first["command"])
	assert.Equal(t, "exit code 4", first["detail"])
	assert.Equal(t, "t-out\n", first["stdout"])
	assert.Equal(t, "", first["stderr"])

	tests := mixed["tests"].([]any)
	require.Len(t, tests, 2)
	pass := tests[0].(map[string]any)
	assert.Equal(t, "passes [k=1]", pass["name"])
	assert.Equal(t, float64(1), pass["index"], "index is the canonical 1-based instance number")
	assert.Equal(t, true, pass["ok"])
	assert.Equal(t, []any{}, pass["failures"], "empty array, not null")
	assert.Equal(t, "echo ok", pass["command"])
	_, hasStdout := pass["stdout"]
	assert.False(t, hasStdout, "passing instances must not carry stdout")
	_, hasStderr := pass["stderr"]
	assert.False(t, hasStderr)

	fail := tests[1].(map[string]any)
	assert.Equal(t, float64(2), fail["index"])
	assert.Equal(t, false, fail["ok"])
	assert.Equal(t, []any{"first message", "second message"}, fail["failures"])
	assert.Equal(t, "bad-out", fail["stdout"])
	assert.Equal(t, "bad-err", fail["stderr"])

	setupFile := files[1].(map[string]any)
	setupFailure := setupFile["setup_failure"].(map[string]any)
	assert.Equal(t, "exit 7", setupFailure["command"])
	assert.Equal(t, "exit code 7", setupFailure["detail"])
	assert.Equal(t, []any{}, setupFile["teardown_failures"], "empty array, not null")
	setupTests := setupFile["tests"].([]any)
	require.Len(t, setupTests, 2)
	unrun := setupTests[0].(map[string]any)
	assert.Equal(t, []any{"file setup failed"}, unrun["failures"])
	assert.Equal(t, "", unrun["command"], "never-ran instances have an empty command")
	assert.Equal(t, "", unrun["stdout"], "failed instances carry stdout even when empty")
}

func TestWriteJSONFailedInstanceWithEmptyOutputKeepsKeys(t *testing.T) {
	// The presence rule is by outcome, not by content: a FAILED instance that
	// printed nothing still has stdout/stderr keys (empty strings).
	results := []*runner.FileResult{{
		Path:    "f.dats",
		Results: []runner.TestResult{{Name: "quiet failure", Failures: []string{"boom"}}},
		Failed:  1,
	}}
	var buf bytes.Buffer
	require.Nil(t, WriteJSON(&buf, results, 0))
	var doc map[string]any
	require.Nil(t, json.Unmarshal(buf.Bytes(), &doc))
	test := doc["files"].([]any)[0].(map[string]any)["tests"].([]any)[0].(map[string]any)
	assert.Equal(t, "", test["stdout"])
	assert.Equal(t, "", test["stderr"])
}

func TestWriteJSONEmptyRun(t *testing.T) {
	var buf bytes.Buffer
	require.Nil(t, WriteJSON(&buf, nil, 0))
	var doc map[string]any
	require.Nil(t, json.Unmarshal(buf.Bytes(), &doc))
	assert.Equal(t, true, doc["ok"])
	assert.Equal(t, []any{}, doc["files"], "empty array, not null")
}

func TestSanitizeXML(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "clean passes through", in: "plain text", want: "plain text"},
		{name: "legal whitespace kept", in: "a\tb\nc\rd", want: "a\tb\nc\rd"},
		{name: "unicode kept", in: "héllo é \U0001F600", want: "héllo é \U0001F600"},
		{name: "NUL replaced", in: "a\x00b", want: "a�b"},
		{name: "ESC replaced", in: "esc \x1b[31m red", want: "esc �[31m red"},
		{name: "other C0 controls replaced", in: "\x01\x0b\x1f", want: "���"},
		{name: "invalid UTF-8 byte replaced", in: "a\xffb", want: "a�b"},
		{name: "real U+FFFD kept", in: "a�b", want: "a�b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sanitizeXML(tt.in))
		})
	}
}

// TestWriteJUnitSanitizesControlChars pins that output containing bytes
// illegal in XML 1.0 still yields a well-formed, parseable document.
func TestWriteJUnitSanitizesControlChars(t *testing.T) {
	results := []*runner.FileResult{{
		Path: "ctrl.dats",
		Results: []runner.TestResult{{
			Name:     "prints control chars",
			Failures: []string{"esc \x1b here", "nul \x00 there"},
			Stdout:   "out \x1b[31m\x00 raw",
			Stderr:   "err \x00",
		}},
		Failed: 1,
	}}
	var buf bytes.Buffer
	require.Nil(t, WriteJUnit(&buf, results, time.Second))

	var root parsedSuites
	require.Nil(t, xml.Unmarshal(buf.Bytes(), &root), "sanitized XML must parse")
	c := root.Suites[0].Cases[0]
	require.NotNil(t, c.Failure)
	assert.Equal(t, "esc � here", c.Failure.Message)
	require.NotNil(t, c.SystemOut)
	assert.Equal(t, "out �[31m� raw", *c.SystemOut)
}

// TestWriteJSONPreservesControlChars pins the JSON side of the same
// guarantee: control characters need no replacement there -- encoding/json
// escapes them -- so the exact bytes round-trip.
func TestWriteJSONPreservesControlChars(t *testing.T) {
	results := []*runner.FileResult{{
		Path: "ctrl.dats",
		Results: []runner.TestResult{{
			Name:     "prints control chars",
			Failures: []string{"boom"},
			Stdout:   "out \x1b[31m\x00 raw",
		}},
		Failed: 1,
	}}
	var buf bytes.Buffer
	require.Nil(t, WriteJSON(&buf, results, time.Second))

	var doc map[string]any
	require.Nil(t, json.Unmarshal(buf.Bytes(), &doc))
	test := doc["files"].([]any)[0].(map[string]any)["tests"].([]any)[0].(map[string]any)
	assert.Equal(t, "out \x1b[31m\x00 raw", test["stdout"])
}
