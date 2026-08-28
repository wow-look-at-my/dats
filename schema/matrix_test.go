package schema


import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFile_MatrixDeclarationOrderPreserved(t *testing.T) {
	path := writeTempDats(t, `
tests:
	- cmd: echo "{matrix.zeta} {matrix.alpha} {matrix.mid}"
	  matrix:
		zeta:
			- z1
			- z2
		alpha:
			- a1
		mid:
			- m1
			- m2
			- m3
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	assert.Equal(t, Matrix{
		{Name: "zeta", Values: []string{"z1", "z2"}},
		{Name: "alpha", Values: []string{"a1"}},
		{Name: "mid", Values: []string{"m1", "m2", "m3"}},
	}, tf.Tests[0].Matrix)

	values, declared := tf.Tests[0].Matrix.Lookup("mid")
	require.True(t, declared)
	assert.Equal(t, []string{"m1", "m2", "m3"}, values)
	_, declared = tf.Tests[0].Matrix.Lookup("nope")
	assert.False(t, declared)
	assert.Equal(t, []string{"zeta", "alpha", "mid"}, tf.Tests[0].Matrix.Names())
}

func TestParseFile_MatrixValueStringification(t *testing.T) {
	// Quoted strings keep their content verbatim.
	path := writeTempDats(t, `
tests:
	- cmd: echo "{matrix.i} {matrix.f} {matrix.b} {matrix.s}"
	  matrix:
		i:
			- 1
			- 42
		f:
			- 1.5
		b:
			- true
			- false
		s:
			- quoted text
			- single
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	assert.Equal(t, Matrix{
		{Name: "i", Values: []string{"1", "42"}},
		{Name: "f", Values: []string{"1.5"}},
		{Name: "b", Values: []string{"true", "false"}},
		{Name: "s", Values: []string{"quoted text", "single"}},
	}, tf.Tests[0].Matrix)
}

func TestParseFile_MatrixNullTreatedAsAbsent(t *testing.T) {
	path := writeTempDats(t, `
tests:
	- cmd: echo hi
	  matrix: null
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	assert.Nil(t, tf.Tests[0].Matrix)
}

func TestParseFile_MatrixRejected(t *testing.T) {
	cases := map[string]struct {
		content string
		wantErr string
	}{
		"empty mapping": {`
tests:
	- cmd: echo hi
	  matrix:
		{}
`, "matrix must declare at least one variable"},
		"non-mapping sequence": {`
tests:
	- cmd: echo hi
	  matrix:
		- a
		- b
`, "matrix must be a mapping of variable names to value lists"},
		"non-mapping scalar": {`
tests:
	- cmd: echo hi
	  matrix: nope
`, "matrix must be a mapping of variable names to value lists"},
		"variable name with dash": {`
tests:
	- cmd: echo hi
	  matrix:
		bad-name:
			- x
`, `matrix variable name "bad-name" must match ^[A-Za-z_][A-Za-z0-9_]*$`},
		"variable name leading digit": {`
tests:
	- cmd: echo hi
	  matrix:
		1x:
			- v
`, `matrix variable name "1x" must match`},
		"duplicate variable": {`
tests:
	- cmd: echo hi
	  matrix:
		x: [a]
		x: [b]
`, `duplicate mapping key "x"`},
		"empty value list": {`
tests:
	- cmd: echo hi
	  matrix:
		x: []
`, `matrix variable "x" must list at least one value`},
		"scalar instead of list": {`
tests:
	- cmd: echo hi
	  matrix:
		x: solo
`, `matrix variable "x" must list its values as a sequence`},
		"mapping value in list": {`
tests:
	- cmd: echo hi
	  matrix:
		x:
			- a: b
`, `matrix variable "x" value 1: values must be scalar strings, numbers, or booleans`},
		"sequence value in list": {`
tests:
	- cmd: echo hi
	  matrix:
		x:
			- ok
			-
				- nested
`, `matrix variable "x" value 2: values must be scalar strings, numbers, or booleans`},
		"null value in list": {`
tests:
	- cmd: echo hi
	  matrix:
		x:
			- ok
			- null
`, `matrix variable "x" value 2: values must be scalar strings, numbers, or booleans`},
		"tilde null value in list": {`
tests:
	- cmd: echo hi
	  matrix:
		x:
			- null
`, `matrix variable "x" value 1: values must be scalar strings, numbers, or booleans`},
		"duplicate value": {`
tests:
	- cmd: echo hi
	  matrix:
		x:
			- a
			- b
			- a
`, `matrix variable "x" lists duplicate value "a"`},
		"duplicate value after stringification": {`
tests:
	- cmd: echo hi
	  matrix:
		x:
			- 1.5
			- "1.5"
`, `matrix variable "x" lists duplicate value "1.5"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, tc.content))
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestParseFile_MatrixUnknownRefRejected(t *testing.T) {
	cases := map[string]string{
		"desc": `
tests:
	- desc: "{matrix.nope}"
	  cmd: echo hi
	  matrix:
		a:
			- x
`,
		"cmd": `
tests:
	- cmd: echo "{matrix.nope}"
	  matrix:
		a:
			- x
`,
		"stdin": `
tests:
	- cmd: cat
	  matrix:
		a:
			- x
	  inputs:
		stdin: "{matrix.nope}"
`,
		"input file contents": `
tests:
	- cmd: echo hi
	  matrix:
		a:
			- x
	  inputs:
		files:
			data.txt: "{matrix.nope}"
`,
		"env value": `
tests:
	- cmd: echo hi
	  matrix:
		a:
			- x
	  inputs:
		env:
			VAR: "{matrix.nope}"
`,
		"stdout pattern": `
tests:
	- cmd: echo hi
	  matrix:
		a:
			- x
	  outputs:
		stdout:
			- "{matrix.nope}"
`,
		"stderr line check": `
tests:
	- cmd: echo hi
	  matrix:
		a:
			- x
	  outputs:
		stderr:
			"0": "{matrix.nope}"
`,
		"negated stdout pattern": `
tests:
	- cmd: echo hi
	  matrix:
		a:
			- x
	  outputs:
		!stdout:
			- "{matrix.nope}"
`,
		"file check match": `
tests:
	- cmd: echo hi
	  matrix:
		a:
			- x
	  outputs:
		files:
			out.txt:
				match:
					- "{matrix.nope}"
`,
		"negated file check notMatch": `
tests:
	- cmd: echo hi
	  matrix:
		a:
			- x
	  outputs:
		!files:
			out.txt:
				notMatch:
					- "{matrix.nope}"
`,
		"json_output value": `
tests:
	- cmd: echo hi
	  matrix:
		a:
			- x
	  outputs:
		json_output:
			key: "{matrix.nope}"
`,
		"json_output key": `
tests:
	- cmd: echo hi
	  matrix:
		a:
			- x
	  outputs:
		json_output:
			"{matrix.nope}": value
`,
		"json_output nested array element": `
tests:
	- cmd: echo hi
	  matrix:
		a:
			- x
	  outputs:
		json_output:
			list:
				- ok
				- "{matrix.nope}"
`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, content))
			require.NotNil(t, err)
			assert.Contains(t, err.Error(),
				`test 1: {matrix.nope} is not a declared matrix variable (declared: a)`)
		})
	}
}

func TestParseFile_MatrixRefWithoutMatrixRejected(t *testing.T) {
	path := writeTempDats(t, `
tests:
	- cmd: echo "{matrix.x}"
`)
	_, err := ParseFile(path)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "test 1: {matrix.x} is used but the test declares no matrix")
}

func TestParseFile_MatrixEmptyRefRejected(t *testing.T) {
	path := writeTempDats(t, `
tests:
	- cmd: echo "{matrix.}"
	  matrix:
		a:
			- x
`)
	_, err := ParseFile(path)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "test 1: {matrix.} must name a matrix variable")
}

func TestParseFile_MatrixErrorNamesTest(t *testing.T) {
	path := writeTempDats(t, `
tests:
	- cmd: echo fine
	- cmd: echo "{matrix.x}"
`)
	_, err := ParseFile(path)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "test 2: {matrix.x} is used but the test declares no matrix")
}

func TestParseFile_MatrixRefInHooksRejected(t *testing.T) {
	// {matrix.X} exists only inside a test instance.
	cases := map[string]struct {
		content string
		wantErr string
	}{
		"setup string form": {`
setup: echo "{matrix.x}"
tests:
	- cmd: echo hi
	  matrix:
		x:
			- a
`, `setup command 1: {matrix.x} is not available outside tests`},
		"setup second command": {`
setup:
	- echo fine
	- echo "{matrix.x}"
tests:
	- cmd: echo hi
`, `setup command 2: {matrix.x} is not available outside tests`},
		"teardown": {`
teardown: echo "{matrix.x}"
tests:
	- cmd: echo hi
`, `teardown command 1: {matrix.x} is not available outside tests`},
		"shared file contents": {`
shared:
	files:
		config.json: "{matrix.x}"
tests:
	- cmd: echo hi
`, `shared file "config.json": {matrix.x} is not available outside tests`},
		"setup hook env value": {`
setup:
	- cmd: echo hi
	  env:
		FOO: "{matrix.x}"
tests:
	- cmd: echo hi
`, `setup command 1: env "FOO": {matrix.x} is not available outside tests`},
		"teardown hook stdin_file": {`
teardown:
	- cmd: echo hi
	  stdin_file: "{matrix.x}"
tests:
	- cmd: echo hi
`, `teardown command 1: stdin_file: {matrix.x} is not available outside tests`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, tc.content))
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestParseFile_MatrixFileNamesNotScanned(t *testing.T) {
	path := writeTempDats(t, `
tests:
	- cmd: echo hi
	  inputs:
		files:
			file-{matrix.x}.txt: literal name
		env:
			"{matrix.x}": literal env name
	  outputs:
		files:
			out-{matrix.x}.txt:
				{}
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	assert.Contains(t, tf.Tests[0].Inputs.Files, "file-{matrix.x}.txt")
	assert.Contains(t, tf.Tests[0].Inputs.Env, "{matrix.x}")
	assert.Contains(t, tf.Tests[0].Outputs.Files, "out-{matrix.x}.txt")
}
