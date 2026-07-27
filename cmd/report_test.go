package cmd

// Tests for the --report-junit/--report-json pipeline: golden files over a
// corpus covering matrix names, multi-assertion failures, and synthetic
// [setup]/[teardown] cases; byte-equality of normalized reports between
// serial and jobs mode; and the write mechanics (parent dirs, failing runs,
// unwritable paths, syntax command unaffected).

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setReportFlags points the report flag variables at the given paths for the
// duration of the test, restoring the previous values afterwards.
func setReportFlags(t *testing.T, junitPath, jsonPath string) {
	t.Helper()
	prevJUnit, prevJSON := reportJUnit, reportJSON
	reportJUnit, reportJSON = junitPath, jsonPath
	t.Cleanup(func() { reportJUnit, reportJSON = prevJUnit, prevJSON })
}

// writeReportCorpus generates the golden corpus: (a) a passing file with
// matrix instances, (b) a file with a multi-assertion failure and a
// temp-path-bearing failure, (c) a setup failure, (d) two teardown failures.
// Every command is deterministic (echo/printf/exit only), so the reports are
// byte-stable after normalization.
func writeReportCorpus(t *testing.T) (dir string, files []string) {
	t.Helper()
	dir = t.TempDir()
	corpus := []struct{ name, content string }{
		{"a-pass.dats", `tests:
  - desc: greets {matrix.who}
    cmd: echo "hi {matrix.who}"
    matrix:
      who: [alice, bob]
    outputs:
      stdout:
        - "hi {matrix.who}"
`},
		{"b-fail.dats", `tests:
  - desc: multi-assert failure
    cmd: printf 'real-out'; printf 'real-err' >&2; exit 3
    outputs:
      stdout:
        - "wanted-one"
        - "wanted-two"
  - desc: still passes
    cmd: echo fine
    outputs:
      stdout:
        - "fine"
  - desc: wrong file content
    cmd: printf data > {outputs.result.txt}
    outputs:
      files:
        result.txt:
          match:
            - "different"
`},
		{"c-setupfail.dats", `setup: echo setup-out; echo setup-err >&2; exit 7
tests:
  - desc: never runs
    cmd: echo hi
  - desc: also never runs
    cmd: echo hi2
`},
		{"d-teardownfail.dats", `teardown:
  - echo cleanup-out; exit 4
  - echo cleanup-err >&2; exit 5
tests:
  - desc: passes fine
    cmd: echo ok
    outputs:
      stdout:
        - "ok"
`},
	}
	for _, f := range corpus {
		require.Nil(t, os.WriteFile(filepath.Join(dir, f.name), []byte(f.content), 0644))
		files = append(files, f.name)
	}
	return dir, files
}

// runCorpusWithReports runs files (paths relative to dir, so report paths
// stay machine-independent) through the exact cmd pipeline -- runTests with
// both report flags set -- and returns the raw report bytes.
func runCorpusWithReports(t *testing.T, dir string, files []string, jobs int) (junitRaw, jsonRaw []byte, runErr error) {
	t.Helper()
	outDir := t.TempDir()
	junitPath := filepath.Join(outDir, "report.xml")
	jsonPath := filepath.Join(outDir, "report.json")
	setReportFlags(t, junitPath, jsonPath)

	t.Chdir(dir)
	var out bytes.Buffer
	runErr = runTests(context.Background(), files, &out, jobs, nil)

	var err error
	junitRaw, err = os.ReadFile(junitPath)
	require.Nil(t, err, "JUnit report must exist after the run")
	jsonRaw, err = os.ReadFile(jsonPath)
	require.Nil(t, err, "JSON report must exist after the run")
	return junitRaw, jsonRaw, runErr
}

var (
	// Durations and wall times are the only volatile values in the reports;
	// normalize them (and the random per-run temp directory) before
	// comparing bytes.
	reXMLTime     = regexp.MustCompile(`time="[0-9.eE+-]+"`)
	reJSONSeconds = regexp.MustCompile(`"(wall_seconds|duration_seconds)": [0-9.eE+-]+`)
	reTempDir     = regexp.MustCompile(regexp.QuoteMeta(os.TempDir()) + `/dats-[0-9]+`)
)

func normalizeReport(raw []byte) string {
	s := string(raw)
	s = reXMLTime.ReplaceAllString(s, `time="X"`)
	s = reJSONSeconds.ReplaceAllString(s, `"$1": 0`)
	return reTempDir.ReplaceAllString(s, "TMPDIR")
}

// compareGolden compares got against the checked-in golden byte-for-byte.
// Regenerate the goldens by running the tests once with
// DATS_UPDATE_GOLDENS=1 in the environment.
func compareGolden(t *testing.T, goldenPath, got string) {
	t.Helper()
	if os.Getenv("DATS_UPDATE_GOLDENS") == "1" {
		require.Nil(t, os.MkdirAll(filepath.Dir(goldenPath), 0o755))
		require.Nil(t, os.WriteFile(goldenPath, []byte(got), 0644))
	}
	want, err := os.ReadFile(goldenPath)
	require.Nil(t, err, "missing golden %s -- regenerate with DATS_UPDATE_GOLDENS=1", goldenPath)
	assert.Equal(t, string(want), got, "%s mismatch -- regenerate with DATS_UPDATE_GOLDENS=1 if the change is intended", goldenPath)
}

// TestReportsGolden runs the corpus through the cmd pipeline with both
// report flags at once and compares the normalized reports byte-for-byte
// against the checked-in goldens. The goldens pin matrix instance names,
// multi-assertion failure detail, synthetic [setup]/[teardown] cases, and
// the counts contract (JUnit totals include synthetics; JSON summary counts
// instances only).
func TestReportsGolden(t *testing.T) {
	junitGolden, err := filepath.Abs(filepath.Join("testdata", "reports", "golden.xml"))
	require.Nil(t, err)
	jsonGolden, err := filepath.Abs(filepath.Join("testdata", "reports", "golden.json"))
	require.Nil(t, err)

	dir, files := writeReportCorpus(t)
	junitRaw, jsonRaw, runErr := runCorpusWithReports(t, dir, files, 0)
	assert.ErrorIs(t, runErr, errTestsFailed, "the corpus contains failing files")

	compareGolden(t, junitGolden, normalizeReport(junitRaw))
	compareGolden(t, jsonGolden, normalizeReport(jsonRaw))
}

// TestReportsSerialParallelEquivalence proves the documented guarantee that
// reports are built from the same data in both modes: the corpus run at
// jobs=0 and jobs=4 yields byte-identical normalized reports.
func TestReportsSerialParallelEquivalence(t *testing.T) {
	dir, files := writeReportCorpus(t)
	serialJUnit, serialJSON, serialErr := runCorpusWithReports(t, dir, files, 0)
	parallelJUnit, parallelJSON, parallelErr := runCorpusWithReports(t, dir, files, 4)

	assert.ErrorIs(t, serialErr, errTestsFailed)
	assert.ErrorIs(t, parallelErr, errTestsFailed)
	assert.Equal(t, normalizeReport(serialJUnit), normalizeReport(parallelJUnit),
		"JUnit report must be identical between serial and jobs mode")
	assert.Equal(t, normalizeReport(serialJSON), normalizeReport(parallelJSON),
		"JSON report must be identical between serial and jobs mode")
}

func TestReportsCreateParentDirectories(t *testing.T) {
	datsFile := writeDats(t, "ok.dats", "tests:\n  - cmd: echo hi\n")
	outDir := t.TempDir()
	junitPath := filepath.Join(outDir, "deep", "junit", "report.xml")
	jsonPath := filepath.Join(outDir, "deeper", "json", "report.json")
	setReportFlags(t, junitPath, jsonPath)

	var out bytes.Buffer
	require.Nil(t, runTests(context.Background(), []string{datsFile}, &out, 0, nil))
	_, err := os.Stat(junitPath)
	assert.Nil(t, err, "missing parent directories must be created")
	_, err = os.Stat(jsonPath)
	assert.Nil(t, err)
}

// TestReportsWrittenWhenRunFails pins the point of the feature: the reports
// are written especially when tests fail and the process is about to exit 1,
// with ok=false and the failure counted.
func TestReportsWrittenWhenRunFails(t *testing.T) {
	datsFile := writeDats(t, "fail.dats", "tests:\n  - desc: fails\n    cmd: exit 9\n")
	outDir := t.TempDir()
	junitPath := filepath.Join(outDir, "report.xml")
	jsonPath := filepath.Join(outDir, "report.json")
	setReportFlags(t, junitPath, jsonPath)

	var out bytes.Buffer
	err := runTests(context.Background(), []string{datsFile}, &out, 0, nil)
	assert.ErrorIs(t, err, errTestsFailed, "exit-code semantics stay unchanged")

	raw, readErr := os.ReadFile(jsonPath)
	require.Nil(t, readErr, "the JSON report must be written when the run fails")
	var doc map[string]any
	require.Nil(t, json.Unmarshal(raw, &doc))
	assert.Equal(t, false, doc["ok"])
	summary := doc["summary"].(map[string]any)
	assert.Equal(t, float64(1), summary["tests"])
	assert.Equal(t, float64(0), summary["passed"])
	assert.Equal(t, float64(1), summary["failed"])

	_, statErr := os.Stat(junitPath)
	assert.Nil(t, statErr, "the JUnit report must be written when the run fails")
}

// TestReportsUnwritablePathFailsTheRun pins the error contract: a report
// that cannot be written is a real error -- surfaced (not the silent
// errTestsFailed sentinel) and failing the run even though every test
// passed. The other report is still attempted.
func TestReportsUnwritablePathFailsTheRun(t *testing.T) {
	datsFile := writeDats(t, "ok.dats", "tests:\n  - cmd: echo hi\n")
	outDir := t.TempDir()
	blocker := filepath.Join(outDir, "blocker")
	require.Nil(t, os.WriteFile(blocker, []byte("a file, not a directory"), 0644))

	junitPath := filepath.Join(outDir, "fine.xml")
	jsonPath := filepath.Join(blocker, "sub", "report.json") // path through a FILE
	setReportFlags(t, junitPath, jsonPath)

	var out bytes.Buffer
	err := runTests(context.Background(), []string{datsFile}, &out, 0, nil)
	require.NotNil(t, err, "a report write failure must fail the run even when all tests passed")
	assert.NotErrorIs(t, err, errTestsFailed, "the error must surface on stderr, not exit silently")
	assert.Contains(t, err.Error(), jsonPath)

	_, statErr := os.Stat(junitPath)
	assert.Nil(t, statErr, "the writable report is still written")
}

// TestReportsControlCharsStayParseable runs a command that emits ESC and NUL
// bytes and asserts both report files still parse: XML via the sanitizer
// (illegal runes become U+FFFD), JSON by encoding/json's escaping (the exact
// bytes round-trip).
func TestReportsControlCharsStayParseable(t *testing.T) {
	datsFile := writeDats(t, "ctrl.dats", `tests:
  - desc: emits control bytes
    cmd: printf 'esc \033[31m nul \000 end'; exit 1
`)
	outDir := t.TempDir()
	junitPath := filepath.Join(outDir, "report.xml")
	jsonPath := filepath.Join(outDir, "report.json")
	setReportFlags(t, junitPath, jsonPath)

	var out bytes.Buffer
	err := runTests(context.Background(), []string{datsFile}, &out, 0, nil)
	assert.ErrorIs(t, err, errTestsFailed)

	junitRaw, readErr := os.ReadFile(junitPath)
	require.Nil(t, readErr)
	var suites struct {
		XMLName  xml.Name `xml:"testsuites"`
		Tests    int      `xml:"tests,attr"`
		Failures int      `xml:"failures,attr"`
	}
	require.Nil(t, xml.Unmarshal(junitRaw, &suites), "JUnit must stay parseable despite control bytes in output")
	assert.Equal(t, 1, suites.Tests)
	assert.Equal(t, 1, suites.Failures)

	jsonRaw, readErr := os.ReadFile(jsonPath)
	require.Nil(t, readErr)
	var doc map[string]any
	require.Nil(t, json.Unmarshal(jsonRaw, &doc), "JSON must stay parseable despite control bytes in output")
	assert.Equal(t, float64(1), doc["format_version"])
	stdout := doc["files"].([]any)[0].(map[string]any)["tests"].([]any)[0].(map[string]any)["stdout"].(string)
	assert.Contains(t, stdout, "\x1b", "JSON preserves the exact bytes")
	assert.Contains(t, stdout, "\x00")
}

// TestSyntaxUnaffectedByReportFlags pins that the flags are registered
// long-only with a required value, and that `dats syntax` never writes
// report files.
func TestSyntaxUnaffectedByReportFlags(t *testing.T) {
	for _, name := range []string{"report-junit", "report-json"} {
		f := rootCmd.PersistentFlags().Lookup(name)
		require.NotNil(t, f, name)
		assert.Equal(t, "", f.Shorthand, "%s must be long-only", name)
		assert.Equal(t, "", f.NoOptDefVal, "%s must require a value", name)
	}

	datsFile := writeDats(t, "ok.dats", "tests:\n  - cmd: echo hi\n")
	outDir := t.TempDir()
	junitPath := filepath.Join(outDir, "report.xml")
	jsonPath := filepath.Join(outDir, "report.json")
	setReportFlags(t, junitPath, jsonPath)

	var out, errw bytes.Buffer
	assert.True(t, runSyntax([]string{datsFile}, &out, &errw))
	_, statErr := os.Stat(junitPath)
	assert.True(t, os.IsNotExist(statErr), "dats syntax must not write reports")
	_, statErr = os.Stat(jsonPath)
	assert.True(t, os.IsNotExist(statErr))
}
