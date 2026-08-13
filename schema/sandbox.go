package schema

// File-level sandboxing: the `sandbox` key narrows the OS-level sandbox that
// a file's commands -- its tests and its setup/teardown hooks -- run inside.
// It is a file-level key rather than a per-test one because a file's commands
// share one temp directory, one shared directory, and one setup/teardown
// lifecycle -- splitting the sandbox per-test would make those shared paths
// mean different things to different tests in the same file.
//
// The key can only TIGHTEN the sandbox. Whether commands are sandboxed at all
// belongs to whoever runs dats (`--no-sandbox`, or Options.Sandbox for a
// library caller), never to the file: a .dats file is code that arrives from
// somewhere, and a file able to switch its own sandbox off would drop the
// isolation exactly where it is needed, without the operator seeing it go.

import (
	"errors"
	"fmt"

	yamlfixed "github.com/wow-look-at-my/yaml-fixed/yaml"
)

// SandboxSpec is a file's `sandbox` block. A nil *SandboxSpec (key absent, or
// explicitly null) accepts the sandbox as the operator configured it; a
// non-nil one narrows it for this file only. The runner owns the defaults --
// the pointer field exists so an unset key stays distinguishable from an
// explicit false.
type SandboxSpec struct {
	// Network is the `network` key: whether commands keep network access.
	// Nil means "not stated", which the runner reads as network allowed.
	Network *bool
	// Image is the `image` key: the container image the docker backend runs
	// commands in. Empty means "not stated" (the CLI's image is used); it has
	// no effect under the bwrap backend.
	Image string
}

// There is deliberately no way to declare extra writable HOST paths. A command
// that needs somewhere to write has the file's temp directory (a real
// filesystem inside every backend, and the one place fixtures, {outputs.X} and
// snapshots already live); a command that genuinely needs the host is not a
// sandboxed command, and the run that wants it is spelled `--no-sandbox`. A
// per-path escape hatch is neither, and each one is a hole in the isolation
// the file's own author cannot see the consequences of.

// NetworkEnabled reports whether sandboxed commands keep network access.
// Unstated means yes: cutting the network is a deliberate, declared choice,
// not something a file inherits by accident.
func (s *SandboxSpec) NetworkEnabled() bool {
	if s == nil || s.Network == nil {
		return true
	}
	return *s.Network
}

// errSandboxOff is the parse error for a file trying to turn its own sandbox
// off. It names the flag that actually does it, because the file's author is
// not always the person who will run the file -- and only that person gets to
// decide their commands run on the host.
var errSandboxOff = errors.New("sandbox: a file cannot turn its own sandbox off -- run dats with --no-sandbox if these commands need the host")

// errSandboxOn is its counterpart: a file stating the default. Silently
// accepting it would read as the file having secured something it did not.
var errSandboxOn = errors.New("sandbox: commands are sandboxed unless the run opts out -- drop the key, or write a mapping (network, image)")

// UnmarshalYAML decodes the sandbox block: a mapping of the keys above. A
// parsed *yamlfixed.Map can never hold a duplicate key (the parser rejects
// that before this ever runs), so no manual duplicate-key bookkeeping is
// needed; unknown keys are still rejected here, mirroring SnapshotCheck and
// Matrix.
func (s *SandboxSpec) UnmarshalYAML(value any) error {
	switch v := value.(type) {
	case bool:
		if !v {
			return errSandboxOff
		}
		return errSandboxOn
	case *yamlfixed.Map:
		spec := SandboxSpec{}
		for _, key := range v.Keys {
			val, _ := v.Get(key)
			switch key {
			case "network":
				flag, ok := val.(bool)
				if !ok {
					return fmt.Errorf("sandbox: %s must be a boolean", key)
				}
				spec.Network = &flag
			case "image":
				image, ok := val.(string)
				if !ok || image == "" {
					return fmt.Errorf("sandbox: image must be a non-empty string")
				}
				spec.Image = image
			case "enabled":
				if on, ok := val.(bool); ok && on {
					return errSandboxOn
				}
				return errSandboxOff
			default:
				return fmt.Errorf("sandbox: unknown key %q (allowed: network, image)", key)
			}
		}
		// A mapping that states nothing looks like configuration but
		// configures nothing -- it can only be a mistake.
		if v.Len() == 0 {
			return fmt.Errorf("sandbox: mapping must set at least one of network, image")
		}
		*s = spec
		return nil
	}
	return fmt.Errorf("sandbox: must be a mapping (network, image)")
}
