// Package dats runs .dats declarative CLI test suites.
//
// This is the library entry point, and it is the whole product: the dats
// binary is a thin cobra wrapper around Run, and any Go program that wants
// to run suites -- a build pipeline, an editor integration, a test harness --
// links this package instead of shelling out to a downloaded executable.
//
//	res, err := dats.Run(context.Background(), dats.Options{
//	        Paths:  []string{"dats"},
//	        Output: os.Stdout,
//	})
//	if err != nil {
//	        return err // hard error: bad path, parse failure, unusable sandbox
//	}
//	if !res.Ok() {
//	        return fmt.Errorf("%d/%d tests failed", res.Failed, res.Passed+res.Failed)
//	}
//
// Run reports failing TESTS in the Result and reserves error for failures of
// the run itself, so a caller never has to pattern-match a sentinel to tell
// "your suite is red" from "dats could not run it".
//
// The zero Options value is a safe, sandboxed run of every suite discovered
// under the working directory. In particular the zero Sandbox is auto
// (bubblewrap, then seatbelt, then docker) -- the same default the CLI has,
// so a library caller does not silently get weaker isolation than a
// developer typing `dats test`. Opting out is explicit: Sandbox{Mode:
// runner.SandboxNone}.
package dats

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/wow-look-at-my/dats/report"
	"github.com/wow-look-at-my/dats/runner"
	"github.com/wow-look-at-my/dats/schema"
)

// Options configures one Run. The zero value is valid: discover every suite
// under the working directory, run it serially, sandboxed, writing progress
// to os.Stdout.
type Options struct {
	// Paths are the .dats files and directories to run. Empty discovers
	// suites recursively from the working directory (and is an error when
	// there are none, exactly like the CLI).
	Paths []string

	// Output receives the human-readable run report -- per-file headers,
	// per-test results, summaries. Nil means os.Stdout. It is the only
	// stream Run writes to; nothing goes to os.Stderr except discovery
	// warnings for unreadable directories.
	Output io.Writer

	// Jobs runs up to N test commands concurrently across all files. 0 (the
	// default) runs everything serially, which keeps output deterministic
	// and is what a build pipeline usually wants.
	Jobs int

	// Verbose prints each command and its output, passing or not.
	Verbose bool

	// Update rewrites snapshot goldens from actual output instead of failing
	// mismatches, and prunes stale ones. A caller that runs suites as a gate
	// must leave this false: with it set, a red run rewrites the very
	// evidence it should have failed on.
	Update bool

	// KeepTemp leaves each file's temp directory behind (and prints its
	// path) for debugging.
	KeepTemp bool

	// CoverDir sets GOCOVERDIR on every executed command to collect coverage
	// from Go binaries under test. The directory is created if needed.
	CoverDir string

	// Env are extra KEY=VALUE entries applied to every executed command --
	// test commands and file-level setup/teardown hooks alike -- on top of
	// the inherited environment. An entry with an empty value (KEY=) clears
	// the inherited one, which is how a caller strips plumbing its children
	// must not inherit.
	Env []string

	// Sandbox selects the isolation every command runs under. The zero value
	// is auto; see the package comment.
	Sandbox Sandbox
}

// Sandbox selects the sandbox backend for a run. The zero value means auto:
// bubblewrap, then seatbelt, then docker. This is the only place a sandbox is
// turned off: a file can narrow its own (cut the network, pin a docker image),
// never switch it off.
type Sandbox struct {
	// Mode is the backend: runner.SandboxAuto (or ""), SandboxBwrap,
	// SandboxSeatbelt, SandboxDocker, or SandboxNone to run on the host.
	Mode runner.SandboxMode

	// Image is the container image the docker backend runs commands in.
	// Empty leaves the choice to the file (and runner.DefaultSandboxImage
	// when it names none); set, it is the caller's and a file's `image:`
	// cannot displace it. The other backends ignore it.
	Image string
}

// config resolves the sandbox into the runner's configuration, or nil when
// the run opted out. Nothing is probed here: the backend is resolved lazily,
// on the first file that actually needs one.
func (s Sandbox) config() (*runner.SandboxConfig, error) {
	mode := s.Mode
	if mode == "" {
		mode = runner.SandboxAuto
	}
	mode, err := runner.ParseSandboxMode(string(mode))
	if err != nil {
		return nil, err
	}
	if mode == runner.SandboxNone {
		return nil, nil
	}
	// Image travels verbatim, empty included: "" is the caller naming none,
	// which is what lets a file pick one without overruling a caller who did.
	return runner.NewSandboxConfig(mode, s.Image), nil
}

// Result is the outcome of a Run: the per-file results plus the totals the
// caller would otherwise have to add up itself.
type Result struct {
	// Files holds one entry per executed file, in the order they ran.
	Files []*runner.FileResult

	// Passed and Failed count test instances across every file. A file whose
	// setup failed contributes each of its instances to Failed.
	Passed int
	Failed int

	// UpdatedGoldens and PrunedGoldens count snapshot golden files written
	// and removed under Options.Update. Always zero otherwise.
	UpdatedGoldens int
	PrunedGoldens  int

	// Wall is how long the execution phase took, and is what the JUnit and
	// JSON reports record as the run duration.
	Wall time.Duration
}

// Ok reports whether the whole run passed: every test passed, every file's
// setup succeeded, and no teardown command failed. A teardown failure fails
// the run even when every test passed, so this is not simply Failed == 0.
func (r *Result) Ok() bool {
	for _, f := range r.Files {
		if !f.Ok() {
			return false
		}
	}
	return true
}

// WriteJUnit renders the run as a JUnit XML document.
func (r *Result) WriteJUnit(w io.Writer) error {
	return report.WriteJUnit(w, r.Files, r.Wall)
}

// WriteJSON renders the run as a JSON document.
func (r *Result) WriteJSON(w io.Writer) error {
	return report.WriteJSON(w, r.Files, r.Wall)
}

// Validate parses every resolved file without running anything, returning one
// error per file that failed to parse (joined). It is the library form of
// `dats syntax`.
func Validate(paths []string) error {
	files, err := FindFiles(paths)
	if err != nil {
		return err
	}
	for _, path := range files {
		if _, err := schema.ParseFile(path); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

// Run executes every resolved suite and returns the run's outcome.
//
// A returned error means the run itself could not be carried out -- an
// unreadable or non-.dats path, a file that failed to parse, a file that
// demands a sandbox on a host that has none, an invalid Jobs value. Failing
// tests are NOT an error: they are counted in the Result, and Result.Ok
// reports the verdict.
//
// Canceling ctx kills in-flight commands (whole process groups); each file's
// teardown still runs, and the aborted instances report as failures.
func Run(ctx context.Context, opts Options) (*Result, error) {
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}

	sandbox, err := opts.Sandbox.config()
	if err != nil {
		return nil, err
	}

	files, err := FindFiles(opts.Paths)
	if err != nil {
		return nil, err
	}

	r := runner.NewRunner(out, opts.Verbose, opts.KeepTemp, opts.CoverDir)
	r.Update = opts.Update
	r.Sandbox = sandbox
	r.Env = opts.Env

	// Wall time of the execution phase, consumed only by the reports; the
	// human-readable output never mentions it.
	start := time.Now()

	var results []*runner.FileResult
	if opts.Jobs > 0 {
		results, err = r.RunFilesParallel(ctx, files, opts.Jobs)
		if err != nil {
			// Already carries the "running <path>:" context.
			return nil, err
		}
	} else {
		for _, path := range files {
			result, err := r.RunFile(ctx, path)
			if err != nil {
				return nil, fmt.Errorf("running %s: %w", path, err)
			}
			results = append(results, result)
		}
	}

	res := &Result{Files: results, Wall: time.Since(start)}
	for _, result := range results {
		res.Passed += result.Passed
		res.Failed += result.Failed
		for i := range result.Results {
			res.UpdatedGoldens += len(result.Results[i].UpdatedGoldens)
		}
		res.PrunedGoldens += len(result.PrunedGoldens)
	}

	if len(files) > 1 {
		fmt.Fprintf(out, "\nTotal: %d/%d passed", res.Passed, res.Passed+res.Failed)
		if res.Failed > 0 {
			fmt.Fprintf(out, ", %d failed", res.Failed)
		}
		fmt.Fprintln(out)
	}

	// Under Update, summarize the golden churn (writes and prunes were
	// already listed per file). Silent when nothing changed.
	if opts.Update && res.UpdatedGoldens+res.PrunedGoldens > 0 {
		fmt.Fprintf(out, "\nUpdated %d golden file(s)", res.UpdatedGoldens)
		if res.PrunedGoldens > 0 {
			fmt.Fprintf(out, ", pruned %d stale", res.PrunedGoldens)
		}
		fmt.Fprintln(out)
	}

	return res, nil
}
