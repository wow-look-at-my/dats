package schema

// Matrix (parameterized) tests: a test may declare `matrix:`, a mapping of
// variable name to a list of scalar values, expanding the test into one
// instance per combination (cartesian product). This file holds the Matrix
// type, the single definition of the matrix substitution scope (shared by
// parse-time reference validation and instance expansion), and ExpandMatrix.

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
	// matrixPlaceholderRe matches {matrix.X} references, including the
	// malformed empty-name form {matrix.} so validation can reject it.
	matrixPlaceholderRe = regexp.MustCompile(`\{matrix\.([^}]*)\}`)
)

// MatrixVariable is one declared matrix variable: its name and its ordered
// list of values. A quoted value is kept as its literal text (so "1.50"
// stays "1.50"); an unquoted value is reformatted from its resolved type
// (so 1.50 becomes "1.5" and true becomes "true") -- yaml-fixed resolves a
// bare scalar to a Go value rather than keeping its source spelling.
type MatrixVariable struct {
	Name   string
	Values []string
}

// Matrix is a test's matrix block: the declared variables in declaration
// order. Declaration order is semantic -- it fixes both the label order and
// the expansion order of instances -- so Matrix is an ordered slice rather
// than a map.
type Matrix []MatrixVariable

// UnmarshalYAML decodes the matrix mapping, preserving declaration order (a
// plain Go map would lose it) via the parsed *yamlfixed.Map's own Keys
// order. That Map can never hold a duplicate key (the parser rejects that
// before this ever runs), so no manual duplicate-variable-name bookkeeping
// is needed. Each value must itself be a
// scalar's literal text -- yaml-fixed resolves scalars to Go values rather
// than keeping source text, so a value is reformatted from its resolved type
// rather than echoed verbatim (a float like 1.50 becomes "1.5", not "1.50";
// see docs/file-format.md). An explicit `matrix: null` is treated the same
// as an absent key (matching how `shared:`/`sandbox:` -- pointer-typed
// fields the decoder never dereferences for a nil value -- already behave):
// decodeStruct invokes UnmarshalYAML for any key that is present, null value
// included, so a slice-typed field like Matrix must opt into that "null
// means absent" reading itself rather than getting it for free.
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
			// Only scalar values make sense as substitution text; null has no
			// text at all, and a mapping/sequence has no scalar text either.
			text, ok := matrixValueText(item)
			if !ok {
				return fmt.Errorf("matrix variable %q value %d: values must be scalar strings, numbers, or booleans", name, j+1)
			}
			// Duplicates are compared after stringification: 1.50 and "1.50"
			// would produce byte-identical instances, so the repeat can only
			// be a mistake.
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
	// Test is a deep copy of the source test with every in-scope {matrix.X}
	// placeholder substituted and its Matrix cleared: an instance is a
	// concrete test, not a template.
	Test Test
	// Label is the display suffix identifying the instance, e.g.
	// "[greeting=hello, name=alice]" (assignments in declaration order).
	// Empty for tests without a matrix.
	Label string
	// Assignments holds the instance's variable=value bindings in
	// declaration order. Nil for tests without a matrix.
	Assignments []MatrixAssignment
}

// ExpandMatrix expands test into its ordered list of instances: one per
// combination of matrix values (cartesian product). Variables keep their
// declaration order in both the label and the expansion order, with the LAST
// declared variable varying fastest. Substitution is a single pass --
// substituted text is never re-scanned, so a matrix value containing a
// literal {matrix.y} stays literal. A test without a matrix expands to a
// single unlabeled instance. Every instance's Test is a deep copy, fully
// independent of the source test and of sibling instances.
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

// substituteMatrix replaces every {matrix.X} in s with X's assigned value, in
// a single pass: the substituted text is not re-scanned, so values containing
// brace constructs pass through literally. A placeholder naming an unassigned
// variable is left verbatim (ParseFile rejects those, so this only arises for
// callers constructing tests directly).
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

// applyToMatrixScope applies f, in place, to every string of test that is
// inside the matrix substitution scope: desc, cmd, ssh, inputs.stdin, inputs.files
// contents, inputs.copy sources, inputs.env values, every output pattern
// string (stdout, stderr, !stdout, and !stderr in both the list and line-map
// forms; files and !files match/notMatch entries), and every string scalar
// inside json_output (keys and values). Out of scope and left untouched:
// fixture file names (files and copy destinations alike), env var names,
// exit, timeout, and the matrix block itself. This is the single definition
// of the scope -- parse-time reference validation and instance expansion both
// walk it, so they can never disagree. Maps are visited in sorted key order
// so validation reports deterministically.
func applyToMatrixScope(test *Test, f func(string) string) {
	test.Desc = f(test.Desc)
	test.Cmd = f(test.Cmd)
	// A per-test ssh target IS in scope, unlike the file-level one: it is
	// resolved per instance, so `ssh: "{matrix.host}"` fans one test across
	// a fleet.
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

// substituteJSONValue returns a deep copy of v -- json_output's generic
// value tree (nil, bool, int, float64, string, []any, or map[string]any) --
// with f applied to every string, mapping keys included; non-string scalars
// (numbers, bools, null) are untouched, so substitution inside json_output
// cannot change a value's JSON type. Passing the identity function makes
// this a pure deep copy, which is how copyTest duplicates JSONOutput without
// a second, separate copying mechanism.
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

// validateMatrixRefs checks every {matrix.X} reference in the test's matrix
// substitution scope against the test's declared variables, returning the
// first violation. ParseFile calls it per test (prefixing "test N:") so
// `dats syntax` catches a typoed or undeclared variable without running
// anything.
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

// scanMatrixScope calls visit on every string in the test's matrix
// substitution scope, without modifying anything. It walks the exact same
// scope as instance expansion (both are applyToMatrixScope).
func scanMatrixScope(test *Test, visit func(s string)) {
	applyToMatrixScope(test, func(s string) string {
		visit(s)
		return s
	})
}

// findMatrixPlaceholder returns the name of the first {matrix.X} reference in
// s, if any. It backs the parse-time guard rejecting matrix placeholders in
// file-level setup/teardown commands and shared file contents, where no
// matrix instance exists.
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
	// SSH is a POINTER: a shallow copy would make every instance share one
	// struct, so substituting {matrix.X} into one would corrupt its siblings.
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
