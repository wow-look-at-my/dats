// Package report renders a finished run's results ([]*runner.FileResult)
// into machine-readable report files for CI consumption and editor
// integration: JUnit XML (WriteJUnit) and JSON (WriteJSON). Field names and
// document structure are a stability contract; see docs/reports.md.
package report

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wow-look-at-my/dats/runner"
)

// junitTestSuites is the <testsuites> root: totals over every suite,
// including the synthetic [setup]/[teardown] cases, and the wall time of the
// whole run.
type junitTestSuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Time     string           `xml:"time,attr"`
	Suites   []junitTestSuite `xml:"testsuite"`
}

// junitTestSuite is one <testsuite> per .dats file, named by the path as
// given on the command line (or discovered). Its time is the sum of its
// cases' durations, which under -j can exceed the run's wall time.
type junitTestSuite struct {
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Time     string          `xml:"time,attr"`
	Cases    []junitTestCase `xml:"testcase"`
}

// junitTestCase is one <testcase> per test instance, plus one synthetic case
// per file-level hook failure ([setup] first, [teardown] trailing). Synthetic
// cases carry no time attribute -- hook failures have no recorded duration.
type junitTestCase struct {
	ClassName string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	Time      string        `xml:"time,attr,omitempty"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
	SystemErr string        `xml:"system-err,omitempty"`
}

// junitFailure carries the first failure message as the message attribute
// and every message joined by newlines as the element text.
type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

// WriteJUnit writes a JUnit XML report of results to w. wall is the wall
// time of the whole execution phase and becomes the root time attribute.
// Ordering is canonical: files as given, instances in expansion order, with
// a failed file-level setup as a synthetic first [setup] case and each
// failed teardown command as a synthetic trailing [teardown] case. Synthetic
// cases count toward the tests/failures attributes, so JUnit totals can
// exceed the CLI's instance-only summary counts.
func WriteJUnit(w io.Writer, results []*runner.FileResult, wall time.Duration) error {
	root := junitTestSuites{Time: seconds(wall)}
	for _, fr := range results {
		suite := junitTestSuite{Name: sanitizeXML(fr.Path)}
		var suiteTime time.Duration

		if fr.SetupFailure != nil {
			suite.Cases = append(suite.Cases, hookCase(fr.Path, "[setup]", fr.SetupFailure))
			suite.Failures++
		}

		for i := range fr.Results {
			tr := &fr.Results[i]
			suiteTime += tr.Duration
			c := junitTestCase{
				ClassName: sanitizeXML(fr.Path),
				Name:      sanitizeXML(tr.Name),
				Time:      seconds(tr.Duration),
			}
			if !tr.Passed {
				c.Failure = &junitFailure{
					Message: sanitizeXML(firstString(tr.Failures)),
					Text:    sanitizeXML(strings.Join(tr.Failures, "\n")),
				}
				// Captured output is included only for failed cases, keeping
				// reports lean when everything passes.
				c.SystemOut = sanitizeXML(tr.Stdout)
				c.SystemErr = sanitizeXML(tr.Stderr)
				suite.Failures++
			}
			suite.Cases = append(suite.Cases, c)
		}

		for i := range fr.TeardownFailures {
			name := "[teardown]"
			if len(fr.TeardownFailures) > 1 {
				name = fmt.Sprintf("[teardown] #%d", i+1)
			}
			suite.Cases = append(suite.Cases, hookCase(fr.Path, name, &fr.TeardownFailures[i]))
			suite.Failures++
		}

		suite.Tests = len(suite.Cases)
		suite.Time = seconds(suiteTime)
		root.Tests += suite.Tests
		root.Failures += suite.Failures
		root.Suites = append(root.Suites, suite)
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(root); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

// hookCase renders a failed file-level setup or teardown command as a
// synthetic <testcase> holding the CommandFailure: the detail as the failure
// message, the command (when there is one) plus detail as the failure text,
// and the captured output as system-out/system-err.
func hookCase(path, name string, fail *runner.CommandFailure) junitTestCase {
	text := fail.Detail
	if fail.Command != "" {
		text = "command: " + fail.Command + "\n" + fail.Detail
	}
	return junitTestCase{
		ClassName: sanitizeXML(path),
		Name:      name,
		Failure: &junitFailure{
			Message: sanitizeXML(fail.Detail),
			Text:    sanitizeXML(text),
		},
		SystemOut: sanitizeXML(fail.Stdout),
		SystemErr: sanitizeXML(fail.Stderr),
	}
}

// seconds renders a duration as decimal seconds, the JUnit time convention.
func seconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 3, 64)
}

// firstString returns the first element, or "" for an empty slice.
func firstString(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// validXMLRune reports whether r is a legal XML 1.0 character. Notably the
// C0 control characters other than tab/newline/carriage-return -- which
// command output can easily contain (\x00, \x1b, ...) -- are not.
func validXMLRune(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD ||
		(r >= 0x20 && r <= 0xD7FF) ||
		(r >= 0xE000 && r <= 0xFFFD) ||
		(r >= 0x10000 && r <= 0x10FFFF)
}

// sanitizeXML replaces every rune that is not a legal XML 1.0 character, and
// every invalid UTF-8 byte, with U+FFFD so the report stays well-formed no
// matter what bytes a command printed. Clean strings are returned unchanged.
func sanitizeXML(s string) string {
	if xmlClean(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	// Ranging a string yields U+FFFD for each invalid byte, so invalid UTF-8
	// is replaced by the same WriteRune that passes legal runes through.
	for _, r := range s {
		if !validXMLRune(r) {
			r = utf8.RuneError
		}
		b.WriteRune(r)
	}
	return b.String()
}

// xmlClean reports whether s consists solely of legal XML 1.0 characters in
// valid UTF-8 (a real U+FFFD is fine; a decode error is not).
func xmlClean(s string) bool {
	for i, r := range s {
		if !validXMLRune(r) {
			return false
		}
		if r == utf8.RuneError {
			if _, size := utf8.DecodeRuneInString(s[i:]); size == 1 {
				return false
			}
		}
	}
	return true
}
