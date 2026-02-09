package schema

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
)

//go:generate xgen -i ../../schema/dats.xsd -o generated -l Go -p schema

var exitVarPattern = regexp.MustCompile(`^EXIT_[A-Z_]+$`)

// UnmarshalXMLAttr validates the exit code attribute value.
// Accepts an integer (0-255) or an EXIT_* variable name.
func (e *ExitCode) UnmarshalXMLAttr(attr xml.Attr) error {
	if attr.Value == "" {
		return nil
	}
	// Try int first
	if _, err := strconv.Atoi(attr.Value); err == nil {
		*e = ExitCode(attr.Value)
		return nil
	}
	// Try string - must match EXIT_* pattern
	if !exitVarPattern.MatchString(attr.Value) {
		return fmt.Errorf("exit %q must be an integer (0-255) or EXIT_* variable name", attr.Value)
	}
	*e = ExitCode(attr.Value)
	return nil
}

// IntValue returns the integer value of the exit code, or 0 if it's a variable.
func (e ExitCode) IntValue() int {
	if val, err := strconv.Atoi(string(e)); err == nil {
		return val
	}
	return 0
}

// IsVariable returns true if the exit code is an EXIT_* variable name.
func (e ExitCode) IsVariable() bool {
	return exitVarPattern.MatchString(string(e))
}

// VariableName returns the variable name if this is a variable exit code, or "".
func (e ExitCode) VariableName() string {
	if e.IsVariable() {
		return string(e)
	}
	return ""
}

// String returns the exit code as a string for display.
func (e ExitCode) String() string {
	if e.IsVariable() {
		return "$" + string(e)
	}
	return string(e)
}
