package cmd

// The --update flag: rewrite snapshot golden files.

import (
	"github.com/spf13/pflag"
)

var updateGoldens bool

// registerUpdateFlag registers --update. No backtick in the usage string:
// pflag reads it as the flag's value placeholder.
func registerUpdateFlag(flags *pflag.FlagSet) {
	flags.BoolVar(&updateGoldens, "update", false,
		"rewrite snapshot golden files from actual output instead of failing (see: dats docs format)")
}
