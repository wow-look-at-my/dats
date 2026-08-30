package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// sshQuote makes s a single literal word for a POSIX shell.
func sshQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func sshRemoteScript(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = sshQuote(arg)
	}
	return strings.Join(quoted, " ")
}

// sshTargetPattern covers a user, host or address, port, IPv6 scope, brackets.
var sshTargetPattern = regexp.MustCompile(`^[A-Za-z0-9._@:%\[\]-]+$`)

// ValidateSSHTarget rejects a target that ssh would read as an option.
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

// remoteJoin builds a POSIX path on the target.
func remoteJoin(base string, elem ...string) string {
	return path.Join(append([]string{base}, elem...)...)
}

// sshControlPathMax bounds the socket path, because macOS caps its length.
const sshControlPathMax = 100

// sshControlPath names a single target's socket inside dir.
func sshControlPath(dir, target string) string {
	sum := sha256.Sum256([]byte(target))
	return path.Join(dir, hex.EncodeToString(sum[:])[:8])
}

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

func sshArgv(target, controlPath, script string) []string {
	argv := append([]string{"ssh"}, sshTransportArgs(controlPath)...)
	return append(argv, target, script)
}

// sshRemoteCommand builds the script for a single command.
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
