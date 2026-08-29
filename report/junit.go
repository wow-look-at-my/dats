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

type junitTestSuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Time     string           `xml:"time,attr"`
	Suites   []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Time     string          `xml:"time,attr"`
	Cases    []junitTestCase `xml:"testcase"`
}

type junitTestCase struct {
	ClassName string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	Time      string        `xml:"time,attr,omitempty"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
	SystemErr string        `xml:"system-err,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

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
				// Captured output is included only for failed cases, keeping reports lean when everything passes.
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

// validXMLRune reports whether r is a legal XML 1.0 character.
func validXMLRune(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD ||
		(r >= 0x20 && r <= 0xD7FF) ||
		(r >= 0xE000 && r <= 0xFFFD) ||
		(r >= 0x10000 && r <= 0x10FFFF)
}

func sanitizeXML(s string) string {
	if xmlClean(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !validXMLRune(r) {
			r = utf8.RuneError
		}
		b.WriteRune(r)
	}
	return b.String()
}

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
