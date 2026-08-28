package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandMatrix_CartesianOrderLastFastest(t *testing.T) {
	tf, err := ParseFile(writeTempDats(t, `
tests:
	- desc: greets
	  cmd: echo "{matrix.greeting}, {matrix.name}!"
	  matrix:
		greeting:
			- hello
			- howdy
		name:
			- alice
			- bob
`))
	require.Nil(t, err)
	instances := ExpandMatrix(&tf.Tests[0])
	require.Len(t, instances, 4)

	labels := make([]string, len(instances))
	cmds := make([]string, len(instances))
	for i, instance := range instances {
		labels[i] = instance.Label
		cmds[i] = instance.Test.Cmd
	}
	assert.Equal(t, []string{
		"[greeting=hello, name=alice]",
		"[greeting=hello, name=bob]",
		"[greeting=howdy, name=alice]",
		"[greeting=howdy, name=bob]",
	}, labels)
	assert.Equal(t, []string{
		`echo "hello, alice!"`,
		`echo "hello, bob!"`,
		`echo "howdy, alice!"`,
		`echo "howdy, bob!"`,
	}, cmds)
	assert.Equal(t, []MatrixAssignment{
		{Name: "greeting", Value: "howdy"},
		{Name: "name", Value: "alice"},
	}, instances[2].Assignments)

	// A 2x3 matrix expands to 6 instances, last variable fastest.
	tf, err = ParseFile(writeTempDats(t, `
tests:
	- cmd: echo "{matrix.a}{matrix.b}"
	  matrix:
		a:
			- 1
			- 2
		b:
			- x
			- y
			- z
`))
	require.Nil(t, err)
	instances = ExpandMatrix(&tf.Tests[0])
	require.Len(t, instances, 6)
	labels = labels[:0]
	for _, instance := range instances {
		labels = append(labels, instance.Label)
	}
	assert.Equal(t, []string{
		"[a=1, b=x]", "[a=1, b=y]", "[a=1, b=z]",
		"[a=2, b=x]", "[a=2, b=y]", "[a=2, b=z]",
	}, labels)
}

func TestExpandMatrix_NoMatrixSingleInstance(t *testing.T) {
	tf, err := ParseFile(writeTempDats(t, `
tests:
	- desc: plain
	  cmd: echo hi
	  inputs:
		files:
			data.txt: content
	  outputs:
		stdout:
			- hi
`))
	require.Nil(t, err)
	instances := ExpandMatrix(&tf.Tests[0])
	require.Len(t, instances, 1)
	assert.Equal(t, "", instances[0].Label)
	assert.Nil(t, instances[0].Assignments)
	assert.Equal(t, tf.Tests[0], instances[0].Test)
}

func TestExpandMatrix_SingleValueStillLabeled(t *testing.T) {
	tf, err := ParseFile(writeTempDats(t, `
tests:
	- cmd: echo "{matrix.k}"
	  matrix:
		k:
			- v
`))
	require.Nil(t, err)
	instances := ExpandMatrix(&tf.Tests[0])
	require.Len(t, instances, 1)
	assert.Equal(t, "[k=v]", instances[0].Label)
	assert.Equal(t, `echo "v"`, instances[0].Test.Cmd)
}

func TestExpandMatrix_SubstitutionScope(t *testing.T) {
	tf, err := ParseFile(writeTempDats(t, `
tests:
	- desc: desc {matrix.a}
	  cmd: echo "cmd {matrix.a}"
	  matrix:
		a:
			- v1
	  inputs:
		stdin: stdin {matrix.a}
		files:
			data.txt: content {matrix.a}
			name-{matrix.a}.txt: literal name
		env:
			VAR: env {matrix.a}
	  outputs:
		stdout:
			- pattern {matrix.a}
		stderr:
			"0": line {matrix.a}
		!stdout:
			- neg {matrix.a}
		!stderr:
			"1": negline {matrix.a}
		files:
			out.txt:
				match:
					- match {matrix.a}
				notMatch:
					- notmatch {matrix.a}
		!files:
			stray.txt:
				match:
					- straymatch {matrix.a}
		json_output:
			key {matrix.a}: value {matrix.a}
`))
	require.Nil(t, err)
	instances := ExpandMatrix(&tf.Tests[0])
	require.Len(t, instances, 1)
	instance := instances[0].Test

	assert.Equal(t, "desc v1", instance.Desc)
	assert.Equal(t, `echo "cmd v1"`, instance.Cmd)
	assert.Equal(t, "stdin v1", instance.Inputs.Stdin)
	assert.Equal(t, "content v1", instance.Inputs.Files["data.txt"])
	assert.Equal(t, "literal name", instance.Inputs.Files["name-{matrix.a}.txt"],
		"file names must stay literal keys")
	assert.Equal(t, "env v1", instance.Inputs.Env["VAR"])
	assert.Equal(t, []string{"pattern v1"}, instance.Outputs.Stdout.Patterns)
	assert.Equal(t, map[int]string{0: "line v1"}, instance.Outputs.Stderr.LineChecks)
	assert.Equal(t, []string{"neg v1"}, instance.Outputs.NotStdout.Patterns)
	assert.Equal(t, map[int]string{1: "negline v1"}, instance.Outputs.NotStderr.LineChecks)
	assert.Equal(t, []string{"match v1"}, instance.Outputs.Files["out.txt"].Match)
	assert.Equal(t, []string{"notmatch v1"}, instance.Outputs.Files["out.txt"].NotMatch)
	assert.Equal(t, []string{"straymatch v1"}, instance.Outputs.NotFiles["stray.txt"].Match)

	jsonValue, err := instance.Outputs.JSONOutputValue()
	require.Nil(t, err)
	assert.Equal(t, map[string]any{"key v1": "value v1"}, jsonValue)

	assert.Nil(t, instance.Matrix, "an expanded instance is concrete: no matrix of its own")
}

func TestExpandMatrix_JSONOutputNonStringScalarsUntouched(t *testing.T) {
	tf, err := ParseFile(writeTempDats(t, `
tests:
	- cmd: echo hi
	  matrix:
		n:
			- x
	  outputs:
		json_output:
			count: 3
			flag: true
			name: "{matrix.n}"
`))
	require.Nil(t, err)
	instances := ExpandMatrix(&tf.Tests[0])
	require.Len(t, instances, 1)
	jsonValue, err := instances[0].Test.Outputs.JSONOutputValue()
	require.Nil(t, err)
	assert.Equal(t, map[string]any{"count": 3, "flag": true, "name": "x"}, jsonValue)
}

func TestExpandMatrix_SubstitutedTextNotRescanned(t *testing.T) {
	tf, err := ParseFile(writeTempDats(t, `
tests:
	- cmd: echo "{matrix.a} {matrix.b}"
	  matrix:
		a:
			- "{matrix.b}"
		b:
			- real
`))
	require.Nil(t, err)
	instances := ExpandMatrix(&tf.Tests[0])
	require.Len(t, instances, 1)
	assert.Equal(t, `echo "{matrix.b} real"`, instances[0].Test.Cmd)
}

func TestExpandMatrix_DeepCopyIsolation(t *testing.T) {
	exists := true
	tf, err := ParseFile(writeTempDats(t, `
tests:
	- cmd: echo "{matrix.x}"
	  matrix:
		x:
			- a
			- b
	  inputs:
		files:
			data.txt: payload {matrix.x}
	  outputs:
		stdout:
			- out {matrix.x}
		files:
			out.txt:
				exists: true
				match:
					- m {matrix.x}
`))
	require.Nil(t, err)
	source := &tf.Tests[0]
	instances := ExpandMatrix(source)
	require.Len(t, instances, 2)
	assert.Equal(t, "payload a", instances[0].Test.Inputs.Files["data.txt"])
	assert.Equal(t, "payload b", instances[1].Test.Inputs.Files["data.txt"])

	// Mutating one instance reaches neither the source test nor a sibling.
	instances[0].Test.Inputs.Files["data.txt"] = "mutated"
	instances[0].Test.Outputs.Stdout.Patterns[0] = "mutated"
	check := instances[0].Test.Outputs.Files["out.txt"]
	check.Match[0] = "mutated"
	*check.Exists = false

	assert.Equal(t, "payload {matrix.x}", source.Inputs.Files["data.txt"])
	assert.Equal(t, "out {matrix.x}", source.Outputs.Stdout.Patterns[0])
	assert.Equal(t, "m {matrix.x}", source.Outputs.Files["out.txt"].Match[0])
	assert.Equal(t, &exists, source.Outputs.Files["out.txt"].Exists)
	assert.Equal(t, "payload b", instances[1].Test.Inputs.Files["data.txt"])
	assert.Equal(t, "out b", instances[1].Test.Outputs.Stdout.Patterns[0])
	assert.Equal(t, "m b", instances[1].Test.Outputs.Files["out.txt"].Match[0])

	// The source keeps its matrix: expansion does not consume it.
	assert.Equal(t, Matrix{{Name: "x", Values: []string{"a", "b"}}}, source.Matrix)
}

func TestExpandMatrix_SnapshotIntactOnEveryInstance(t *testing.T) {
	tf, err := ParseFile(writeTempDats(t, `
tests:
	- desc: snap {matrix.who}
	  cmd: echo {matrix.who}
	  matrix:
		who:
			- alice
			- bob
	  outputs:
		snapshot:
			stdout: true
			stderr: true
`))
	require.Nil(t, err)
	instances := ExpandMatrix(&tf.Tests[0])
	require.Len(t, instances, 2)
	want := SnapshotCheck{Enabled: true, Stdout: true, Stderr: true}
	for i := range instances {
		assert.Equal(t, want, instances[i].Test.Outputs.Snapshot, "instance %d", i)
	}
}
