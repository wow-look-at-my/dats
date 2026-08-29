package cmd

// The --update flag: rewrite snapshot golden files.

import (
	"github.com/spf13/pflag"
)

var updateGoldens bool

// registerUpdateFlag registers --update on flags.
func registerUpdateFlag(flags *pflag.FlagSet) {
	flags.BoolVar(&updateGoldens, "update", false,
		// No backticks in a usage string: pflag reads the first backticked
		// word as the flag's value placeholder, so a bool flag would render
		// as if it took an argument.
		"rewrite snapshot golden files from actual output instead of failing (see: dats docs format)")
}
