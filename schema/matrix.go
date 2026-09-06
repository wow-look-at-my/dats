package schema

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
	matrixNameRe        = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	matrixPlaceholderRe = regexp.MustCompile(`\{matrix\.([^}]*)\}`)
)

// MatrixVariable is a single declared matrix variable: its name and its ordered list of values.
type MatrixVariable struct {
	Name   string
	Values []string
}

// Matrix is a test's matrix block: the declared variables in declaration order.
type Matrix []MatrixVariable

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
			text, ok := matrixValueText(item)
			if !ok {
				return fmt.Errorf("matrix variable %q value %d: values must be scalar strings, numbers, or booleans", name, j+1)
			}
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

// MatrixAssignment is a single variable=value binding of an expanded instance.
type MatrixAssignment struct {
	Name  string
	Value string
}

// TestInstance is a single concrete, runnable instance of a test after matrix expansion.
type TestInstance struct {
	Test  Test
	Label string
	// Assignments holds the instance's variable=value bindings in declaration order.
	Assignments []MatrixAssignment
}

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

func applyToMatrixScope(test *Test, f func(string) string) {
	test.Desc = f(test.Desc)
	test.Cmd = f(test.Cmd)
	// A per-test ssh target IS in scope: it resolves per instance, so a test fans across a fleet.
	if test.SSH != nil {
		test.SSH.Target = f(test.SSH.Target)
	}
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

func scanMatrixScope(test *Test, visit func(s string)) {
	applyToMatrixScope(test, func(s string) string {
		visit(s)
		return s
	})
}

// findMatrixPlaceholder returns the name of the earliest {matrix.X} reference in s, if any.
func findMatrixPlaceholder(s string) (string, bool) {
	match := matrixPlaceholderRe.FindStringSubmatch(s)
	if match == nil {
		return "", false
	}
	return match[1], true
}

func copyTest(t *Test) Test {
	c := *t
	c.Matrix = nil
	// SSH is a POINTER: a shallow copy would let an instance corrupt its siblings.
	if t.SSH != nil {
		spec := *t.SSH
		c.SSH = &spec
	}
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
		Stated:     check.Stated,
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
