package schema

// The file-level `ssh` key: a REQUEST for the machine this file's commands
// run on. A file that could dial out on its own would spend the reader's
// credentials before they knew it had asked, so a typed target outranks this
// one and an unapproved (file, target) pair is refused. For `sandbox` a file
// may only NARROW; for `ssh` it may only PROPOSE -- both say that a file
// never changes what runs on the operator's own machine.

import (
	"fmt"
	"regexp"
	"strings"
)

// SSHSpec is an `ssh` block: the target this file's commands run on. Nil
// means the key is absent, so the commands run wherever the run says.
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

// errSSHLocal is the parse error for the dangerous direction: the operator's
// machine is the MORE privileged place, so a file cannot pull a command onto
// it. That is the file reaching for what it was never given.
var errSSHLocal = fmt.Errorf("ssh: a file cannot move a command onto the machine running dats -- remove the key")

// errSSHBare is its counterpart: a file stating a target it did not name.
var errSSHBare = fmt.Errorf("ssh: name the target ([user@]host); whether a run goes remote at all is --ssh")

// sshTargetPattern is the character set a target may use.
var sshTargetPattern = regexp.MustCompile(`^[A-Za-z0-9._@:%\[\]-]+$`)

// UnmarshalYAML decodes the ssh block: a bare target string. There is
// deliberately no port, identity or options key -- connection policy is spent
// from the reader's credentials, so it lives in their ~/.ssh/config, and a
// non-default port is a Host alias away.
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

// validateSSHTarget rejects an empty target and one ssh would read as an
// option. The runner checks again before building an argv, the way fixture
// names are re-checked at setup for a caller that bypassed ParseFile.
func validateSSHTarget(target string) error {
	// Named before the character check, so the most plausible wrong guess
	// gets the reason instead of "must look like [user@]host".
	if name, found := findMatrixPlaceholder(target); found {
		return fmt.Errorf("ssh target: {matrix.%s} is not available outside tests", name)
	}
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
