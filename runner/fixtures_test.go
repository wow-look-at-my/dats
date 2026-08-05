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

	ctx, err := SetupFixtures(tmp, 0, test, "")
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

	ctx, err := SetupFixtures(tmp, 0, test, "")
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

	ctx, err := SetupFixtures(tmp, 0, test, "")
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

	ctx, err := SetupFixtures(tmp, 0, test, "")
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

	ctx, err := SetupFixtures(tmp, 0, test, "")
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

	ctx, err := SetupFixtures(tmp, 0, test, "")
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

func TestExpandSharedPlaceholders(t *testing.T) {
	assert.Equal(t, "cat /tmp/f/shared/config.json",
		ExpandSharedPlaceholders("cat {shared.config.json}", "/tmp/f/shared"))
	// Only {shared.X} expands: the per-test namespaces stay verbatim.
	assert.Equal(t, "cat {inputs.a} {outputs.b}",
		ExpandSharedPlaceholders("cat {inputs.a} {outputs.b}", "/tmp/f/shared"))
	// Non-local names are left untouched.
	assert.Equal(t, "cat {shared.../escape}",
		ExpandSharedPlaceholders("cat {shared.../escape}", "/tmp/f/shared"))
	assert.Equal(t, "cat {shared./abs}",
		ExpandSharedPlaceholders("cat {shared./abs}", "/tmp/f/shared"))
	// An empty sharedDir leaves everything untouched.
	assert.Equal(t, "cat {shared.config.json}",
		ExpandSharedPlaceholders("cat {shared.config.json}", ""))
}

func TestExpandPlaceholdersSharedNamespace(t *testing.T) {
	ctx := &TestContext{SharedDir: "/tmp/f/shared"}
	assert.Equal(t, "cat /tmp/f/shared/cfg.txt", ExpandPlaceholders("cat {shared.cfg.txt}", ctx))
	// Non-local shared names stay verbatim in the per-test expansion too.
	assert.Equal(t, "cat {shared.../escape}", ExpandPlaceholders("cat {shared.../escape}", ctx))
}

func TestSetupFixturesSetsSharedDir(t *testing.T) {
	tmp := t.TempDir()
	ctx, err := SetupFixtures(tmp, 0, &schema.Test{Cmd: "true"}, "")
	require.Nil(t, err)
	assert.Equal(t, filepath.Join(tmp, "shared"), ctx.SharedDir)
	// The shared directory exists so {shared.X} resolves to a writable path
	// even when RunTest is driven directly.
	info, err := os.Stat(ctx.SharedDir)
	require.Nil(t, err)
	assert.True(t, info.IsDir())
}

func TestSetupSharedFixtures(t *testing.T) {
	sharedDir := filepath.Join(t.TempDir(), "shared")
	require.Nil(t, os.MkdirAll(sharedDir, 0755))
	files := map[string]string{
		"config.json":  `{"debug": true}`,
		"sub/deep.txt": "path: {shared.config.json}",
	}
	require.Nil(t, SetupSharedFixtures(sharedDir, files, nil, ""))

	data, err := os.ReadFile(filepath.Join(sharedDir, "config.json"))
	require.Nil(t, err)
	assert.Equal(t, `{"debug": true}`, string(data))

	// Nested parents are created; contents expand only {shared.X}.
	data, err = os.ReadFile(filepath.Join(sharedDir, "sub", "deep.txt"))
	require.Nil(t, err)
	assert.Equal(t, "path: "+filepath.Join(sharedDir, "config.json"), string(data))
}

func TestSetupSharedFixturesLeavesTestNamespacesVerbatim(t *testing.T) {
	sharedDir := filepath.Join(t.TempDir(), "shared")
	require.Nil(t, os.MkdirAll(sharedDir, 0755))
	require.Nil(t, SetupSharedFixtures(sharedDir, map[string]string{
		"script.sh": "cp {inputs.a.txt} {outputs.b.txt}",
	}, nil, ""))
	data, err := os.ReadFile(filepath.Join(sharedDir, "script.sh"))
	require.Nil(t, err)
	assert.Equal(t, "cp {inputs.a.txt} {outputs.b.txt}", string(data))
}

func TestSetupSharedFixturesRejectsNonLocalNames(t *testing.T) {
	tmp := t.TempDir()
	sharedDir := filepath.Join(tmp, "shared")
	require.Nil(t, os.MkdirAll(sharedDir, 0755))
	for _, name := range []string{"../evil.txt", "/abs/evil.txt", ".."} {
		err := SetupSharedFixtures(sharedDir, map[string]string{name: "pwned"}, nil, "")
		require.NotNil(t, err, "shared name %q must be rejected", name)
		assert.Contains(t, err.Error(), "must be a relative path that stays inside the shared directory")
	}
	// Nothing may be written at the escaped location.
	_, statErr := os.Stat(filepath.Join(tmp, "evil.txt"))
	assert.True(t, os.IsNotExist(statErr), "traversal shared file must not be written outside the shared directory")
}

func TestSetupFixturesRejectsTraversalInputName(t *testing.T) {
	tmp := t.TempDir()
	for _, name := range []string{"../../evil.txt", "/abs/evil.txt", ".."} {
		test := &schema.Test{
			Cmd:    "true",
			Inputs: schema.InputBlock{Files: map[string]string{name: "pwned"}},
		}
		_, err := SetupFixtures(tmp, 0, test, "")
		require.NotNil(t, err, "input name %q must be rejected", name)
		assert.Contains(t, err.Error(), "input file name")
		assert.Contains(t, err.Error(), "must be a relative path that stays inside the test directory")
	}
	// Nothing may be written at the escaped location.
	_, statErr := os.Stat(filepath.Join(tmp, "evil.txt"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestSetupFixturesRejectsTraversalOutputName(t *testing.T) {
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "true",
		Outputs: schema.OutputBlock{
			Files: map[string]schema.FileCheck{"../../evil.txt": {}},
		},
	}
	_, err := SetupFixtures(tmp, 0, test, "")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "output file name")
	assert.Contains(t, err.Error(), "must be a relative path that stays inside the test directory")

	// !files names are validated the same way.
	test2 := &schema.Test{
		Cmd: "true",
		Outputs: schema.OutputBlock{
			NotFiles: map[string]schema.FileCheck{"/etc/passwd": {}},
		},
	}
	_, err = SetupFixtures(tmp, 1, test2, "")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "output file name")
}

func TestSetupFixturesCreatesNestedOutputParents(t *testing.T) {
	tmp := t.TempDir()
	test := &schema.Test{
		Cmd: "true",
		Outputs: schema.OutputBlock{
			Files: map[string]schema.FileCheck{"sub/dir/out.txt": {}},
		},
	}

	ctx, err := SetupFixtures(tmp, 0, test, "")
	require.Nil(t, err)

	// The registered output's parent directory exists so commands can write
	// the file directly.
	info, statErr := os.Stat(filepath.Dir(ctx.OutputPaths["sub/dir/out.txt"]))
	require.Nil(t, statErr)
	assert.True(t, info.IsDir())
}

func TestExpandPlaceholdersNonLocalOutputLeftVerbatim(t *testing.T) {
	ctx := &TestContext{
		InputPaths:  map[string]string{},
		OutputsDir:  "/tmp/test/outputs",
		OutputPaths: map[string]string{},
	}

	// Unregistered output names only resolve into the outputs directory when
	// they stay inside it; traversal and absolute names are left verbatim.
	assert.Equal(t, "cat {outputs.../../evil}", ExpandPlaceholders("cat {outputs.../../evil}", ctx))
	assert.Equal(t, "cat {outputs./etc/passwd}", ExpandPlaceholders("cat {outputs./etc/passwd}", ctx))
}

func TestSetupFixturesCopy(t *testing.T) {
	sourceDir := t.TempDir()
	require.Nil(t, os.WriteFile(filepath.Join(sourceDir, "script.sh"), []byte("#!/bin/sh\necho hi\n"), 0755))

	tmp := t.TempDir()
	test := &schema.Test{
		Cmd: "cat {inputs.script.sh}",
		Inputs: schema.InputBlock{
			Copy: map[string]string{"script.sh": "script.sh"},
		},
	}
	ctx, err := SetupFixtures(tmp, 0, test, sourceDir)
	require.Nil(t, err)

	content, err := os.ReadFile(ctx.InputPaths["script.sh"])
	require.Nil(t, err)
	assert.Equal(t, "#!/bin/sh\necho hi\n", string(content))

	// The copy preserves the source's permission bits (the executable bit
	// matters for a script pulled in to be run).
	info, err := os.Stat(ctx.InputPaths["script.sh"])
	require.Nil(t, err)
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
}

func TestSetupFixturesCopyAbsoluteSource(t *testing.T) {
	sourceDir := t.TempDir()
	abs := filepath.Join(t.TempDir(), "data.bin")
	require.Nil(t, os.WriteFile(abs, []byte("binary data"), 0644))

	tmp := t.TempDir()
	test := &schema.Test{
		Cmd:    "true",
		Inputs: schema.InputBlock{Copy: map[string]string{"data.bin": abs}},
	}
	ctx, err := SetupFixtures(tmp, 0, test, sourceDir)
	require.Nil(t, err)

	content, err := os.ReadFile(ctx.InputPaths["data.bin"])
	require.Nil(t, err)
	assert.Equal(t, "binary data", string(content))
}

func TestSetupFixturesCopyMissingSourceFails(t *testing.T) {
	tmp := t.TempDir()
	test := &schema.Test{
		Cmd:    "true",
		Inputs: schema.InputBlock{Copy: map[string]string{"data.txt": "nope/does-not-exist.txt"}},
	}
	_, err := SetupFixtures(tmp, 0, test, t.TempDir())
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "copying input file data.txt")
}

func TestSetupFixturesCopyCollisionWithFiles(t *testing.T) {
	// ParseFile already rejects this; SetupFixtures checks again for a
	// library caller constructing a Test directly.
	tmp := t.TempDir()
	test := &schema.Test{
		Cmd: "true",
		Inputs: schema.InputBlock{
			Files: map[string]string{"dup.txt": "content"},
			Copy:  map[string]string{"dup.txt": "some/source.txt"},
		},
	}
	_, err := SetupFixtures(tmp, 0, test, t.TempDir())
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), `"dup.txt" is declared under both files and copy`)
}

func TestSetupFixturesCopyNestedDestination(t *testing.T) {
	sourceDir := t.TempDir()
	require.Nil(t, os.WriteFile(filepath.Join(sourceDir, "src.txt"), []byte("nested"), 0644))

	tmp := t.TempDir()
	test := &schema.Test{
		Cmd:    "true",
		Inputs: schema.InputBlock{Copy: map[string]string{"sub/dir/dest.txt": "src.txt"}},
	}
	ctx, err := SetupFixtures(tmp, 0, test, sourceDir)
	require.Nil(t, err)

	content, err := os.ReadFile(ctx.InputPaths["sub/dir/dest.txt"])
	require.Nil(t, err)
	assert.Equal(t, "nested", string(content))
}

func TestSetupSharedFixturesCopy(t *testing.T) {
	sourceDir := t.TempDir()
	require.Nil(t, os.WriteFile(filepath.Join(sourceDir, "fixture.bin"), []byte("shared binary"), 0644))

	sharedDir := filepath.Join(t.TempDir(), "shared")
	require.Nil(t, os.MkdirAll(sharedDir, 0755))
	require.Nil(t, SetupSharedFixtures(sharedDir, nil, map[string]string{"fixture.bin": "fixture.bin"}, sourceDir))

	content, err := os.ReadFile(filepath.Join(sharedDir, "fixture.bin"))
	require.Nil(t, err)
	assert.Equal(t, "shared binary", string(content))
}

func TestSetupSharedFixturesCopyRejectsTraversalName(t *testing.T) {
	sharedDir := filepath.Join(t.TempDir(), "shared")
	require.Nil(t, os.MkdirAll(sharedDir, 0755))
	err := SetupSharedFixtures(sharedDir, nil, map[string]string{"../evil.txt": "some/source.txt"}, t.TempDir())
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "must be a relative path that stays inside the shared directory")
}

func TestSetupSharedFixturesCopyCollisionWithFiles(t *testing.T) {
	sharedDir := filepath.Join(t.TempDir(), "shared")
	require.Nil(t, os.MkdirAll(sharedDir, 0755))
	err := SetupSharedFixtures(sharedDir,
		map[string]string{"dup.txt": "content"},
		map[string]string{"dup.txt": "some/source.txt"},
		t.TempDir())
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), `"dup.txt" is declared under both files and copy`)
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
