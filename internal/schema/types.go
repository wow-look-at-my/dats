package schema

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
)

var exitVarPattern = regexp.MustCompile(`^EXIT_[A-Z_]+$`)

// TestFile represents the root <dats> element
type TestFile struct {
	XMLName xml.Name `xml:"dats"`
	Tests   []Test   `xml:"test"`
}

// Test represents a single <test> element.
// Scalar properties are attributes ON the element.
// Structured content lives IN the element as children.
type Test struct {
	Desc    string       `xml:"desc,attr"`
	Cmd     string       `xml:"cmd,attr"`
	Exit    ExitCode     `xml:"exit,attr"`
	Stdin   string       `xml:"stdin"`
	Inputs  []InputFile  `xml:"input"`
	Stdout  *StreamCheck `xml:"stdout"`
	Stderr  *StreamCheck `xml:"stderr"`
	Outputs []FileOutput `xml:"output"`
}

// InputFile represents <input name="file.txt">content</input>
type InputFile struct {
	Name    string `xml:"name,attr"`
	Content string `xml:",chardata"`
}

// StreamCheck represents <stdout> or <stderr> with match/not-match/line children.
// Combines positive and negative assertions in one block.
type StreamCheck struct {
	Match    []string    `xml:"match"`
	NotMatch []string    `xml:"not-match"`
	Lines    []LineCheck `xml:"line"`
}

// LineCheck represents <line n="0">^pattern$</line>
type LineCheck struct {
	N       int    `xml:"n,attr"`
	Pattern string `xml:",chardata"`
}

// FileOutput represents <output name="result.txt" exists="true">
type FileOutput struct {
	Name     string     `xml:"name,attr"`
	Exists   ExistsBool `xml:"exists,attr"`
	Match    []string   `xml:"match"`
	NotMatch []string   `xml:"not-match"`
}

// ExistsBool is a boolean attribute that tracks whether it was explicitly set.
type ExistsBool struct {
	Set   bool
	Value bool
}

func (b *ExistsBool) UnmarshalXMLAttr(attr xml.Attr) error {
	b.Set = true
	switch attr.Value {
	case "true":
		b.Value = true
	case "false":
		b.Value = false
	default:
		return fmt.Errorf("exists must be 'true' or 'false', got %q", attr.Value)
	}
	return nil
}

// ExitCode can be an int or a string like "EXIT_SUCCESS"
type ExitCode struct {
	Value    int
	Variable string // If non-empty, use this variable name instead of Value
}

func (e *ExitCode) UnmarshalXMLAttr(attr xml.Attr) error {
	if attr.Value == "" {
		return nil
	}
	// Try int first
	if intVal, err := strconv.Atoi(attr.Value); err == nil {
		e.Value = intVal
		return nil
	}
	// Try string - must match EXIT_* pattern
	if !exitVarPattern.MatchString(attr.Value) {
		return fmt.Errorf("exit %q must be an integer (0-255) or EXIT_* variable name", attr.Value)
	}
	e.Variable = attr.Value
	return nil
}

// String returns the exit code as a string for display
func (e ExitCode) String() string {
	if e.Variable != "" {
		return "$" + e.Variable
	}
	return strconv.Itoa(e.Value)
}
