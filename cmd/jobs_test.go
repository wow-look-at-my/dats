package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newJobsProbe returns a fresh command with -j/--jobs registered exactly the
// way the real root command registers it, whose RunE records the resolved
// jobs value and the leftover positional args. A fresh instance per case
// keeps pflag's Changed state from bleeding between tests.
func newJobsProbe(jobs *int, positional *[]string) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "probe",
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveJobs(cmd.Flags())
			if err != nil {
				return err
			}
			*jobs = resolved
			*positional = args
			return nil
		},
	}
	registerJobsFlag(cmd.PersistentFlags())
	return cmd
}

func TestJobsFlagResolution(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantJobs int
		wantPos  []string
		wantErr  string
	}{
		{name: "absent means serial", args: []string{"pos"}, wantJobs: 0, wantPos: []string{"pos"}},
		{name: "bare -j means one worker per CPU", args: []string{"-j"}, wantJobs: runtime.NumCPU(), wantPos: []string{}},
		{name: "bare --jobs means one worker per CPU", args: []string{"--jobs"}, wantJobs: runtime.NumCPU(), wantPos: []string{}},
		{name: "attached -j4 binds 4", args: []string{"-j4", "a"}, wantJobs: 4, wantPos: []string{"a"}},
		{name: "-j=4 binds 4", args: []string{"-j=4"}, wantJobs: 4, wantPos: []string{}},
		{name: "--jobs=4 binds 4", args: []string{"--jobs=4"}, wantJobs: 4, wantPos: []string{}},
		{name: "explicit -j0 is an error", args: []string{"-j=0"}, wantErr: "at least 1"},
		{name: "explicit --jobs=0 is an error", args: []string{"--jobs=0"}, wantErr: "at least 1"},
		{name: "negative is an error", args: []string{"--jobs=-2"}, wantErr: "at least 1"},
		// The documented pflag trap: with NoOptDefVal set, a SPACE-separated
		// value does not bind to the flag. "-j 4" is bare -j (one worker per
		// CPU) plus a positional "4" -- the same trap as GNU make.
		{name: "space form -j 4 leaves 4 positional", args: []string{"-j", "4"}, wantJobs: runtime.NumCPU(), wantPos: []string{"4"}},
		{name: "space form --jobs 4 leaves 4 positional", args: []string{"--jobs", "4"}, wantJobs: runtime.NumCPU(), wantPos: []string{"4"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var jobs int
			var positional []string
			cmd := newJobsProbe(&jobs, &positional)
			// The full pipeline the binary uses: normalize argv, then parse.
			cmd.SetArgs(normalizeJobsShorthand(tt.args))
			err := cmd.Execute()
			if tt.wantErr != "" {
				require.NotNil(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.Nil(t, err)
			assert.Equal(t, tt.wantJobs, jobs)
			assert.Equal(t, tt.wantPos, positional)
		})
	}
}

// TestJobsAttachedShorthandNeedsNormalization pins WHY normalizeJobsShorthand
// exists: pflag resolves a shorthand with NoOptDefVal set BEFORE considering
// the attached "-farg" form, so a raw -j4 parses as bare -j followed by an
// unknown '4' shorthand instead of binding 4. If this ever starts passing,
// pflag changed and the normalization can go.
func TestJobsAttachedShorthandNeedsNormalization(t *testing.T) {
	var jobs int
	var positional []string
	cmd := newJobsProbe(&jobs, &positional)
	cmd.SetArgs([]string{"-j4"}) // deliberately NOT normalized
	err := cmd.Execute()
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "unknown shorthand flag")
}

func TestNormalizeJobsShorthand(t *testing.T) {
	assert.Equal(t, []string{"--jobs=4", "a.dats"}, normalizeJobsShorthand([]string{"-j4", "a.dats"}))
	assert.Equal(t, []string{"--jobs=16"}, normalizeJobsShorthand([]string{"-j16"}))
	// Only exact -jN tokens are rewritten; every other spelling is pflag's
	// business.
	assert.Equal(t, []string{"-j", "-j=4", "--jobs", "-jx", "-vj4"},
		normalizeJobsShorthand([]string{"-j", "-j=4", "--jobs", "-jx", "-vj4"}))
	// Everything after a "--" terminator is positional and stays untouched.
	assert.Equal(t, []string{"a.dats", "--", "-j4"}, normalizeJobsShorthand([]string{"a.dats", "--", "-j4"}))
	assert.Equal(t, []string{}, normalizeJobsShorthand([]string{}))
}

// TestRunTestsJobsOutputMatchesSerial is the determinism guarantee: the same
// corpus, run through the same runTests pipeline the CLI uses, must produce
// byte-identical output with and without -j when the outcomes are equal. The
// corpus is the repo's real example file -- 22 instances including matrix
// expansion, shared fixtures, setup, and teardown; nothing in it is
// timing-sensitive under parallel load.
func TestRunTestsJobsOutputMatchesSerial(t *testing.T) {
	example := filepath.Join("..", "examples", "example.dats")

	var serial, parallel bytes.Buffer
	require.Nil(t, runTests(context.Background(), []string{example}, &serial, 0, nil))
	require.Nil(t, runTests(context.Background(), []string{example}, &parallel, 4, nil))

	require.NotEmpty(t, serial.String())
	assert.Contains(t, serial.String(), "(22 tests)")
	assert.Contains(t, serial.String(), "22/22 passed")
	assert.Equal(t, serial.String(), parallel.String(),
		"jobs-mode output must be byte-identical to a serial run")
}

// TestRunTestsJobsMultiFileOutputMatchesSerial extends the byte-equality
// guarantee to a generated multi-file corpus with matrix names, per-file
// hooks, deterministic failures, and the multi-file Total line.
func TestRunTestsJobsMultiFileOutputMatchesSerial(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"a.dats": `shared:
  files:
    config.txt: from-shared
setup: echo prepared > {shared.gen.txt}
teardown: echo done
tests:
  - desc: greets {matrix.who} at {matrix.volume}
    cmd: echo "{matrix.who}-{matrix.volume}"
    matrix:
      who: [alice, bob]
      volume: [quiet, loud]
    outputs:
      stdout:
        - "{matrix.who}-{matrix.volume}"
  - desc: reads shared
    cmd: cat {shared.config.txt} {shared.gen.txt}
    outputs:
      stdout:
        - "from-shared"
        - "prepared"
`,
		"b.dats": `tests:
  - desc: deliberately fails
    cmd: echo wrong
    outputs:
      stdout:
        - "expected-text"
  - desc: passes
    cmd: echo fine
`,
		"c.dats": `tests:
  - desc: instance {matrix.i}
    cmd: echo "i={matrix.i}"
    matrix:
      i: [1, 2, 3]
    outputs:
      stdout:
        - "i={matrix.i}"
`,
	}
	var paths []string
	for _, name := range []string{"a.dats", "b.dats", "c.dats"} {
		path := filepath.Join(dir, name)
		require.Nil(t, os.WriteFile(path, []byte(files[name]), 0644))
		paths = append(paths, path)
	}

	var serial, parallel bytes.Buffer
	errSerial := runTests(context.Background(), paths, &serial, 0, nil)
	errParallel := runTests(context.Background(), paths, &parallel, 4, nil)

	// Equal outcomes in both modes, including the failing exit...
	assert.ErrorIs(t, errSerial, errTestsFailed)
	assert.ErrorIs(t, errParallel, errTestsFailed)
	// ...the multi-file total...
	assert.Contains(t, serial.String(), fmt.Sprintf("Total: %d/%d passed, 1 failed", 9, 10))
	// ...and byte-identical output.
	assert.Equal(t, serial.String(), parallel.String(),
		"jobs-mode multi-file output must be byte-identical to a serial run")
}
