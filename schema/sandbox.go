package schema

// The file-level `sandbox` key. It only TIGHTENS: whether commands are
// sandboxed at all belongs to whoever runs dats, never to the file.

import (
	"errors"
	"fmt"

	yamlfixed "github.com/wow-look-at-my/yaml-fixed/yaml"
)

// SandboxSpec is a file's `sandbox` block; nil narrows nothing.
type SandboxSpec struct {
	// Network nil is "not stated", which the runner reads as network allowed.
	Network *bool
	// Image is the docker backend's image; empty is "not stated".
	Image string
}

// There is deliberately no key for extra writable HOST paths: scratch goes in
// the temp dir, and a command that needs the host needs `--no-sandbox`.

// NetworkEnabled: unstated means yes, since cutting it is a declared choice.
func (s *SandboxSpec) NetworkEnabled() bool {
	if s == nil || s.Network == nil {
		return true
	}
	return *s.Network
}

// ImageName is a request, not a decision: --sandbox-image outranks it.
func (s *SandboxSpec) ImageName() string {
	if s == nil {
		return ""
	}
	return s.Image
}

// errSandboxOff names the flag instead: that choice is the runner's.
var errSandboxOff = errors.New("sandbox: a file cannot turn its own sandbox off -- run dats with --no-sandbox if these commands need the host")

// errSandboxOn: accepting it would read as the file securing something.
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
