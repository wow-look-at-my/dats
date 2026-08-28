package cmd

// The --ssh flag: run this run's commands on another machine.
//
// Long-only, like the sandbox flags, and for the same reason: a one-letter
// shorthand for something this consequential invites the typo nobody
// notices. Registration and resolution live here; the transport itself is
// runner.SSHConfig.

import (
	"github.com/spf13/pflag"
	"github.com/wow-look-at-my/dats/runner"
)

// registerSSHFlag registers --ssh on flags.
func registerSSHFlag(flags *pflag.FlagSet) {
	flags.String("ssh", "",
		"run test commands on [user@]host over ssh instead of this machine")
}

// resolveSSH turns the parsed flag into the run's target, or "" when the run
// stays local. The target is validated here so a value that ssh would read
// as an option is refused before anything runs.
func resolveSSH(flags *pflag.FlagSet) (string, error) {
	target, err := flags.GetString("ssh")
	if err != nil {
		return "", err
	}
	if target == "" {
		return "", nil
	}
	if err := runner.ValidateSSHTarget(target); err != nil {
		return "", err
	}
	return target, nil
}
