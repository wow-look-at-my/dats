package cmd

// The --update flag: rewrite snapshot golden files. Registration lives here;
// the golden writing and pruning themselves are the runner's snapshot logic
// (runner/snapshot.go), reached through Runner.Update. runTests prints the
// end-of-run goldens summary line. `dats syntax` accepts the flag but
// ignores it -- nothing runs, so there is nothing to update.

import (
	"github.com/spf13/pflag"
)

var updateGoldens bool

// registerUpdateFlag registers --update on flags. Long-only, boolean, off by
// default: ordinary runs compare against goldens and fail on mismatch.
func registerUpdateFlag(flags *pflag.FlagSet) {
	flags.BoolVar(&updateGoldens, "update", false,
		"rewrite snapshot golden files from actual output instead of failing (see `dats docs format`)")
}
