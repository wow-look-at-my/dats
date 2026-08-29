package schema

// The file-level `ssh` key: a REQUEST, not a decision.

import (
	"fmt"
	"regexp"
	"strings"
)

// SSHSpec is an `ssh` block: the target this file's commands run on.
type SSHSpec struct {
	// Target is [user@]host, as ssh spells it.
	Target string
}

// TargetName is the nil-safe accessor; "" when the key is absent.
func (s *SSHSpec) TargetName() string {
	if s == nil {
		return ""
	}
	return s.Target
}

// errSSHLocal guards the dangerous direction: local is the privileged side.
var errSSHLocal = fmt.Errorf("ssh: a file cannot move a command onto the machine running dats -- remove the key")

// errSSHBare is its counterpart: a file stating a target it did not name.
var errSSHBare = fmt.Errorf("ssh: name the target ([user@]host); whether a run goes remote at all is --ssh")

// sshTargetPattern allows braces so a PER-TEST target can hold {matrix.X}.
var sshTargetPattern = regexp.MustCompile(`^[A-Za-z0-9._@:%\[\]{}-]+$`)

// UnmarshalYAML decodes the ssh block: a bare target string.
func (s *SSHSpec) UnmarshalYAML(value any) error {
	switch v := value.(type) {
	case bool:
		if !v {
			return errSSHLocal
		}
		return errSSHBare
	case string:
		if err := validateSSHTarget(v); err != nil {
			return err
		}
		*s = SSHSpec{Target: v}
		return nil
	}
	return fmt.Errorf("ssh: must be a target string like user@host")
}

// validateSSHTarget rejects an empty target and one ssh would read as an option.
func validateSSHTarget(target string) error {
	switch {
	case target == "":
		return fmt.Errorf("ssh: target must be a non-empty string")
	case strings.HasPrefix(target, "-"):
		return fmt.Errorf("ssh: target %q must not start with a dash: ssh reads it as an option, and an option can run a command on this machine", target)
	case !sshTargetPattern.MatchString(target):
		return fmt.Errorf("ssh: target %q must look like [user@]host", target)
	}
	return nil
}
