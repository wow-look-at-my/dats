package cmd

// The -j/--jobs flag: how many test commands run at once. Registration, the
// make-style -jN argv normalization, and resolution of the parsed flag into
// a worker count live here; the orchestration itself is runner.RunFiles.

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

// normalizeJobsShorthand rewrites make-style -jN tokens to --jobs=N before
// cobra parses them. With NoOptDefVal set (needed so a bare -j works), pflag
// resolves the shorthand to the no-option default BEFORE considering the
// attached "-farg" form, so a raw -j4 would parse as bare -j followed by an
// unknown '4' shorthand and fail. Only exact -jN tokens are rewritten, and
// nothing after a "--" terminator is touched. The space-separated forms
// (-j 4, --jobs 4) are intentionally NOT rescued: as with GNU make, the
// value does not bind and "4" becomes a positional argument.
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

// resolveJobs returns the worker count from the parsed --jobs flag: the
// per-CPU default when absent, or N when set (bare -j resolves to the same
// per-CPU NoOptDefVal). Any value below 1 is an error -- including an
// explicit -j0, which now has no meaning of its own.
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
