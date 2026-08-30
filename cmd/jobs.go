package cmd

// The -j/--jobs flag: how many test commands run concurrently.

import (
	"fmt"
	"regexp"
	"runtime"
	"strconv"

	"github.com/spf13/pflag"
)

// registerJobsFlag registers -j/--jobs on flags.
func registerJobsFlag(flags *pflag.FlagSet) {
	flags.IntP("jobs", "j", runtime.NumCPU(),
		"run up to N test commands concurrently (default: one per CPU; -j1 for one at a time); use -jN or --jobs=N, not '-j N'")
	flags.Lookup("jobs").NoOptDefVal = strconv.Itoa(runtime.NumCPU())
}

// jobsShorthandRe matches exactly a make-style attached shorthand: -jN.
var jobsShorthandRe = regexp.MustCompile(`^-j([0-9]+)$`)

// normalizeJobsShorthand rewrites make-style -jN tokens to --jobs=N before cobra parses them.
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
