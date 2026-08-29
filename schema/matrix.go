package schema

// Matrix tests: the type, the one definition of the substitution scope, and
// ExpandMatrix.

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/wow-look-at-my/go-containers/set"
	yamlfixed "github.com/wow-look-at-my/yaml-fixed/yaml"
)

var (
	// matrixNameRe is the allowed shape of a matrix variable name.
	matrixNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// Matches {matrix.} too, so validation can reject the empty name.
	matrixPlaceholderRe = regexp.MustCompile(`\{matrix\.([^}]*)\}`)
)

// MatrixVariable is one variable's name and ordered values. A quoted value
// keeps its text; an unquoted one is reformatted from its resolved type, so
// 1.50 becomes "1.5".
type MatrixVariable struct {
	Name   string
	Values []string
}

// Matrix is a slice, not a map: declaration order fixes the label order and
// the expansion order.
type Matrix []MatrixVariable

// UnmarshalYAML decodes the mapping through the parsed Map's Keys, which is
// what preserves declaration order. A slice field must opt into reading an
// explicit null as absent; a pointer field gets that for free.
func (m *Matrix) UnmarshalYAML(value any) error {
	if value == nil {
		return nil
	}
	mv, ok := value.(*yamlfixed.Map)
	if !ok {
		return fmt.Errorf("matrix must be a mapping of variable names to value lists")
	}
	if mv.Len() == 0 {
		return fmt.Errorf("matrix must declare at least one variable")
	}
	vars := make(Matrix, 0, mv.Len())
	for _, name := range mv.Keys {
		if !matrixNameRe.MatchString(name) {
			return fmt.Errorf("matrix variable name %q must match ^[A-Za-z_][A-Za-z0-9_]*$", name)
		}
		rawValues, _ := mv.Get(name)
		valueList, ok := rawValues.([]any)
		if !ok {
			return fmt.Errorf("matrix variable %q must list its values as a sequence", name)
		}
		if len(valueList) == 0 {
			return fmt.Errorf("matrix variable %q must list at least one value", name)
		}
		values := make([]string, 0, len(valueList))
		seen := set.New[string](len(valueList))
		for j, item := range valueList {
			// Only a scalar has substitution text.
			text, ok := matrixValueText(item)
			if !ok {
				return fmt.Errorf("matrix variable %q value %d: values must be scalar strings, numbers, or booleans", name, j+1)
			}
			// Compared after stringification: 1.50 and "1.50" are one instance.
			if !seen.Add(text) {
				return fmt.Errorf("matrix variable %q lists duplicate value %q", name, text)
			}
			values = append(values, text)
		}
		vars = append(vars, MatrixVariable{Name: name, Values: values})
	}
	*m = vars
	return nil
}

// matrixValueText renders a matrix value's resolved scalar as substitution
// text: a string is used verbatim (so a quoted "1.50" stays "1.50"), while a
// bool/int/float is formatted from its resolved value.
func matrixValueText(item any) (string, bool) {
	switch v := item.(type) {
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case int:
		return strconv.Itoa(v), true
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), true
	default:
		return "", false
	}
}

// Lookup returns the values declared for name and whether name is declared.
func (m Matrix) Lookup(name string) ([]string, bool) {
	for _, v := range m {
		if v.Name == name {
			return v.Values, true
		}
	}
	return nil, false
}

// Names returns the declared variable names in declaration order.
func (m Matrix) Names() []string {
	names := make([]string, len(m))
	for i, v := range m {
		names[i] = v.Name
	}
	return names
}

// MatrixAssignment is one variable=value binding of an expanded instance.
type MatrixAssignment struct {
	Name  string
	Value string
}

// TestInstance is one concrete, runnable instance of a test after matrix
// expansion. Ordinary tests expand to exactly one instance with no label.
type TestInstance struct {
	// Test is a substituted deep copy with Matrix cleared: not a template.
	Test Test
	// Label is the display suffix, e.g. "[greeting=hello, name=alice]".
	Label string
	// Assignments are the bindings, in declaration order; nil without a matrix.
	Assignments []MatrixAssignment
}

// ExpandMatrix returns one instance per combination, the LAST variable
// varying fastest. Substitution is a single pass, so a matrix value holding
// {matrix.y} stays literal, and each instance's Test is an independent copy.
func ExpandMatrix(test *Test) []TestInstance {
	if len(test.Matrix) == 0 {
		return []TestInstance{{Test: copyTest(test)}}
	}
	total := 1
	for _, v := range test.Matrix {
		total *= len(v.Values)
	}
	instances := make([]TestInstance, 0, total)
	indices := make([]int, len(test.Matrix))
	for n := 0; n < total; n++ {
		assignments := make([]MatrixAssignment, len(test.Matrix))
		labelParts := make([]string, len(test.Matrix))
		for j, v := range test.Matrix {
			assignments[j] = MatrixAssignment{Name: v.Name, Value: v.Values[indices[j]]}
			labelParts[j] = v.Name + "=" + v.Values[indices[j]]
		}
		instance := TestInstance{
			Test:        copyTest(test),
			Label:       "[" + strings.Join(labelParts, ", ") + "]",
			Assignments: assignments,
		}
		applyToMatrixScope(&instance.Test, func(s string) string {
			return substituteMatrix(s, assignments)
		})
		instances = append(instances, instance)
		// Odometer increment: the last variable varies fastest.
		for j := len(indices) - 1; j >= 0; j-- {
			indices[j]++
			if indices[j] < len(test.Matrix[j].Values) {
				break
			}
			indices[j] = 0
		}
	}
	return instances
}

// substituteMatrix replaces every {matrix.X} in one pass, leaving text it
// wrote alone. An unassigned name stays verbatim; ParseFile rejects those.
func substituteMatrix(s string, assignments []MatrixAssignment) string {
	return matrixPlaceholderRe.ReplaceAllStringFunc(s, func(match string) string {
		name := matrixPlaceholderRe.FindStringSubmatch(match)[1]
		for _, a := range assignments {
			if a.Name == name {
				return a.Value
			}
		}
		return match
	})
}

// applyToMatrixScope applies f in place to every string in the substitution
// scope. Fixture NAMES, env var names, exit and timeout stay out of it. This
// is the scope's one definition: validation and expansion both walk it, so
// they cannot disagree. Maps are visited in sorted key order.
func applyToMatrixScope(test *Test, f func(string) string) {
	test.Desc = f(test.Desc)
	test.Cmd = f(test.Cmd)
	test.Inputs.Stdin = f(test.Inputs.Stdin)
	for _, name := range slices.Sorted(maps.Keys(test.Inputs.Files)) {
		test.Inputs.Files[name] = f(test.Inputs.Files[name])
	}
	for _, name := range slices.Sorted(maps.Keys(test.Inputs.Copy)) {
		test.Inputs.Copy[name] = f(test.Inputs.Copy[name])
	}
	for _, name := range slices.Sorted(maps.Keys(test.Inputs.Env)) {
		test.Inputs.Env[name] = f(test.Inputs.Env[name])
	}
	checks := []*OutputCheck{
		&test.Outputs.Stdout, &test.Outputs.Stderr,
		&test.Outputs.NotStdout, &test.Outputs.NotStderr,
	}
	for _, check := range checks {
		for i := range check.Patterns {
			check.Patterns[i] = f(check.Patterns[i])
		}
		for _, line := range slices.Sorted(maps.Keys(check.LineChecks)) {
			check.LineChecks[line] = f(check.LineChecks[line])
		}
	}
	for _, files := range []map[string]FileCheck{test.Outputs.Files, test.Outputs.NotFiles} {
		for _, name := range slices.Sorted(maps.Keys(files)) {
			check := files[name]
			for i := range check.Match {
				check.Match[i] = f(check.Match[i])
			}
			for i := range check.NotMatch {
				check.NotMatch[i] = f(check.NotMatch[i])
			}
			files[name] = check
		}
	}
	if test.Outputs.JSONOutput.set {
		test.Outputs.JSONOutput.value = substituteJSONValue(test.Outputs.JSONOutput.value, f)
	}
}

// substituteJSONValue deep-copies json_output's value tree, applying f to
// every string and mapping key. A non-string scalar is untouched, so no
// substitution can change a JSON type. The identity f is copyTest's deep copy.
func substituteJSONValue(v any, f func(string) string) any {
	switch val := v.(type) {
	case string:
		return f(val)
	case []any:
		out := make([]any, len(val))
		for i, e := range val {
			out[i] = substituteJSONValue(e, f)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, e := range val {
			out[f(k)] = substituteJSONValue(e, f)
		}
		return out
	default:
		return val
	}
}

// validateMatrixRefs returns the first {matrix.X} naming an undeclared
// variable, so `dats syntax` catches a typo without running anything.
func validateMatrixRefs(test *Test) error {
	var firstErr error
	scanMatrixScope(test, func(s string) {
		if firstErr != nil {
			return
		}
		for _, match := range matrixPlaceholderRe.FindAllStringSubmatch(s, -1) {
			name := match[1]
			switch {
			case name == "":
				firstErr = fmt.Errorf("{matrix.} must name a matrix variable")
			case len(test.Matrix) == 0:
				firstErr = fmt.Errorf("{matrix.%s} is used but the test declares no matrix", name)
			default:
				if _, declared := test.Matrix.Lookup(name); !declared {
					firstErr = fmt.Errorf("{matrix.%s} is not a declared matrix variable (declared: %s)",
						name, strings.Join(test.Matrix.Names(), ", "))
				}
			}
			if firstErr != nil {
				return
			}
		}
	})
	return firstErr
}

// scanMatrixScope visits the scope without modifying it -- the same
// applyToMatrixScope walk expansion uses.
func scanMatrixScope(test *Test, visit func(s string)) {
	applyToMatrixScope(test, func(s string) string {
		visit(s)
		return s
	})
}

// findMatrixPlaceholder names the first {matrix.X} in s. It backs the guard
// on setup/teardown/shared, where no instance exists yet.
func findMatrixPlaceholder(s string) (string, bool) {
	match := matrixPlaceholderRe.FindStringSubmatch(s)
	if match == nil {
		return "", false
	}
	return match[1], true
}

// copyTest returns a deep copy of t: every map, every slice, and the
// json_output value tree are duplicated, so substitutions applied to the
// copy can never reach the source test or a sibling instance. The copy's
// Matrix is cleared -- an expanded instance is a concrete test, not a
// template.
func copyTest(t *Test) Test {
	c := *t
	c.Matrix = nil
	c.Inputs.Files = maps.Clone(t.Inputs.Files)
	c.Inputs.Copy = maps.Clone(t.Inputs.Copy)
	c.Inputs.Env = maps.Clone(t.Inputs.Env)
	c.Outputs.Stdout = copyOutputCheck(t.Outputs.Stdout)
	c.Outputs.Stderr = copyOutputCheck(t.Outputs.Stderr)
	c.Outputs.NotStdout = copyOutputCheck(t.Outputs.NotStdout)
	c.Outputs.NotStderr = copyOutputCheck(t.Outputs.NotStderr)
	c.Outputs.Files = copyFileChecks(t.Outputs.Files)
	c.Outputs.NotFiles = copyFileChecks(t.Outputs.NotFiles)
	if t.Outputs.JSONOutput.set {
		c.Outputs.JSONOutput.value = substituteJSONValue(t.Outputs.JSONOutput.value, func(s string) string { return s })
	}
	return c
}

func copyOutputCheck(check OutputCheck) OutputCheck {
	return OutputCheck{
		Patterns:   slices.Clone(check.Patterns),
		LineChecks: maps.Clone(check.LineChecks),
	}
}

func copyFileChecks(m map[string]FileCheck) map[string]FileCheck {
	if m == nil {
		return nil
	}
	c := make(map[string]FileCheck, len(m))
	for name, check := range m {
		if check.Exists != nil {
			exists := *check.Exists
			check.Exists = &exists
		}
		check.Match = slices.Clone(check.Match)
		check.NotMatch = slices.Clone(check.NotMatch)
		c[name] = check
	}
	return c
}
