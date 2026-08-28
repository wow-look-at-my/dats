package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// sshProbePayload is shell syntax on purpose.
const sshProbePayload = "dats probe: '\"$`\\ * {} ${x} <>|& ;"

// sshConnectTimeout bounds the wait for the multiplexing master.
const sshConnectTimeout = 30 * time.Second

// SSHConfig is the run-wide ssh target.
type SSHConfig struct {
	// Target is [user@]host, exactly as ssh spells it.
	Target string

	once        sync.Once
	err         error
	controlDir  string
	controlPath string
	master      *exec.Cmd
}

// NewSSHConfig builds a config for target. Nothing connects until Connect.
func NewSSHConfig(target string) *SSHConfig {
	return &SSHConfig{Target: target}
}

func (c *SSHConfig) Connect(ctx context.Context) error {
	c.once.Do(func() { c.err = c.connect(ctx) })
	return c.err
}

func (c *SSHConfig) connect(ctx context.Context) error {
	if err := ValidateSSHTarget(c.Target); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "dats-ssh-*")
	if err != nil {
		return fmt.Errorf("ssh: creating control directory: %w", err)
	}
	c.controlDir = dir
	c.controlPath = sshControlPath(dir, c.Target)
	if len(c.controlPath) >= sshControlPathMax {
		return fmt.Errorf("ssh: control socket path %q is too long for a unix socket", c.controlPath)
	}
	if err := c.startMaster(ctx); err != nil {
		return err
	}
	return c.probe(ctx)
}

// startMaster runs the master as a child we own.
func (c *SSHConfig) startMaster(ctx context.Context) error {
	args := []string{
		"-T",
		"-o", "BatchMode=yes",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=10",
		"-o", "ControlMaster=yes",
		"-o", "ControlPersist=no",
		"-o", "ControlPath=" + c.controlPath,
		"-N", c.Target,
	}
	master := exec.Command("ssh", args...)
	var stderr bytes.Buffer
	master.Stderr = &stderr
	if err := master.Start(); err != nil {
		return fmt.Errorf("ssh: starting connection to %s: %w", c.Target, err)
	}
	c.master = master

	deadline := time.Now().Add(sshConnectTimeout)
	for time.Now().Before(deadline) {
		check := exec.CommandContext(ctx, "ssh", "-o", "ControlPath="+c.controlPath, "-O", "check", c.Target)
		if check.Run() == nil {
			return nil
		}
		if master.ProcessState != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	c.stopMaster()
	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		detail = "no connection after " + sshConnectTimeout.String()
	}
	return fmt.Errorf("ssh: cannot connect to %s: %s", c.Target, detail)
}

// probe proves the remote shell returns a quoted argument unchanged and has the two tools a run needs.
func (c *SSHConfig) probe(ctx context.Context) error {
	script := "command -v bash >/dev/null 2>&1 || { echo 'no bash on the remote host' >&2; exit 90; }; " +
		"command -v tar >/dev/null 2>&1 || { echo 'no tar on the remote host' >&2; exit 91; }; " +
		sshRemoteScript([]string{"printf", "%s", sshProbePayload})

	out, err := c.output(ctx, script)
	if err != nil {
		return fmt.Errorf("ssh: %s is not usable: %w", c.Target, err)
	}
	if out != sshProbePayload {
		return fmt.Errorf("ssh: %s mangled a quoted argument, so its login shell is not POSIX (dats needs sh, bash, zsh or ksh)", c.Target)
	}
	return nil
}

// Close tears the master down. Safe to call on a config that never connected.
func (c *SSHConfig) Close() {
	c.stopMaster()
	if c.controlDir != "" {
		_ = os.RemoveAll(c.controlDir)
		c.controlDir = ""
	}
}

func (c *SSHConfig) stopMaster() {
	if c.master == nil || c.master.Process == nil {
		return
	}
	_ = c.master.Process.Kill()
	_, _ = c.master.Process.Wait()
	c.master = nil
}

// command builds an ssh invocation carrying script over the shared master.
func (c *SSHConfig) command(ctx context.Context, script string) *exec.Cmd {
	return exec.CommandContext(ctx, "ssh", sshArgv(c.Target, c.controlPath, script)[1:]...)
}

func (c *SSHConfig) output(ctx context.Context, script string) (string, error) {
	cmd := c.command(ctx, script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, detail)
	}
	return stdout.String(), nil
}

// AllocBase creates the per-file temp directory on the target.
func (c *SSHConfig) AllocBase(ctx context.Context) (string, error) {
	out, err := c.output(ctx, "mktemp -d 2>/dev/null || mktemp -d -t dats")
	if err != nil {
		return "", fmt.Errorf("ssh: creating remote temp directory: %w", err)
	}
	base := strings.TrimSpace(out)
	if base == "" {
		return "", fmt.Errorf("ssh: remote mktemp returned no path")
	}
	return base, nil
}

func (c *SSHConfig) RemoveBase(base string) {
	if base == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	_ = c.command(ctx, "rm -rf "+sshQuote(base)).Run()
}

func (c *SSHConfig) KillRemote(base, id string) {
	if base == "" || id == "" {
		return
	}
	pidFile := sshQuote(path.Join(base, sshPidDirName, id))
	script := "p=$(cat " + pidFile + " 2>/dev/null) || exit 0; [ -n \"$p\" ] || exit 0; " +
		"kill -TERM \"-$p\" 2>/dev/null || kill -TERM \"$p\" 2>/dev/null; sleep 1; " +
		"kill -KILL \"-$p\" 2>/dev/null || kill -KILL \"$p\" 2>/dev/null; :"
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	_ = c.command(ctx, script).Run()
}

// sshPidDirName holds the per-command pid files under a file's remote base.
const sshPidDirName = ".pids"

// Push copies a local directory tree onto the target.
func (c *SSHConfig) Push(ctx context.Context, localDir, remoteDir string) error {
	var buf bytes.Buffer
	if err := writeTarDir(localDir, &buf); err != nil {
		return fmt.Errorf("ssh: packing %s: %w", localDir, err)
	}
	script := "mkdir -p " + sshQuote(remoteDir) + " && exec tar -xpf - -C " + sshQuote(remoteDir)
	cmd := c.command(ctx, script)
	cmd.Stdin = &buf
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh: copying fixtures to %s: %w: %s", c.Target, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Pull copies one directory back from the target into localParent.
func (c *SSHConfig) Pull(ctx context.Context, remoteParent, name, localParent string) error {
	script := "exec tar -cf - -C " + sshQuote(remoteParent) + " " + sshQuote(name)
	cmd := c.command(ctx, script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh: collecting outputs from %s: %w: %s", c.Target, err, strings.TrimSpace(stderr.String()))
	}
	if err := extractTar(&stdout, localParent); err != nil {
		return fmt.Errorf("ssh: unpacking outputs: %w", err)
	}
	return nil
}

func writeTarDir(dir string, w io.Writer) error {
	tw := tar.NewWriter(w)
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		// A symlink's target travels as-is; nothing dats writes creates one.
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			if link, err = os.Readlink(p); err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

// extractTar unpacks r under dest.
func extractTar(r io.Reader, dest string) error {
	tr := tar.NewReader(r)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.FromSlash(header.Name)
		if !filepath.IsLocal(name) {
			return fmt.Errorf("archive member %q escapes the destination", header.Name)
		}
		target := filepath.Join(dest, name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)|0o700); err != nil {
				return err
			}
		case tar.TypeSymlink:
			_ = os.Remove(target)
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := writeTarFile(tr, target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		}
	}
}

func writeTarFile(r io.Reader, target string, mode os.FileMode) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}
