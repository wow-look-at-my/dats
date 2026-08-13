package cmd

// The sandbox flags: --sandbox, --no-sandbox, and --sandbox-image. Test
// commands are sandboxed by DEFAULT (--sandbox=auto: bubblewrap, falling back
// to docker), so running them straight on the host is an explicit opt-out,
// and it is made HERE: a file can narrow its own sandbox but never turn one
// off, so this is the only place the isolation comes down.
//
// Registration and resolution live here; the backends themselves are
// runner.SandboxConfig.

import (
	"fmt"

	"github.com/spf13/pflag"
	"github.com/wow-look-at-my/dats/runner"
)

// registerSandboxFlags registers the sandbox flags on flags. They are
// long-only: -s is not taken, but a one-letter shorthand for something this
// consequential invites exactly the typo nobody notices.
func registerSandboxFlags(flags *pflag.FlagSet) {
	flags.String("sandbox", string(runner.SandboxAuto),
		"sandbox test commands: auto (bwrap, then seatbelt, then docker), bwrap, seatbelt, docker, or none")
	flags.Bool("no-sandbox", false,
		"run test commands directly on the host (same as --sandbox=none)")
	flags.String("sandbox-image", runner.DefaultSandboxImage,
		"container image the docker sandbox backend runs commands in")
}

// resolveSandbox turns the parsed flags into the run's sandbox configuration,
// or nil when the run is opting out. Nothing is probed here: the backend is
// resolved lazily, on the first file that actually needs one, so --no-sandbox
// and `dats syntax` run fine on a machine with no backend installed.
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
		// --no-sandbox is shorthand for --sandbox=none, so pairing it with a
		// different backend is a contradiction, not a precedence puzzle to
		// resolve silently.
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
	image, err := flags.GetString("sandbox-image")
	if err != nil {
		return nil, err
	}
	return runner.NewSandboxConfig(mode, image), nil
}
