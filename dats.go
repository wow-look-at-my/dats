// Package dats runs .dats declarative CLI test suites.
package dats

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/wow-look-at-my/dats/report"
	"github.com/wow-look-at-my/dats/runner"
	"github.com/wow-look-at-my/dats/schema"
)

// Options configures a single Run.
type Options struct {
	// Paths are the .dats files and directories to run.
	Paths []string

	// Output receives the human-readable run report -- per-file headers, per-test results, summaries.
	Output io.Writer

	// Jobs runs up to N test commands concurrently across all files.
	Jobs int

	// Verbose prints each command and its output, passing or not.
	Verbose bool

	// Update rewrites snapshot goldens from actual output instead of failing mismatches, and prunes stale ones.
	Update bool

	// KeepTemp leaves each file's temp directory behind (and prints its path) for debugging.
	KeepTemp bool

	// CoverDir sets GOCOVERDIR on every executed command to collect coverage from Go binaries under test.
	CoverDir string

	Env []string

	// Sandbox selects the isolation every command runs under.
	Sandbox Sandbox

	// SSH runs every command on another machine.
	SSH SSH
}

// SSH names the machine a run's commands execute on.
type SSH struct {
	// Target is [user@]host, as ssh spells it.
	Target string

	// Allow is consulted for a target a FILE named in its own ssh: block, never for Target.
	Allow func(datsPath, target string) error
}

// config resolves the run's ssh policy, or nil when nothing can go remote.
func (s SSH) config(coverDir string) (*runner.SSHManager, error) {
	if s.Target == "" && s.Allow == nil {
		return nil, nil
	}
	if s.Target != "" {
		if err := runner.ValidateSSHTarget(s.Target); err != nil {
			return nil, err
		}
	}
	// Coverage data is written by the command itself into a host directory that must outlive the run.
	if coverDir != "" && s.Target != "" {
		return nil, fmt.Errorf("--coverdir cannot be combined with --ssh: coverage is written on %s and never reaches this machine", s.Target)
	}
	return &runner.SSHManager{Target: s.Target, Allow: s.Allow}, nil
}

// Sandbox selects the sandbox backend for a run.
type Sandbox struct {
	Mode runner.SandboxMode

	// Image is the container image the docker backend runs commands in.
	Image string
}

// config resolves the sandbox into the runner's configuration, or nil when the run opted out.
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
	return runner.NewSandboxConfig(mode, s.Image), nil
}

type Result struct {
	// Files holds an entry per executed file, in the order they ran.
	Files []*runner.FileResult

	// Passed and Failed count test instances across every file.
	Passed int
	Failed int

	// UpdatedGoldens and PrunedGoldens count snapshot golden files written and removed under Options.Update.
	UpdatedGoldens int
	PrunedGoldens  int

	Wall time.Duration
}

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

	sshManager, err := opts.SSH.config(opts.CoverDir)
	if err != nil {
		return nil, err
	}
	if sshManager != nil {
		defer sshManager.Close()
	}

	r := runner.NewRunner(out, opts.Verbose, opts.KeepTemp, opts.CoverDir)
	r.Update = opts.Update
	r.Sandbox = sandbox
	r.SSH = sshManager
	r.Env = opts.Env

	start := time.Now()

	jobs := opts.Jobs
	if jobs == 0 {
		jobs = runtime.NumCPU()
	}
	if jobs < 1 {
		// Only the UNSET value means "choose for me".
		return nil, fmt.Errorf("Jobs must be at least 1 (0 means one per CPU), got %d", opts.Jobs)
	}
	results, err := r.RunFiles(ctx, files, jobs)
	if err != nil {
		// Already carries the "running <path>:" context.
		return nil, err
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

	// Under Update, summarize the golden churn (writes and prunes were already listed per file).
	if opts.Update && res.UpdatedGoldens+res.PrunedGoldens > 0 {
		fmt.Fprintf(out, "\nUpdated %d golden file(s)", res.UpdatedGoldens)
		if res.PrunedGoldens > 0 {
			fmt.Fprintf(out, ", pruned %d stale", res.PrunedGoldens)
		}
		fmt.Fprintln(out)
	}

	return res, nil
}
