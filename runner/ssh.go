package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// sshQuote makes s one literal word for a POSIX shell. Quote it once, here:
// a second pass, or any concatenation, re-opens the injection this closes.
func sshQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sshRemoteScript joins argv into the one string the remote login shell
// parses. ssh passes no argv to the far side, so quoting is the whole
// interface. The remote shell must be POSIX; csh cannot parse the escape.
func sshRemoteScript(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = sshQuote(arg)
	}
	return strings.Join(quoted, " ")
}

// sshTargetPattern covers a user, a host or address, a port separator, an
// IPv6 scope, and the brackets around a literal IPv6 address.
var sshTargetPattern = regexp.MustCompile(`^[A-Za-z0-9._@:%\[\]-]+$`)

// ValidateSSHTarget rejects a target that ssh would read as an option.
// SECURITY: ssh takes no "--" before its target, so "-oProxyCommand=..."
// runs a command on the machine that started dats. This check is the only
// defense, so it runs at parse time and again before the argv is built.
func ValidateSSHTarget(target string) error {
	if target == "" {
		return fmt.Errorf("ssh: target must be a non-empty string")
	}
	if strings.HasPrefix(target, "-") {
		return fmt.Errorf("ssh: target %q must not start with a dash: ssh reads it as an option, and an option can run a command on this machine", target)
	}
	if !sshTargetPattern.MatchString(target) {
		return fmt.Errorf("ssh: target %q must look like [user@]host", target)
	}
	return nil
}

// sshControlPathMax bounds the control socket path: a unix socket path stops
// at 104 bytes on macOS, and ssh reports the overflow obscurely.
const sshControlPathMax = 100

// sshControlPath names one target's multiplexing socket inside dir. The
// idiomatic ~/.ssh/cm-%r@%h:%p spelling grows with the host name and
// overflows the limit above, so the name is a fixed-width hash.
func sshControlPath(dir, target string) string {
	sum := sha256.Sum256([]byte(target))
	return path.Join(dir, hex.EncodeToString(sum[:])[:8])
}

// sshTransportArgs are the options every ssh invocation carries. Each is
// load-bearing: -T refuses a pty, which would merge stderr into stdout and
// rewrite newlines (the default turns on dats' own stdin, so state it);
// BatchMode makes an unknown host key a failure, not a prompt nobody can
// answer; LogLevel drops the known-hosts warning, which otherwise lands in
// captured stderr on the first run against a host and fails an assertion once.
func sshTransportArgs(controlPath string) []string {
	args := []string{
		"-T",
		"-o", "BatchMode=yes",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=10",
	}
	if controlPath != "" {
		args = append(args, "-o", "ControlMaster=no", "-o", "ControlPath="+controlPath)
	}
	return args
}

// sshArgv is the argv that runs script on target. script is one element: the
// remote login shell receives it whole and parses it.
func sshArgv(target, controlPath, script string) []string {
	argv := append([]string{"ssh"}, sshTransportArgs(controlPath)...)
	return append(argv, target, script)
}

// sshRemoteCommand builds the script for one command. It records the login
// shell PID first, because ssh forwards no signal and killRemote reads that
// file. Close the record with ";", never "&&": a failed write must not become
// the command's exit code. "exec" then hands the status back unchanged. env
// carries the run environment, which an ssh session does not inherit and
// SendEnv cannot deliver (sshd accepts only LANG and LC_* by default).
func sshRemoteCommand(pidDir, id string, env, argv []string) string {
	var b strings.Builder
	if pidDir != "" && id != "" {
		b.WriteString("{ mkdir -p ")
		b.WriteString(sshQuote(pidDir))
		b.WriteString(" && echo $$ > ")
		b.WriteString(sshQuote(path.Join(pidDir, id)))
		b.WriteString("; } 2>/dev/null; ")
	}
	b.WriteString("exec ")
	if len(env) > 0 {
		b.WriteString("env -- ")
		for _, entry := range env {
			b.WriteString(sshQuote(entry))
			b.WriteByte(' ')
		}
	}
	b.WriteString(sshRemoteScript(argv))
	return b.String()
}
