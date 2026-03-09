package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mhaynie/bats-declarative/schema"
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

func TestSetupFixtures(t *testing.T) {
	tmp := t.TempDir()
	test := &schema.Test{
		Cmd: "echo hi",
		Inputs: schema.InputBlock{
			Files: map[string]string{
				"input.txt": "hello",
			},
		},
		Outputs: schema.OutputBlock{
			Files: map[string]schema.FileCheck{
				"output.txt": {Match: []string{"result"}},
			},
		},
	}

	ctx, err := SetupFixtures(tmp, 0, test)
	require.Nil(t, err)

	// Input file should exist
	content, err := os.ReadFile(ctx.InputPaths["input.txt"])
	require.Nil(t, err)
	assert.Equal(t, "hello", string(content))

	// Output path should be set but file should not exist yet
	assert.Contains(t, ctx.OutputPaths["output.txt"], "outputs/output.txt")
	_, err = os.Stat(ctx.OutputPaths["output.txt"])
	assert.True(t, os.IsNotExist(err))
}

func TestSetupFixturesNoFiles(t *testing.T) {
	tmp := t.TempDir()
	test := &schema.Test{Cmd: "echo hi"}

	ctx, err := SetupFixtures(tmp, 0, test)
	require.Nil(t, err)
	assert.Empty(t, ctx.InputPaths)
	assert.Empty(t, ctx.OutputPaths)
}

func TestSetupFixturesNestedInputFile(t *testing.T) {
	tmp := t.TempDir()
	test := &schema.Test{
		Cmd: "echo hi",
		Inputs: schema.InputBlock{
			Files: map[string]string{
				"sub/dir/file.txt": "nested",
			},
		},
	}

	ctx, err := SetupFixtures(tmp, 0, test)
	require.Nil(t, err)

	content, err := os.ReadFile(ctx.InputPaths["sub/dir/file.txt"])
	require.Nil(t, err)
	assert.Equal(t, "nested", string(content))
}

func TestExpandPlaceholders(t *testing.T) {
	ctx := &TestContext{
		InputPaths:  map[string]string{"input.txt": "/tmp/test/inputs/input.txt"},
		OutputPaths: map[string]string{"output.txt": "/tmp/test/outputs/output.txt"},
	}

	result := ExpandPlaceholders("cat {inputs.input.txt} > {outputs.output.txt}", ctx)
	assert.Equal(t, "cat /tmp/test/inputs/input.txt > /tmp/test/outputs/output.txt", result)
}

func TestExpandPlaceholdersUnknown(t *testing.T) {
	ctx := &TestContext{
		InputPaths:  map[string]string{},
		OutputPaths: map[string]string{},
	}

	result := ExpandPlaceholders("cat {inputs.missing}", ctx)
	assert.Equal(t, "cat {inputs.missing}", result)
}

func TestCleanup(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "cleanup-test")
	require.Nil(t, os.MkdirAll(dir, 0755))
	require.Nil(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hi"), 0644))

	require.Nil(t, Cleanup(dir))
	_, err := os.Stat(dir)
	assert.True(t, os.IsNotExist(err))
}
