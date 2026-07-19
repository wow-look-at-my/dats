package cmd

// The -j/--jobs flag: parallel test execution. Registration, the make-style
// -jN argv normalization, and resolution of the parsed flag into a worker
// count live here; the orchestration itself is runner.RunFilesParallel.

import (
	"fmt"
	"regexp"
	"runtime"
	"strconv"

	"github.com/spf13/pflag"
)

// registerJobsFlag registers -j/--jobs on flags. The flag defaults to 0
// (absent = fully serial execution, the historical behavior); a bare -j or
// --jobs (no value) means one worker per CPU via pflag's NoOptDefVal.
func registerJobsFlag(flags *pflag.FlagSet) {
	flags.IntP("jobs", "j", 0,
		"run test commands in parallel with N workers (bare -j = one per CPU); use -jN or --jobs=N, not '-j N'")
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

// resolveJobs returns the worker count from the parsed --jobs flag: 0 when
// the flag is absent (run serially, exactly the historical code path), or N
// when set (bare -j resolves to the per-CPU NoOptDefVal). An explicitly set
// value below 1 is an error; the absent-flag default 0 is distinguished from
// an explicit -j0 via Changed.
func resolveJobs(flags *pflag.FlagSet) (int, error) {
	jobs, err := flags.GetInt("jobs")
	if err != nil {
		return 0, err
	}
	if flags.Changed("jobs") && jobs < 1 {
		return 0, fmt.Errorf("--jobs must be at least 1, got %d", jobs)
	}
	return jobs, nil
}
