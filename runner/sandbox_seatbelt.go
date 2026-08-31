package runner

// The seatbelt backend: macOS sandboxing via sandbox-exec and a generated SBPL profile.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func probeSeatbelt() error {
	path, err := exec.LookPath("sandbox-exec")
	if err != nil {
		return fmt.Errorf("sandbox-exec: not found in $PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	profile := seatbeltProfile([]string{os.TempDir()}, true)
	if out, err := exec.CommandContext(ctx, path, "-p", profile, "true").CombinedOutput(); err != nil {
		return fmt.Errorf("sandbox-exec: %s", probeFailure(out, err))
	}
	return nil
}

// sandboxTmpDirName is the scratch directory seatbelt points TMPDIR at, inside work.
const sandboxTmpDirName = ".dats-tmp"

// seatbeltArgv builds the macOS sandbox-exec invocation for cmd.
func (p *sandboxPlan) seatbeltArgv(cmd string) []string {
	argv := []string{"sandbox-exec", "-p", seatbeltProfile(p.seatbeltWritablePaths(), p.network)}
	if p.tmp != "" {
		// The host's TMPDIR is outside the writable set, so a command that inherits
		// it writes nowhere. Every spelling, because tools disagree on which to read.
		argv = append(argv, "env", "TMPDIR="+p.tmp, "TMP="+p.tmp, "TEMP="+p.tmp)
	}
	return append(argv, "bash", "-c", cmd)
}

// seatbeltWritablePaths returns the writable set with symlinks resolved.
func (p *sandboxPlan) seatbeltWritablePaths() []string {
	paths := p.writablePaths()
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		if real, err := filepath.EvalSymlinks(path); err == nil {
			resolved = append(resolved, real)
			if real != path {
				// Keep the unresolved form too: a command may address the path either way, and rules are cheap.
				resolved = append(resolved, path)
			}
			continue
		}
		resolved = append(resolved, path)
	}
	return resolved
}

func seatbeltProfile(writable []string, network bool) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n(deny file-write*)\n")
	if !network {
		b.WriteString("(deny network*)\n")
	}
	b.WriteString("(allow file-write*\n")
	for _, path := range writable {
		fmt.Fprintf(&b, "    (subpath %s)\n", sbplString(path))
	}
	// Writable device nodes: the shell's own plumbing, not host state.
	b.WriteString(`    (literal "/dev/null")
    (literal "/dev/zero")
    (literal "/dev/random")
    (literal "/dev/urandom")
    (literal "/dev/dtracehelper")
    (subpath "/dev/fd")
    (regex #"^/dev/tty")
)
`)
	return b.String()
}

// sbplString quotes a path as an SBPL string literal, escaping what the dialect treats specially.
func sbplString(path string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(path) + `"`
}
