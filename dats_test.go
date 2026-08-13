package dats

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/dats/runner"
)

// writeSuite writes a .dats file into a fresh temp directory and returns its
// path. Suites here run unsandboxed: these tests exercise the library's own
// contract, not the backends (runner/sandbox_run_test.go covers those), and
// the host is where the test binary already is.
func writeSuite(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "suite.dats")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

// minimalSuite is the smallest valid suite: dats rejects a file with no
// tests, so "nothing to assert" still needs one command.
const minimalSuite = `
tests:
	- desc: trivial
	  cmd: "true"
`

// hostOpts is the common shape: run these files, on the host, capturing
// output.
func hostOpts(out *bytes.Buffer, paths ...string) Options {
	return Options{
		Paths:   paths,
		Output:  out,
		Sandbox: Sandbox{Mode: runner.SandboxNone},
	}
}

func TestRunReportsFailingTestsInTheResultNotAsAnError(t *testing.T) {
	// The library's central contract: a red suite is a Result, not an error.
	// A caller must never have to string-match or errors.Is a sentinel to
	// tell "your tests failed" from "dats could not run them".
	suite := writeSuite(t, `
tests:
	- desc: passes
	  cmd: echo hello
	  outputs:
		stdout:
			- hello
	- desc: fails
	  cmd: echo nope
	  outputs:
		stdout:
			- expected-this-instead
`)

	var out bytes.Buffer
	res, err := Run(context.Background(), hostOpts(&out, suite))
	require.NoError(t, err)
	assert.Equal(t, 1, res.Passed)
	assert.Equal(t, 1, res.Failed)
	assert.False(t, res.Ok())
	require.Len(t, res.Files, 1)
	assert.Positive(t, res.Wall)
	assert.Contains(t, out.String(), "passes")
}

func TestRunAllPassing(t *testing.T) {
	suite := writeSuite(t, `
tests:
	- desc: passes
	  cmd: echo hello
	  outputs:
		stdout:
			- hello
`)

	var out bytes.Buffer
	res, err := Run(context.Background(), hostOpts(&out, suite))
	require.NoError(t, err)
	assert.True(t, res.Ok())
	assert.Equal(t, 1, res.Passed)
	assert.Zero(t, res.Failed)
	// The totals line is for multi-file runs only, exactly as the CLI prints
	// it -- a single file already ends in its own summary.
	assert.NotContains(t, out.String(), "Total:")
}

func TestRunTeardownFailureFailsTheRunEvenWithEveryTestPassing(t *testing.T) {
	// Ok() is not "Failed == 0": a broken teardown is a failed run.
	suite := writeSuite(t, `
teardown:
	- exit 3
tests:
	- desc: passes
	  cmd: echo hello
	  outputs:
		stdout:
			- hello
`)

	var out bytes.Buffer
	res, err := Run(context.Background(), hostOpts(&out, suite))
	require.NoError(t, err)
	assert.Zero(t, res.Failed)
	assert.False(t, res.Ok(), "a teardown failure must fail the run")
}

func TestRunTotalsAcrossFiles(t *testing.T) {
	a := writeSuite(t, `
tests:
	- desc: a
	  cmd: echo a
	  outputs:
		stdout:
			- a
`)
	b := writeSuite(t, `
tests:
	- desc: b
	  cmd: echo b
	  outputs:
		stdout:
			- nope
`)

	var out bytes.Buffer
	res, err := Run(context.Background(), hostOpts(&out, a, b))
	require.NoError(t, err)
	assert.Equal(t, 1, res.Passed)
	assert.Equal(t, 1, res.Failed)
	assert.Len(t, res.Files, 2)
	assert.Contains(t, out.String(), "Total: 1/2 passed, 1 failed")
}

func TestRunEnvAppliesToTestsAndHooks(t *testing.T) {
	// Options.Env reaches every command, hooks included -- the setup command
	// asserts it too, and a setup failure would fail every test below.
	suite := writeSuite(t, `
setup:
	- "[ \"$FROM_CALLER\" = \"yes\" ]"
tests:
	- desc: sees the caller env
	  cmd: echo "$FROM_CALLER"
	  outputs:
		stdout:
			- yes
`)

	var out bytes.Buffer
	opts := hostOpts(&out, suite)
	opts.Env = []string{"FROM_CALLER=yes"}
	res, err := Run(context.Background(), opts)
	require.NoError(t, err)
	assert.True(t, res.Ok(), out.String())
}

func TestRunEnvEmptyValueClearsAnInheritedVariable(t *testing.T) {
	// The go-toolchain case: plumbing the parent must not hand to children
	// (GOCACHEPROG and friends) is cleared by an empty value.
	t.Setenv("DATS_TEST_INHERITED", "leaked")
	suite := writeSuite(t, `
tests:
	- desc: does not inherit
	  cmd: echo "[$DATS_TEST_INHERITED]"
	  outputs:
		stdout:
			- "[]"
`)

	var out bytes.Buffer
	opts := hostOpts(&out, suite)
	opts.Env = []string{"DATS_TEST_INHERITED="}
	res, err := Run(context.Background(), opts)
	require.NoError(t, err)
	assert.True(t, res.Ok(), out.String())
}

func TestRunTestEnvWinsOverCallerEnv(t *testing.T) {
	suite := writeSuite(t, `
tests:
	- desc: file wins
	  cmd: echo "$WHO"
	  inputs:
		env:
			WHO: file
	  outputs:
		stdout:
			- file
`)

	var out bytes.Buffer
	opts := hostOpts(&out, suite)
	opts.Env = []string{"WHO=caller"}
	res, err := Run(context.Background(), opts)
	require.NoError(t, err)
	assert.True(t, res.Ok(), out.String())
}

func TestRunJobsModeMatchesSerialOutcome(t *testing.T) {
	suite := writeSuite(t, `
tests:
	- desc: one
	  cmd: echo one
	  outputs:
		stdout:
			- one
	- desc: two
	  cmd: echo two
	  outputs:
		stdout:
			- two
`)

	var out bytes.Buffer
	opts := hostOpts(&out, suite)
	opts.Jobs = 2
	res, err := Run(context.Background(), opts)
	require.NoError(t, err)
	assert.True(t, res.Ok())
	assert.Equal(t, 2, res.Passed)
}

func TestRunHardErrors(t *testing.T) {
	var out bytes.Buffer

	t.Run("missing path", func(t *testing.T) {
		_, err := Run(context.Background(), hostOpts(&out, filepath.Join(t.TempDir(), "nope.dats")))
		require.Error(t, err)
	})

	t.Run("unparsable file", func(t *testing.T) {
		suite := writeSuite(t, "tests:\n\t- this is not a test\n")
		_, err := Run(context.Background(), hostOpts(&out, suite))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "running ")
	})

	t.Run("unknown sandbox mode", func(t *testing.T) {
		opts := hostOpts(&out, writeSuite(t, minimalSuite))
		opts.Sandbox = Sandbox{Mode: "sandcastle"}
		_, err := Run(context.Background(), opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown sandbox mode")
	})

	t.Run("invalid jobs", func(t *testing.T) {
		opts := hostOpts(&out, writeSuite(t, minimalSuite))
		opts.Jobs = -1
		// Negative jobs is not "invalid": Run treats anything below 1 as
		// serial, the same reading the CLI's absent flag gets.
		res, err := Run(context.Background(), opts)
		require.NoError(t, err)
		assert.True(t, res.Ok())
	})
}

func TestZeroSandboxIsAuto(t *testing.T) {
	// The safe default lives here: a caller that says nothing about the
	// sandbox gets one, not a host-level run. Weakening this silently
	// weakens every consumer that links the library.
	cfg, err := Sandbox{}.config()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, runner.SandboxAuto, cfg.Mode)
	// No image named: the file may pick one, and the runner falls back to
	// DefaultSandboxImage when it does not.
	assert.Equal(t, "", cfg.Image)

	// And the zero Options value carries that same default.
	cfg, err = (Options{}).Sandbox.config()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, runner.SandboxAuto, cfg.Mode)
}

func TestSandboxConfig(t *testing.T) {
	t.Run("none opts out", func(t *testing.T) {
		cfg, err := Sandbox{Mode: runner.SandboxNone}.config()
		require.NoError(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("explicit backend and image", func(t *testing.T) {
		cfg, err := Sandbox{Mode: runner.SandboxDocker, Image: "golang:1.25"}.config()
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, runner.SandboxDocker, cfg.Mode)
		assert.Equal(t, "golang:1.25", cfg.Image)
	})

	t.Run("unknown mode", func(t *testing.T) {
		_, err := Sandbox{Mode: "nope"}.config()
		require.Error(t, err)
	})
}

func TestResultReports(t *testing.T) {
	suite := writeSuite(t, `
tests:
	- desc: passes
	  cmd: echo hello
	  outputs:
		stdout:
			- hello
`)

	var out bytes.Buffer
	res, err := Run(context.Background(), hostOpts(&out, suite))
	require.NoError(t, err)

	var jsonBuf bytes.Buffer
	require.NoError(t, res.WriteJSON(&jsonBuf))
	var doc map[string]any
	require.NoError(t, json.Unmarshal(jsonBuf.Bytes(), &doc))

	var xmlBuf bytes.Buffer
	require.NoError(t, res.WriteJUnit(&xmlBuf))
	assert.True(t, strings.Contains(xmlBuf.String(), "<testsuites"), xmlBuf.String())
}

func TestRunUpdateRewritesGoldensAndCountsThem(t *testing.T) {
	dir := t.TempDir()
	suite := filepath.Join(dir, "suite.dats")
	require.NoError(t, os.WriteFile(suite, []byte(`
tests:
	- desc: snapshots stdout
	  cmd: echo hello
	  outputs:
		snapshot:
			stdout: true
`), 0o644))

	var out bytes.Buffer
	opts := hostOpts(&out, suite)
	opts.Update = true
	res, err := Run(context.Background(), opts)
	require.NoError(t, err)
	assert.True(t, res.Ok(), out.String())
	assert.Equal(t, 1, res.UpdatedGoldens)
	assert.Contains(t, out.String(), "Updated 1 golden file(s)")

	// The written golden now makes an ordinary (non-update) run pass, and
	// nothing is reported as updated.
	out.Reset()
	res, err = Run(context.Background(), hostOpts(&out, suite))
	require.NoError(t, err)
	assert.True(t, res.Ok(), out.String())
	assert.Zero(t, res.UpdatedGoldens)
}

func TestRunDiscoversFromWorkingDirectoryWhenNoPathsGiven(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.dats"), []byte(`
tests:
	- desc: passes
	  cmd: echo hello
	  outputs:
		stdout:
			- hello
`), 0o644))
	t.Chdir(dir)

	var out bytes.Buffer
	res, err := Run(context.Background(), Options{
		Output:  &out,
		Sandbox: Sandbox{Mode: runner.SandboxNone},
	})
	require.NoError(t, err)
	assert.True(t, res.Ok(), out.String())
	assert.Equal(t, 1, res.Passed)
}

func TestRunNilOutputDoesNotPanic(t *testing.T) {
	// Output defaults to os.Stdout; the point is that the zero value is
	// usable, not where the bytes land.
	suite := writeSuite(t, minimalSuite)
	res, err := Run(context.Background(), Options{
		Paths:   []string{suite},
		Sandbox: Sandbox{Mode: runner.SandboxNone},
	})
	require.NoError(t, err)
	assert.True(t, res.Ok())
}

func TestRunCanceledContextFailsInstances(t *testing.T) {
	suite := writeSuite(t, `
tests:
	- desc: sleeps
	  cmd: sleep 30
`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out bytes.Buffer
	res, err := Run(ctx, hostOpts(&out, suite))
	require.NoError(t, err)
	assert.False(t, res.Ok())
	assert.Equal(t, 1, res.Failed)
}

func TestValidate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		require.NoError(t, Validate([]string{writeSuite(t, minimalSuite)}))
	})

	t.Run("invalid", func(t *testing.T) {
		err := Validate([]string{writeSuite(t, "tests:\n\t- this is not a test\n")})
		require.Error(t, err)
	})

	t.Run("unresolvable path", func(t *testing.T) {
		require.Error(t, Validate([]string{filepath.Join(t.TempDir(), "nope.dats")}))
	})
}
