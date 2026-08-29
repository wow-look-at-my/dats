package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxJSONDiffs = 10

// AssertJSONOutput checks that stdout parses as a single JSON value that deep-equals expected.
func AssertJSONOutput(stdout string, expected any) []error {
	canonical, err := canonicalizeJSON(expected)
	if err != nil {
		return []error{fmt.Errorf("json_output: expected value cannot be represented as JSON: %w", err)}
	}

	dec := json.NewDecoder(strings.NewReader(stdout))
	dec.UseNumber()
	var actual any
	if err := dec.Decode(&actual); err != nil {
		return []error{fmt.Errorf("json_output: stdout is not valid JSON: %v", err)}
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return []error{fmt.Errorf("json_output: stdout contains trailing data after the JSON value")}
	}

	diffs := compareJSON("$", canonical, actual, nil)
	if len(diffs) == 0 {
		return nil
	}

	shown := diffs
	if len(shown) > maxJSONDiffs {
		shown = shown[:maxJSONDiffs]
	}
	errs := make([]error, 0, len(shown)+1)
	for _, d := range shown {
		errs = append(errs, fmt.Errorf("json_output: %s", d))
	}
	if extra := len(diffs) - len(shown); extra > 0 {
		errs = append(errs, fmt.Errorf("json_output: (%d more differences)", extra))
	}
	return errs
}

func canonicalizeJSON(v any) (any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func compareJSON(path string, expected, actual any, diffs []string) []string {
	switch exp := expected.(type) {
	case map[string]any:
		act, ok := actual.(map[string]any)
		if !ok {
			return append(diffs, jsonMismatch(path, expected, actual))
		}
		for _, k := range sortedStringKeys(exp) {
			av, ok := act[k]
			if !ok {
				diffs = append(diffs, fmt.Sprintf("at %s: missing key %q (expected %s)", path, k, renderJSON(exp[k])))
				continue
			}
			diffs = compareJSON(childPath(path, k), exp[k], av, diffs)
		}
		for _, k := range sortedStringKeys(act) {
			if _, ok := exp[k]; !ok {
				diffs = append(diffs, fmt.Sprintf("at %s: unexpected key %q (value %s)", path, k, renderJSON(act[k])))
			}
		}
		return diffs

	case []any:
		act, ok := actual.([]any)
		if !ok {
			return append(diffs, jsonMismatch(path, expected, actual))
		}
		if len(exp) != len(act) {
			diffs = append(diffs, fmt.Sprintf("at %s: expected array of %d elements, got %d", path, len(exp), len(act)))
		}
		for i := 0; i < len(exp) && i < len(act); i++ {
			diffs = compareJSON(fmt.Sprintf("%s[%d]", path, i), exp[i], act[i], diffs)
		}
		return diffs

	case json.Number:
		act, ok := actual.(json.Number)
		if !ok {
			return append(diffs, jsonMismatch(path, expected, actual))
		}
		if !jsonNumbersEqual(exp, act) {
			diffs = append(diffs, fmt.Sprintf("at %s: expected %s, got %s", path, exp, act))
		}
		return diffs

	case string:
		act, ok := actual.(string)
		if !ok {
			return append(diffs, jsonMismatch(path, expected, actual))
		}
		if exp != act {
			diffs = append(diffs, fmt.Sprintf("at %s: expected %q, got %q", path, exp, act))
		}
		return diffs

	case bool:
		act, ok := actual.(bool)
		if !ok {
			return append(diffs, jsonMismatch(path, expected, actual))
		}
		if exp != act {
			diffs = append(diffs, fmt.Sprintf("at %s: expected %v, got %v", path, exp, act))
		}
		return diffs

	case nil:
		if actual != nil {
			return append(diffs, jsonMismatch(path, expected, actual))
		}
		return diffs

	default:
		// canonicalizeJSON only produces the types handled above
		return append(diffs, fmt.Sprintf("at %s: unsupported expected value of type %T", path, expected))
	}
}

func jsonNumbersEqual(a, b json.Number) bool {
	if a.String() == b.String() {
		return true
	}
	// big.Rat parses decimal and exponent forms (e.g. "2.0", "1e3") exactly.
	ar, aok := new(big.Rat).SetString(a.String())
	br, bok := new(big.Rat).SetString(b.String())
	if aok && bok {
		return ar.Cmp(br) == 0
	}
	af, errA := a.Float64()
	bf, errB := b.Float64()
	return errA == nil && errB == nil && af == bf
}

// jsonMismatch renders a type/value difference at path.
func jsonMismatch(path string, expected, actual any) string {
	return fmt.Sprintf("at %s: expected %s %s, got %s %s",
		path, jsonTypeName(expected), renderJSON(expected), jsonTypeName(actual), renderJSON(actual))
}

// jsonTypeName names a decoded JSON value's type.
func jsonTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case json.Number:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// renderJSON renders a value as compact JSON, truncated for readability.
func renderJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	const limit = 60
	s := string(data)
	if len(s) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "..."
	}
	return s
}

var plainKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// childPath extends a JSONPath-style path with an object key.
func childPath(path, key string) string {
	if plainKeyRe.MatchString(key) {
		return path + "." + key
	}
	return path + "[" + strconv.Quote(key) + "]"
}

func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
