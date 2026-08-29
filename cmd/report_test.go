package cmd

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

func setReportFlags(t *testing.T, junitPath, jsonPath string) {
	t.Helper()
	prevJUnit, prevJSON := reportJUnit, reportJSON
	reportJUnit, reportJSON = junitPath, jsonPath
	t.Cleanup(func() { reportJUnit, reportJSON = prevJUnit, prevJSON })
}

func writeReportCorpus(t *testing.T) (dir string, files []string) {
	t.Helper()
	dir = t.TempDir()
	corpus := []struct{ name, content string }{
		{"a-pass.dats", `tests:
	- desc: greets {matrix.who}
	  cmd: echo "hi {matrix.who}"
	  matrix:
		who:
			- alice
			- bob
	  outputs:
		stdout:
			- hi {matrix.who}
`},
		{"b-fail.dats", `tests:
	- desc: multi-assert failure
	  cmd: printf 'real-out'; printf 'real-err' >&2; exit 3
	  outputs:
		stdout:
			- wanted-one
			- wanted-two
	- desc: still passes
	  cmd: echo fine
	  outputs:
		stdout:
			- fine
	- desc: wrong file content
	  cmd: printf data > {outputs.result.txt}
	  outputs:
		files:
			result.txt:
				match:
					- different
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
			- ok
`},
	}
	for _, f := range corpus {
		require.Nil(t, os.WriteFile(filepath.Join(dir, f.name), []byte(f.content), 0644))
		files = append(files, f.name)
	}
	return dir, files
}

func runCorpusWithReports(t *testing.T, dir string, files []string, jobs int) (junitRaw, jsonRaw []byte, runErr error) {
	t.Helper()
	outDir := t.TempDir()
	junitPath := filepath.Join(outDir, "report.xml")
	jsonPath := filepath.Join(outDir, "report.json")
	setReportFlags(t, junitPath, jsonPath)

	t.Chdir(dir)
	var out bytes.Buffer
	runErr = runTests(context.Background(), files, &out, jobs, nil, "")

	var err error
	junitRaw, err = os.ReadFile(junitPath)
	require.Nil(t, err, "JUnit report must exist after the run")
	jsonRaw, err = os.ReadFile(jsonPath)
	require.Nil(t, err, "JSON report must exist after the run")
	return junitRaw, jsonRaw, runErr
}

var (
	reXMLTime     = regexp.MustCompile(`time="[0-9.eE+-]+"`)
	reJSONSeconds = regexp.MustCompile(`"(wall_seconds|duration_seconds)": [0-9.eE+-]+`)
	reTempDir     = regexp.MustCompile(`(` + regexp.QuoteMeta(resolvedTempDir()) +
		`|` + regexp.QuoteMeta(os.TempDir()) + `)/dats-[0-9]+`)
)

// resolvedTempDir is os.TempDir() with symlinks resolved, matching what the runner uses.
func resolvedTempDir() string {
	resolved, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return os.TempDir()
	}
	return resolved
}

func normalizeReport(raw []byte) string {
	s := string(raw)
	s = reXMLTime.ReplaceAllString(s, `time="X"`)
	s = reJSONSeconds.ReplaceAllString(s, `"$1": 0`)
	return reTempDir.ReplaceAllString(s, "TMPDIR")
}

// compareGolden compares got against the checked-in golden byte-for-byte.
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
	datsFile := writeDats(t, "ok.dats", "tests:\n\t- cmd: echo hi\n")
	outDir := t.TempDir()
	junitPath := filepath.Join(outDir, "deep", "junit", "report.xml")
	jsonPath := filepath.Join(outDir, "deeper", "json", "report.json")
	setReportFlags(t, junitPath, jsonPath)

	var out bytes.Buffer
	require.Nil(t, runTests(context.Background(), []string{datsFile}, &out, 0, nil, ""))
	_, err := os.Stat(junitPath)
	assert.Nil(t, err, "missing parent directories must be created")
	_, err = os.Stat(jsonPath)
	assert.Nil(t, err)
}

func TestReportsWrittenWhenRunFails(t *testing.T) {
	datsFile := writeDats(t, "fail.dats", "tests:\n\t- desc: fails\n\t  cmd: exit 9\n")
	outDir := t.TempDir()
	junitPath := filepath.Join(outDir, "report.xml")
	jsonPath := filepath.Join(outDir, "report.json")
	setReportFlags(t, junitPath, jsonPath)

	var out bytes.Buffer
	err := runTests(context.Background(), []string{datsFile}, &out, 0, nil, "")
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

func TestReportsUnwritablePathFailsTheRun(t *testing.T) {
	datsFile := writeDats(t, "ok.dats", "tests:\n\t- cmd: echo hi\n")
	outDir := t.TempDir()
	blocker := filepath.Join(outDir, "blocker")
	require.Nil(t, os.WriteFile(blocker, []byte("a file, not a directory"), 0644))

	junitPath := filepath.Join(outDir, "fine.xml")
	jsonPath := filepath.Join(blocker, "sub", "report.json") // path through a FILE
	setReportFlags(t, junitPath, jsonPath)

	var out bytes.Buffer
	err := runTests(context.Background(), []string{datsFile}, &out, 0, nil, "")
	require.NotNil(t, err, "a report write failure must fail the run even when all tests passed")
	assert.NotErrorIs(t, err, errTestsFailed, "the error must surface on stderr, not exit silently")
	assert.Contains(t, err.Error(), jsonPath)

	_, statErr := os.Stat(junitPath)
	assert.Nil(t, statErr, "the writable report is still written")
}

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
	err := runTests(context.Background(), []string{datsFile}, &out, 0, nil, "")
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

func TestSyntaxUnaffectedByReportFlags(t *testing.T) {
	for _, name := range []string{"report-junit", "report-json"} {
		f := rootCmd.PersistentFlags().Lookup(name)
		require.NotNil(t, f, name)
		assert.Equal(t, "", f.Shorthand, "%s must be long-only", name)
		assert.Equal(t, "", f.NoOptDefVal, "%s must require a value", name)
	}

	datsFile := writeDats(t, "ok.dats", "tests:\n\t- cmd: echo hi\n")
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
