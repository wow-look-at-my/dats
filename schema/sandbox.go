package schema

// File-level sandboxing: the `sandbox` key controls whether a file's commands
// (its tests and its setup/teardown hooks) run inside an OS-level sandbox,
// and how permissive that sandbox is. It is a file-level key rather than a
// per-test one because a file's commands share one temp directory, one shared
// directory, and one setup/teardown lifecycle -- splitting the sandbox
// per-test would make those shared paths mean different things to different
// tests in the same file.
//
// The key exists mainly as the declarative opt-out: sandboxing is on by
// default in the CLI, and a file whose commands genuinely need the host
// (writing outside the temp directory, driving the local docker daemon,
// testing the sandbox machinery itself) says so with `sandbox: false`.

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// SandboxSpec is a file's `sandbox` block. A nil *SandboxSpec (key absent, or
// explicitly null) leaves every decision to the CLI; a non-nil one refines or
// disables it for this file only. The runner owns the defaults -- the pointer
// fields exist so an unset key stays distinguishable from an explicit false.
type SandboxSpec struct {
	// Enabled is the `enabled` key (or the whole block written as a scalar
	// bool). Nil means "not stated": sandbox if the CLI says so.
	Enabled *bool
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
// sandboxed command, and says so with `sandbox: false`. A per-path escape
// hatch is neither, and each one is a hole in the isolation the file's own
// author cannot see the consequences of.

// IsEnabled reports whether the file wants its commands sandboxed. A nil spec
// and an unstated `enabled` key both mean yes -- the CLI decides whether a
// sandbox is used at all, and this only ever narrows that choice.
func (s *SandboxSpec) IsEnabled() bool {
	if s == nil || s.Enabled == nil {
		return true
	}
	return *s.Enabled
}

// NetworkEnabled reports whether sandboxed commands keep network access.
// Unstated means yes: cutting the network is a deliberate, declared choice,
// not something a file inherits by accident.
func (s *SandboxSpec) NetworkEnabled() bool {
	if s == nil || s.Network == nil {
		return true
	}
	return *s.Network
}

// UnmarshalYAML decodes the two accepted sandbox shapes: a scalar boolean
// (`sandbox: false` opts the file out; `sandbox: true` is the explicit
// opt-in, identical to omitting the key) or a mapping of the keys above. The
// mapping node is iterated directly, which bypasses yaml.v3's own
// duplicate-key detection and KnownFields checking, so unknown and duplicate
// keys are rejected here -- mirroring SnapshotCheck and Matrix.
func (s *SandboxSpec) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var enabled bool
		if err := node.Decode(&enabled); err != nil {
			return fmt.Errorf("sandbox: must be true, false, or a mapping (enabled, network, image)")
		}
		*s = SandboxSpec{Enabled: &enabled}
		return nil
	case yaml.MappingNode:
		spec := SandboxSpec{}
		seen := make(map[string]bool, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			keyNode, valNode := node.Content[i], node.Content[i+1]
			key := keyNode.Value
			switch key {
			case "enabled", "network", "image":
			default:
				return fmt.Errorf("sandbox: unknown key %q (allowed: enabled, network, image)", key)
			}
			if seen[key] {
				return fmt.Errorf("sandbox: %s declared more than once", key)
			}
			seen[key] = true

			switch key {
			case "enabled", "network":
				var flag bool
				if valNode.Kind != yaml.ScalarNode || valNode.Decode(&flag) != nil {
					return fmt.Errorf("sandbox: %s must be a boolean", key)
				}
				if key == "enabled" {
					spec.Enabled = &flag
				} else {
					spec.Network = &flag
				}
			case "image":
				if valNode.Kind != yaml.ScalarNode || valNode.Tag != "!!str" || valNode.Value == "" {
					return fmt.Errorf("sandbox: image must be a non-empty string")
				}
				spec.Image = valNode.Value
			}
		}
		// A mapping that states nothing looks like configuration but
		// configures nothing -- it can only be a mistake.
		if len(seen) == 0 {
			return fmt.Errorf("sandbox: mapping must set at least one of enabled, network, image")
		}
		*s = spec
		return nil
	}
	return fmt.Errorf("sandbox: must be true, false, or a mapping (enabled, network, image)")
}
