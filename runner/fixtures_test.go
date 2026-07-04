package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/dats/schema"
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

func TestSetupFixturesExpandsPlaceholdersInContents(t *testing.T) {
	tmp := t.TempDir()
	test := &schema.Test{
		Cmd: "bash {inputs.script.sh}",
		Inputs: schema.InputBlock{
			Files: map[string]string{
				"script.sh": "cp {inputs.data.txt} {outputs.result.txt} # keep {inputs.missing} and {braces} and {}",
				"data.txt":  "hello",
			},
		},
		Outputs: schema.OutputBlock{
			Files: map[string]schema.FileCheck{
				"result.txt": {Match: []string{"hello"}},
			},
		},
	}

	ctx, err := SetupFixtures(tmp, 0, test)
	require.Nil(t, err)

	// Placeholders in contents expand to the same paths as in cmd; unknown
	// input placeholders and non-placeholder braces are left untouched.
	content, err := os.ReadFile(ctx.InputPaths["script.sh"])
	require.Nil(t, err)
	want := "cp " + ctx.InputPaths["data.txt"] + " " + ctx.OutputPaths["result.txt"] +
		" # keep {inputs.missing} and {braces} and {}"
	assert.Equal(t, want, string(content))

	// Files without placeholders are written verbatim
	content, err = os.ReadFile(ctx.InputPaths["data.txt"])
	require.Nil(t, err)
	assert.Equal(t, "hello", string(content))
}

func TestSetupFixturesContentSelfReference(t *testing.T) {
	tmp := t.TempDir()
	test := &schema.Test{
		Cmd: "cat {inputs.self.txt}",
		Inputs: schema.InputBlock{
			Files: map[string]string{
				"self.txt": "I live at {inputs.self.txt}",
			},
		},
	}

	ctx, err := SetupFixtures(tmp, 0, test)
	require.Nil(t, err)

	content, err := os.ReadFile(ctx.InputPaths["self.txt"])
	require.Nil(t, err)
	assert.Equal(t, "I live at "+ctx.InputPaths["self.txt"], string(content))
}

func TestSetupFixturesOutputPlaceholderWithoutFilesCheck(t *testing.T) {
	tmp := t.TempDir()
	test := &schema.Test{
		Cmd: "cat {inputs.prog.txt}",
		Inputs: schema.InputBlock{
			Files: map[string]string{
				"prog.txt": "write to {outputs.data.txt}",
			},
		},
	}

	ctx, err := SetupFixtures(tmp, 0, test)
	require.Nil(t, err)

	// {outputs.X} resolves even when no files check references X
	content, err := os.ReadFile(ctx.InputPaths["prog.txt"])
	require.Nil(t, err)
	assert.Equal(t, "write to "+filepath.Join(ctx.OutputsDir, "data.txt"), string(content))

	// The outputs directory exists so the command can write there
	info, err := os.Stat(ctx.OutputsDir)
	require.Nil(t, err)
	assert.True(t, info.IsDir())
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

func TestExpandPlaceholdersUnregisteredOutput(t *testing.T) {
	ctx := &TestContext{
		InputPaths:  map[string]string{},
		OutputsDir:  "/tmp/test/outputs",
		OutputPaths: map[string]string{},
	}

	// Unregistered output names resolve into the outputs directory
	result := ExpandPlaceholders("touch {outputs.new.txt}", ctx)
	assert.Equal(t, "touch /tmp/test/outputs/new.txt", result)

	// Without an outputs directory (context not from SetupFixtures), the
	// placeholder is left untouched
	bare := &TestContext{InputPaths: map[string]string{}, OutputPaths: map[string]string{}}
	result = ExpandPlaceholders("touch {outputs.new.txt}", bare)
	assert.Equal(t, "touch {outputs.new.txt}", result)
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
