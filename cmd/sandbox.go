package cmd

// The sandbox flags: --sandbox, --no-sandbox, and --sandbox-image.

import (
	"fmt"

	"github.com/spf13/pflag"
	"github.com/wow-look-at-my/dats/runner"
)

// registerSandboxFlags registers the sandbox flags on flags.
func registerSandboxFlags(flags *pflag.FlagSet) {
	flags.String("sandbox", string(runner.SandboxAuto),
		"sandbox test commands: auto (bwrap, then seatbelt, then docker), bwrap, seatbelt, docker, or none")
	flags.Bool("no-sandbox", false,
		"run test commands directly on the host (same as --sandbox=none)")
	flags.String("sandbox-image", runner.DefaultSandboxImage,
		"container image the docker sandbox backend runs commands in (typing it outranks a file's image:)")
}

func resolveSandbox(flags *pflag.FlagSet) (*runner.SandboxConfig, error) {
	name, err := flags.GetString("sandbox")
	if err != nil {
		return nil, err
	}
	off, err := flags.GetBool("no-sandbox")
	if err != nil {
		return nil, err
	}
	if off {
		if flags.Changed("sandbox") && runner.SandboxMode(name) != runner.SandboxNone {
			return nil, fmt.Errorf("--no-sandbox conflicts with --sandbox=%s", name)
		}
		name = string(runner.SandboxNone)
	}
	mode, err := runner.ParseSandboxMode(name)
	if err != nil {
		// Naming the flag points the message at what the user typed.
		return nil, fmt.Errorf("--sandbox: %w", err)
	}
	if mode == runner.SandboxNone {
		return nil, nil
	}
	// Only a TYPED --sandbox-image is carried through.
	var image string
	if flags.Changed("sandbox-image") {
		if image, err = flags.GetString("sandbox-image"); err != nil {
			return nil, err
		}
	}
	return runner.NewSandboxConfig(mode, image), nil
}
