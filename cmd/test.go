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

// errTestsFailed signals that at least one test failed.
var errTestsFailed = errors.New("tests failed")

var testCmd = &cobra.Command{
	Use:   "test [files...]",
	Short: "Run tests from .dats files",
	Long: `Run tests defined in .dats files. If no files are specified,
recursively finds and runs all .dats files in the current directory tree.`,
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
