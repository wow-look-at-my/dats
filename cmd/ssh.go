package cmd

// The --ssh flag: run this run's commands on another machine.

import (
	"github.com/spf13/pflag"
	"github.com/wow-look-at-my/dats/runner"
)

// registerSSHFlag registers --ssh on flags.
func registerSSHFlag(flags *pflag.FlagSet) {
	flags.String("ssh", "",
		"run test commands on [user@]host over ssh instead of this machine")
}

// resolveSSH turns the parsed flag into the run's target, or "" when the run stays local.
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
