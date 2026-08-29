package cmd

// The -j/--jobs flag: registration, -jN normalization, resolution.

import (
	"fmt"
	"regexp"
	"runtime"
	"strconv"

	"github.com/spf13/pflag"
)

// registerJobsFlag registers -j/--jobs on flags. Parallel is the DEFAULT:
// an absent flag means one worker per logical CPU, and -j1 is how a caller
// asks for one command at a time. A bare -j or --jobs (no value) resolves
// to the same per-CPU count via pflag's NoOptDefVal.
func registerJobsFlag(flags *pflag.FlagSet) {
	flags.IntP("jobs", "j", runtime.NumCPU(),
		"run up to N test commands concurrently (default: one per CPU; -j1 for one at a time); use -jN or --jobs=N, not '-j N'")
	flags.Lookup("jobs").NoOptDefVal = strconv.Itoa(runtime.NumCPU())
}

// jobsShorthandRe matches exactly a make-style attached shorthand: -jN.
var jobsShorthandRe = regexp.MustCompile(`^-j([0-9]+)$`)

// normalizeJobsShorthand rewrites -jN to --jobs=N before cobra parses it:
// NoOptDefVal, needed for a bare -j, makes pflag read -j4 as a bare -j plus
// an unknown '4'. Nothing after "--" is touched, and -j 4 is left unbound,
// as in GNU make.
func normalizeJobsShorthand(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i, arg := range args {
		if arg == "--" {
			break
		}
		if m := jobsShorthandRe.FindStringSubmatch(arg); m != nil {
			out[i] = "--jobs=" + m[1]
		}
	}
	return out
}

// resolveJobs returns the worker count: per-CPU when absent or bare, N when
// set. Below 1 -- -j0 included -- is an error.
func resolveJobs(flags *pflag.FlagSet) (int, error) {
	jobs, err := flags.GetInt("jobs")
	if err != nil {
		return 0, err
	}
	if jobs < 1 {
		return 0, fmt.Errorf("--jobs must be at least 1, got %d", jobs)
	}
	return jobs, nil
}
