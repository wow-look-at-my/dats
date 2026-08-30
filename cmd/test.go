package cmd

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/dats/runner"

	dats "github.com/wow-look-at-my/dats"
)

var (
	keepTemp bool
	coverDir string
)

// errTestsFailed signals that a test failed.
var errTestsFailed = errors.New("tests failed")

var testCmd = &cobra.Command{
	Use:   "test [files-or-dirs...]",
	Short: "Run tests from .dats files",
	Long: `Run the tests in the given .dats files and directories. This is the default
action, so "dats file.dats" and "dats test file.dats" are the same run.

Arguments may be files and directories in any mix. A directory is searched
recursively; discovery skips hidden directories and hidden .dats files, though
a file named explicitly is always accepted. Arguments are deduplicated by
absolute path, so a file named twice runs once. With no arguments, dats
discovers every .dats file under the working directory.

Every test in every file runs: there is no test selection or filtering, by
design. Each command runs under "bash -c" in dats' own working directory, with
the inherited environment plus the test's inputs.env, inside the run's sandbox,
and with its fixtures in a per-instance temp directory.

Results print in a TAP-like format on stdout. The exit status is 0 when every
test passed, and 1 when any test failed, any file-level setup or teardown hook
failed, or the run could not be carried out.

Depth: "dats docs cli" (flags, discovery, sandboxing, -j, output),
"dats docs format" (every .dats key), "dats docs reports" (--report-* files).`,
	Example: `  dats test                          # every .dats file under the tree
  dats test tests/ smoke.dats        # a directory and a file
  dats test -j1 tests/               # one command at a time
  dats test --update tests/          # rewrite snapshot goldens from output
  dats test --report-json out.json tests/`,
	RunE: runTestsCommand,
}

func runTestsCommand(cmd *cobra.Command, args []string) error {
	jobs, err := resolveJobs(cmd.Flags())
	if err != nil {
		return err
	}
	sandbox, err := resolveSandbox(cmd.Flags())
	if err != nil {
		return err
	}
	sshTarget, err := resolveSSH(cmd.Flags())
	if err != nil {
		return err
	}
	return runTests(context.Background(), args, os.Stdout, jobs, sandbox, sshTarget)
}

func runTests(ctx context.Context, args []string, out io.Writer, jobs int, sandbox *runner.SandboxConfig, sshTarget string) error {
	opts := dats.Options{
		Paths:    args,
		Output:   out,
		Jobs:     jobs,
		SSH:      dats.SSH{Target: sshTarget, Allow: approveSSHTarget},
		Verbose:  verbose,
		Update:   updateGoldens,
		KeepTemp: keepTemp,
		CoverDir: coverDir,
		Sandbox:  dats.Sandbox{Mode: runner.SandboxNone},
	}
	if sandbox != nil {
		opts.Sandbox = dats.Sandbox{Mode: sandbox.Mode, Image: sandbox.Image}
	}

	result, err := dats.Run(ctx, opts)
	if err != nil {
		return err
	}

	if err := writeReports(result.Files, result.Wall); err != nil {
		return err
	}

	if !result.Ok() {
		return errTestsFailed
	}

	return nil
}

func init() {
	// --keep-temp and --coverdir are registered as persistent flags on rootCmd (see root.go) and inherited here.
	rootCmd.AddCommand(testCmd)
}
