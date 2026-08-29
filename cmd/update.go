package cmd

// The --update flag: rewrite snapshot golden files.

import (
	"github.com/spf13/pflag"
)

var updateGoldens bool

// registerUpdateFlag registers --update on flags.
func registerUpdateFlag(flags *pflag.FlagSet) {
	flags.BoolVar(&updateGoldens, "update", false,
		"rewrite snapshot golden files from actual output instead of failing (see docs/file-format.md)")
}
