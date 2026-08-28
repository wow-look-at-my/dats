package runner

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/wow-look-at-my/dats/schema"
)

// slots is the global workload pool: one token per concurrently-running command.
type slots chan struct{}

func newSlots(n int) slots {
	if n < 1 {
		n = 1
	}
	return make(slots, n)
}

func (s slots) acquire() { s <- struct{}{} }
func (s slots) release() { <-s }

// RunFiles runs every file with up to jobs concurrently-running commands globally.
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
