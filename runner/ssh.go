package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// sshQuote wraps s so a POSIX shell reads it back as one literal word. A
// single quote cannot appear inside single quotes, so it is closed, escaped
// and reopened: '\''.
//
// Every element of a remote command goes through this exactly once, in
// sshRemoteScript. Quoting a string twice, or building the remote command by
// concatenation, is the defect this function exists to prevent.
func sshQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sshRemoteScript joins argv into the single string the remote login shell
// parses. ssh always hands its command to that shell, so dats never gets to
// pass an argument vector: the shell is the only interface, and quoting is
// what makes the vector survive it.
//
// The remote shell must be POSIX-compatible. '\'' is not valid in csh or
// tcsh. probeSSH proves the round trip rather than assumes it.
func sshRemoteScript(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = sshQuote(arg)
	}
	return strings.Join(quoted, " ")
}

// sshTargetPattern is the character set a target may use: a user name, a host
// name or address, a port separator, an IPv6 scope, and the brackets around a
// literal IPv6 address.
var sshTargetPattern = regexp.MustCompile(`^[A-Za-z0-9._@:%\[\]-]+$`)

// ValidateSSHTarget checks that target names a host and cannot act as an ssh
// option.
//
// SECURITY: ssh accepts no "--" before the target, so a target that starts
// with "-" is read as an option. "-oProxyCommand=..." then runs a command on
// the machine that started dats. Validation is the only defense, so it runs
// at parse time and again before the argv is built.
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

// sshControlPathMax bounds the control socket path. A unix socket path stops
// at 104 bytes on macOS and 108 on Linux, and ssh reports the overflow as an
// obscure failure. The name is a fixed-width hash, so a long target cannot
// push the path over the limit.
const sshControlPathMax = 100

// sshControlPath names the multiplexing socket for one target inside dir.
// The idiomatic ~/.ssh/cm-%r@%h:%p spelling is deliberately not used: it
// grows with the host name and overflows the socket path limit.
func sshControlPath(dir, target string) string {
	sum := sha256.Sum256([]byte(target))
	return path.Join(dir, hex.EncodeToString(sum[:])[:8])
}

// sshTransportArgs are the options every ssh invocation carries, for a
// command and for the master alike.
//
// Each one is load-bearing:
//
//   - -T asks for no pty. A remote tty rewrites "\n" as "\r\n" and merges
//     stderr into stdout, which breaks stderr assertions and byte-exact
//     goldens. The default depends on whether dats' own stdin is a tty, so
//     the flag must be explicit.
//   - BatchMode=yes stops every prompt: password, passphrase and unknown host
//     key. An unknown host key is then a failure, never a question nobody is
//     present to answer.
//   - LogLevel=ERROR drops the "Permanently added ... to the list of known
//     hosts" warning. That warning appears on the first run against a host,
//     lands in captured stderr, and fails stderr assertions exactly once.
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

// sshRemoteCommand builds the script for one test command.
//
// The script records the login shell PID before it runs anything, because
// ssh does not forward a signal to the remote side. killRemote reads that
// file. The record is written outside the command, so a failure to write it
// can never become the command's exit code: the braces are closed with ";",
// never with "&&".
//
// "exec" then replaces the shell, so the remote exit status is the command's
// own. env carries the run environment, which an ssh session does not
// inherit; SendEnv cannot do this, because sshd accepts only LANG and LC_*
// by default and drops the rest without a word.
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
