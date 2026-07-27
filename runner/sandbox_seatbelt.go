package runner

// The seatbelt backend: macOS sandboxing via sandbox-exec and a generated SBPL
// profile. It is the macOS counterpart of the bwrap backend and enforces the
// same contract -- reads unrestricted so commands still find the host's tools,
// writes confined to the file's own directories, network optional -- with the
// kernel's TrustedBSD MAC policy instead of namespaces.
//
// Apple has marked sandbox-exec deprecated for years while continuing to ship
// it and to rely on it (it is how the platform sandboxes its own tooling), and
// no supported replacement exists for wrapping an arbitrary child process. If
// it is ever actually removed, the probe fails and `--sandbox=auto` falls
// through to docker on its own -- the deprecation costs nothing today and
// degrades gracefully.

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
	// Exercise a real profile, not just the binary: sandbox-exec exists on
	// every mac, so its presence proves nothing, while a profile it refuses
	// to compile or apply fails exactly like a locked-down kernel does for
	// bwrap. The probe profile has the same shape as a run's -- deny writes,
	// then allow a specific subpath -- so a rejected dialect is caught here
	// rather than on the first test.
	profile := seatbeltProfile([]string{os.TempDir()}, true)
	if out, err := exec.CommandContext(ctx, path, "-p", profile, "true").CombinedOutput(); err != nil {
		return fmt.Errorf("sandbox-exec: %s", probeFailure(out, err))
	}
	return nil
}

// seatbeltArgv builds the macOS sandbox-exec invocation for cmd. The profile
// is passed inline (-p) rather than through a file: it is generated per file
// and short-lived, and a temp file would be one more thing to create, secure
// and clean up on every command.
func (p *sandboxPlan) seatbeltArgv(cmd string) []string {
	return []string{
		"sandbox-exec", "-p", seatbeltProfile(p.seatbeltWritablePaths(), p.network),
		"bash", "-c", cmd,
	}
}

// seatbeltWritablePaths returns the writable set with symlinks resolved. This
// is load-bearing on macOS and easy to miss: the sandbox matches on the REAL
// path, while the paths dats hands to commands routinely arrive through
// symlinks (/tmp is a link to /private/tmp, and the per-user TMPDIR that
// os.MkdirTemp uses lives under /private/var/folders). An unresolved subpath
// rule silently matches nothing, and every fixture write is denied. A path
// that cannot be resolved is kept as-is: better an over-narrow rule that
// surfaces as a clear denial than a dropped one that silently widens the
// sandbox.
func (p *sandboxPlan) seatbeltWritablePaths() []string {
	paths := p.writablePaths()
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		if real, err := filepath.EvalSymlinks(path); err == nil {
			resolved = append(resolved, real)
			if real != path {
				// Keep the unresolved form too: a command may address the
				// path either way, and rules are cheap.
				resolved = append(resolved, path)
			}
			continue
		}
		resolved = append(resolved, path)
	}
	return resolved
}

// seatbeltProfile generates the SBPL profile enforcing the same contract the
// bwrap backend enforces with namespaces: reads unrestricted (commands still
// find the host's tools and data), writes confined to the file's own
// directories, network optional.
//
// SBPL is LAST-MATCH-WINS, so the order below is the whole design: allow
// everything, then deny all writes, then re-allow the specific subpaths. The
// device nodes come last because a shell that cannot write /dev/null or a tty
// is not a usable shell -- every redirection and every prompt-less command
// touches them.
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

// sbplString quotes a path as an SBPL string literal, escaping what the
// dialect treats specially. A path is attacker-influenced only insofar as a
// .dats file declares it, but an unescaped quote would end the literal early
// and change which paths the profile allows -- exactly the bug worth never
// having.
func sbplString(path string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(path) + `"`
}
