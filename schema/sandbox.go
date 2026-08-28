package schema


import (
	"errors"
	"fmt"

	yamlfixed "github.com/wow-look-at-my/yaml-fixed/yaml"
)

// SandboxSpec is a file's `sandbox` block.
type SandboxSpec struct {
	// Network is the `network` key: whether commands keep network access.
	Network *bool
	// Image is the `image` key: the container image the docker backend runs commands in.
	Image string
}

// There is deliberately no way to declare extra writable HOST paths.

// NetworkEnabled reports whether sandboxed commands keep network access.
func (s *SandboxSpec) NetworkEnabled() bool {
	if s == nil || s.Network == nil {
		return true
	}
	return *s.Network
}

// ImageName reports the docker image the file asked for, empty when it asked for none.
func (s *SandboxSpec) ImageName() string {
	if s == nil {
		return ""
	}
	return s.Image
}

// errSandboxOff is the parse error for a file trying to turn its own sandbox off.
var errSandboxOff = errors.New("sandbox: a file cannot turn its own sandbox off -- run dats with --no-sandbox if these commands need the host")

// errSandboxOn is its counterpart: a file stating the default.
var errSandboxOn = errors.New("sandbox: commands are sandboxed unless the run opts out -- drop the key, or write a mapping (network, image)")

// UnmarshalYAML decodes the sandbox block: a mapping of the keys above.
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
		if v.Len() == 0 {
			return fmt.Errorf("sandbox: mapping must set at least one of network, image")
		}
		*s = spec
		return nil
	}
	return fmt.Errorf("sandbox: must be a mapping (network, image)")
}
