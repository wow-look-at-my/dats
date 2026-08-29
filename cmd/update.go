package cmd

// The --update flag: registration only. The writing and pruning are
// runner/snapshot.go, reached through Runner.Update.

import (
	"github.com/spf13/pflag"
)

var updateGoldens bool

// registerUpdateFlag registers --update on flags. Long-only, boolean, off by
// default: ordinary runs compare against goldens and fail on mismatch.
func registerUpdateFlag(flags *pflag.FlagSet) {
	flags.BoolVar(&updateGoldens, "update", false,
		// No backticks: pflag reads one as the flag's value placeholder.
		"rewrite snapshot golden files from actual output instead of failing (see: dats docs format)")
}
