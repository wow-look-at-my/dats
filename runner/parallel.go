package runner

// Jobs-mode orchestration (-j/--jobs): RunFilesParallel runs multiple files
// with up to N workload commands running concurrently across ALL files,
// preserving every per-file semantic of the serial path (RunFile) and
// printing byte-identical output in canonical order. runFileParallel below
// deliberately mirrors RunFile's structure and Formatter call sequence step
// for step -- keep the two in sync (the cmd-level determinism test pins the
// equal-outcome byte equality).

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/wow-look-at-my/dats/schema"
)

// slots is the global workload pool: one token per concurrently-running
// command. Test-instance commands and file-level setup/teardown hook
// commands all hold a token while they run, so no more than N spawned
// processes exist at once, no matter how many files are in flight.
type slots chan struct{}

func (s slots) acquire() { s <- struct{}{} }
func (s slots) release() { <-s }

// RunFilesParallel runs every file with up to jobs concurrently-running
// commands globally. Per-file semantics are identical to RunFile: shared
// files are written and setup commands run (sequentially, in declared order)
// before any of the file's test instances start; a setup failure reports
// every instance as failed without running them; teardown commands run
// (sequentially, in declared order) only after the file's last instance
// finished, and always run. Test instances of one file may run concurrently
// with each other and with other files' instances and hooks. Every spawned
// command runs at low OS priority (unix nice 19, best-effort; no-op on
// windows); the dats process itself stays at normal priority.
//
// Output is buffered per file and flushed in the given file order once
// everything finished -- instances print in expansion order within each file
// -- so a run's bytes match a serial run of the same corpus whenever the
// outcomes are equal.
//
// Unlike the serial loop, which parses each file as it reaches it, every
// file is parsed up front: a parse error in any file aborts before a single
// command runs.
func (r *Runner) RunFilesParallel(paths []string, jobs int) ([]*FileResult, error) {
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

	pool := make(slots, jobs)
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
			// (verbosity, temp handling, coverage) matches the parent.
			fileRunner := &Runner{
				Verbose:     r.Verbose,
				KeepTemp:    r.KeepTemp,
				CoverDir:    r.CoverDir,
				Formatter:   &Formatter{Writer: &buffers[i], Verbose: r.Verbose},
				lowPriority: true,
			}
			results[i], errs[i] = fileRunner.runFileParallel(paths[i], parsed[i], pool)
		}(i)
	}
	wg.Wait()

	// Flush the buffered output in canonical order: file order as given,
	// instances already printed in expansion order within each file. On an
	// infrastructure error (e.g. temp dir creation failed) the files before
	// it still print, then the error surfaces -- the same reporting shape as
	// the serial loop aborting at that file.
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

// runFileParallel is RunFile's jobs-mode counterpart, operating on an
// already-parsed file: identical per-file semantics and an identical
// Formatter call sequence (equal outcomes produce equal bytes), except that
// the test instances run concurrently instead of one after another. Results
// land at their canonical index -- instance numbering and test-<index> temp
// directories keep the expansion order regardless of completion order -- and
// print in that order once every instance finished. Each command, hook or
// instance, holds a pool slot while it runs.
func (r *Runner) runFileParallel(path string, testFile *schema.TestFile, pool slots) (*FileResult, error) {
	tempDir, err := os.MkdirTemp("", "dats-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp directory: %w", err)
	}
	if !r.KeepTemp {
		defer Cleanup(tempDir)
	} else {
		fmt.Fprintf(r.Formatter.Writer, "# Temp directory: %s\n", tempDir)
	}

	if r.CoverDir != "" {
		if err := os.MkdirAll(r.CoverDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating coverage directory: %w", err)
		}
	}

	instances := make([]schema.TestInstance, 0, len(testFile.Tests))
	for i := range testFile.Tests {
		instances = append(instances, schema.ExpandMatrix(&testFile.Tests[i])...)
	}

	r.Formatter.PrintHeader(path, len(instances))

	result := &FileResult{
		Path:    path,
		Results: make([]TestResult, 0, len(instances)),
	}

	sharedDir := filepath.Join(tempDir, sharedDirName)
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		return nil, fmt.Errorf("creating shared dir: %w", err)
	}

	// Shared fixtures are written and setup commands run strictly before any
	// instance starts. Hooks stay sequential within the file (their declared
	// order is semantic); each holds a pool slot while its command runs.
	if testFile.Shared != nil {
		if err := SetupSharedFixtures(sharedDir, testFile.Shared.Files); err != nil {
			result.SetupFailure = &CommandFailure{Detail: fmt.Sprintf("shared fixtures: %v", err)}
			r.Formatter.PrintHookFailure("setup", result.SetupFailure)
		}
	}
	if result.SetupFailure == nil {
		for _, raw := range testFile.Setup {
			pool.acquire()
			fail := r.runHookCommand("setup", raw, sharedDir)
			pool.release()
			if fail != nil {
				result.SetupFailure = fail
				r.Formatter.PrintHookFailure("setup", fail)
				break // remaining setup commands are skipped; every test fails below
			}
		}
	}

	if result.SetupFailure != nil {
		// Identical to serial: every instance is reported as a normal
		// failure -- loudly, never as "skipped" -- without running any of
		// their commands.
		fmt.Fprintln(r.Formatter.Writer)
		for i := range instances {
			testResult := TestResult{
				Name:     instanceName(&instances[i]),
				Index:    i,
				Failures: []string{"file setup failed"},
			}
			result.Results = append(result.Results, testResult)
			result.Failed++
			r.Formatter.PrintResult(&testResult)
		}
	} else {
		if r.Verbose && len(testFile.Setup) > 0 {
			fmt.Fprintln(r.Formatter.Writer)
		}
		// Launch every instance; the shared global pool bounds how many run
		// at once across all files. Each writes only its own slice element,
		// its own test-<index> directory, and (by convention, read-only) the
		// file's shared directory.
		instanceResults := make([]TestResult, len(instances))
		var wg sync.WaitGroup
		for i := range instances {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				pool.acquire()
				defer pool.release()
				testResult := r.RunTest(&instances[i].Test, tempDir, i)
				testResult.Name = instanceName(&instances[i])
				instanceResults[i] = testResult
			}(i)
		}
		wg.Wait()

		for i := range instanceResults {
			result.Results = append(result.Results, instanceResults[i])
			r.Formatter.PrintResult(&instanceResults[i])

			if instanceResults[i].Passed {
				result.Passed++
			} else {
				result.Failed++
			}
		}
	}

	// Teardown always runs -- in declared order, sequentially, after every
	// instance has finished, and even when setup failed. One failing command
	// does not stop the rest; any failure marks the file failed.
	if r.Verbose && len(testFile.Teardown) > 0 {
		fmt.Fprintln(r.Formatter.Writer)
	}
	for _, raw := range testFile.Teardown {
		pool.acquire()
		fail := r.runHookCommand("teardown", raw, sharedDir)
		pool.release()
		if fail != nil {
			result.TeardownFailures = append(result.TeardownFailures, *fail)
			r.Formatter.PrintHookFailure("teardown", fail)
		}
	}

	r.Formatter.PrintSummary(result)

	return result, nil
}
