package runner

// Multi-file orchestration: RunFiles runs every file with up to N workload
// commands running concurrently across ALL files. There is only ONE
// execution path -- RunFile (single file) and RunFiles (many) both drive
// runFile against a pool; N=1 simply means one command at a time.

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/wow-look-at-my/dats/schema"
)

// slots is the global workload pool: one token per concurrently-running
// command. Test-instance commands and file-level setup/teardown hook
// commands all hold a token while they run, so no more than N spawned
// processes exist at once, no matter how many files are in flight.
type slots chan struct{}

func newSlots(n int) slots {
	if n < 1 {
		n = 1
	}
	return make(slots, n)
}

func (s slots) acquire() { s <- struct{}{} }
func (s slots) release() { <-s }

// RunFiles runs every file with up to jobs concurrently-running commands
// globally. Per-file semantics are exactly RunFile's: shared files are
// written and setup commands run (sequentially, in declared order) before
// any of the file's test instances start; a setup failure reports every
// instance as failed without running them; teardown commands run
// (sequentially, in declared order) only after the file's last instance
// finished, and always run. Test instances of one file may run concurrently
// with each other and with other files' instances and hooks. Every spawned
// command runs at low OS priority (unix nice 19, best-effort; no-op on
// windows).
//
// Output is buffered per file and flushed in the given file order once
// everything finished -- instances print in expansion order within each file
// -- so a run's bytes depend only on the outcomes, never on scheduling.
//
// Every file is parsed up front: a parse error in any file aborts before a
// single command runs.
//
// Canceling ctx kills every in-flight command's process group promptly;
// teardown commands still run (context.WithoutCancel in runFile).
func (r *Runner) RunFiles(ctx context.Context, paths []string, jobs int) ([]*FileResult, error) {
	if jobs < 1 {
		return nil, fmt.Errorf("jobs must be at least 1, got %d", jobs)
	}

	parsed := make([]*schema.TestFile, len(paths))
	for i, path := range paths {
		testFile, err := schema.ParseFile(path)
		if err != nil {
			return nil, fmt.Errorf("running %s: %w", path, err)
		}
		parsed[i] = testFile
	}

	pool := newSlots(jobs)
	results := make([]*FileResult, len(paths))
	errs := make([]error, len(paths))
	buffers := make([]bytes.Buffer, len(paths))

	var wg sync.WaitGroup
	for i := range paths {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each file gets its own runner writing to its own buffer so
			// concurrent files never interleave output; everything else
			// (verbosity, temp handling, coverage, golden updating) matches
			// the parent.
			fileRunner := &Runner{
				Verbose:     r.Verbose,
				KeepTemp:    r.KeepTemp,
				CoverDir:    r.CoverDir,
				Update:      r.Update,
				Sandbox:     r.Sandbox,
				SSH:         r.SSH,
				Env:         r.Env,
				Formatter:   &Formatter{Writer: &buffers[i], Verbose: r.Verbose},
				lowPriority: true,
			}
			results[i], errs[i] = fileRunner.runFile(ctx, paths[i], parsed[i], pool)
		}(i)
	}
	wg.Wait()

	// Flush the buffered output in canonical order: file order as given,
	// instances already printed in expansion order within each file. On an
	// infrastructure error (e.g. temp dir creation failed) the files before
	// it still print, then the error surfaces.
	for i := range paths {
		if _, err := buffers[i].WriteTo(r.Formatter.Writer); err != nil {
			return nil, err
		}
		if errs[i] != nil {
			return nil, fmt.Errorf("running %s: %w", paths[i], errs[i])
		}
	}

	return results, nil
}
