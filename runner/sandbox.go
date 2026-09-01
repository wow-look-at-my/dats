package runner

// Sandboxed command execution.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wow-look-at-my/dats/schema"
	"github.com/wow-look-at-my/go-containers/set"
)

type SandboxMode string

const (
	// SandboxAuto picks the earliest usable backend: bwrap, then seatbelt, then docker.
	SandboxAuto SandboxMode = "auto"
	// SandboxBwrap requires bubblewrap; an unusable bwrap is an error rather than a silent fallback.
	SandboxBwrap SandboxMode = "bwrap"
	// SandboxSeatbelt requires macOS's sandbox-exec, likewise.
	SandboxSeatbelt SandboxMode = "seatbelt"
	// SandboxDocker requires a reachable docker daemon, likewise.
	SandboxDocker SandboxMode = "docker"
	// SandboxNone runs commands directly on the host -- the opt-out.
	SandboxNone SandboxMode = "none"
)

const DefaultSandboxImage = "debian:stable-slim"

const probeTimeout = 20 * time.Second

func ParseSandboxMode(s string) (SandboxMode, error) {
	switch SandboxMode(s) {
	case SandboxAuto, SandboxBwrap, SandboxSeatbelt, SandboxDocker, SandboxNone:
		return SandboxMode(s), nil
	}
	return "", fmt.Errorf("unknown sandbox mode %q (use auto, bwrap, seatbelt, docker, or none)", s)
}

// SandboxConfig is the run-wide sandbox selection: which backend to use and, for docker, which image.
type SandboxConfig struct {
	Mode SandboxMode
	// Image is the operator's docker image, and empty means they named none.
	Image string

	once       sync.Once
	noticeOnce sync.Once
	backend    SandboxMode
	// proc is the /proc shape the bwrap probe settled on, meaningful only for that backend.
	proc procMode
	err  error

	// probe reports whether a concrete backend is usable here, and for bwrap which /proc shape it took.
	probe func(SandboxMode) (procMode, error)
}

// NewSandboxConfig builds a config for mode.
func NewSandboxConfig(mode SandboxMode, image string) *SandboxConfig {
	return &SandboxConfig{Mode: mode, Image: image, probe: probeBackend}
}

func (c *SandboxConfig) Backend() (SandboxMode, error) {
	c.once.Do(func() {
		probe := c.probe
		if probe == nil {
			probe = probeBackend
		}
		switch c.Mode {
		case SandboxNone, "":
			c.backend = SandboxNone
		case SandboxBwrap, SandboxSeatbelt, SandboxDocker:
			proc, err := probe(c.Mode)
			if err != nil {
				c.err = fmt.Errorf("--sandbox=%s is not usable here: %w\n%s", c.Mode, err, sandboxOptOutHint)
				return
			}
			c.backend, c.proc = c.Mode, proc
		default: // SandboxAuto
			var failures []string
			seatbeltFoundButUnusable := false
			for _, backend := range []SandboxMode{SandboxBwrap, SandboxSeatbelt, SandboxDocker} {
				proc, err := probe(backend)
				if err == nil {
					c.backend, c.proc = backend, proc
					return
				}
				failures = append(failures, err.Error())
				if backend == SandboxSeatbelt && !strings.HasSuffix(err.Error(), "not found in $PATH") {
					seatbeltFoundButUnusable = true
				}
			}
			c.err = fmt.Errorf("no usable sandbox backend: %s\n%s", strings.Join(failures, "; "), sandboxOptOutHint)
			if hostGOOS == "windows" || (hostGOOS == "darwin" && seatbeltFoundButUnusable) {
				c.err = fmt.Errorf("%w: %w", ErrNoBackendOnHost, c.err)
			}
		}
	})
	return c.backend, c.err
}

func (c *SandboxConfig) TakeProcNotice() string {
	if c == nil || c.backend != SandboxBwrap || c.proc != procShared {
		return ""
	}
	var note string
	c.noticeOnce.Do(func() { note = procSharedReason })
	return note
}

const sandboxOptOutHint = "install bubblewrap (Linux), or start docker, or opt out with --no-sandbox"

// ErrNoBackendOnHost marks the auto failure no install cures.
var ErrNoBackendOnHost = errors.New("this host has no sandbox backend dats can build on")

// probeBackend reports whether backend is usable on this host.
func probeBackend(backend SandboxMode) (procMode, error) {
	switch backend {
	case SandboxBwrap:
		return probeBwrap()
	case SandboxSeatbelt:
		return procFresh, probeSeatbelt()
	case SandboxDocker:
		return procFresh, probeDocker()
	}
	return procFresh, fmt.Errorf("%s: not a concrete sandbox backend", backend)
}

// probeBwrap reports whether bwrap can build a sandbox here, and which /proc shape it took to do it.
func probeBwrap() (procMode, error) {
	path, err := exec.LookPath("bwrap")
	if err != nil {
		return procFresh, fmt.Errorf("bwrap: not found in $PATH")
	}
	freshErr := runBwrapProbe(path, procFresh)
	if freshErr == nil {
		return procFresh, nil
	}
	if why := procBindWouldRevealOutsideProcesses(); why != "" {
		return procFresh, fmt.Errorf("%w (refusing the read-only /proc fallback: %s)", freshErr, why)
	}
	if err := runBwrapProbe(path, procShared); err == nil {
		return procShared, nil
	}
	return procFresh, freshErr
}

// runBwrapProbe exercises a single /proc shape.
func runBwrapProbe(path string, proc procMode) error {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	args := append(bwrapIsolationArgs(proc), "--chdir", "/", "true")
	if out, err := exec.CommandContext(ctx, path, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("bwrap: %s", probeFailure(out, err))
	}
	return nil
}

func probeDocker() error {
	path, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker: not found in $PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version", "--format", "{{.Server.APIVersion}} {{.Server.Os}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker: %s", probeFailure(out, err))
	}
	return dockerServerUsable(firstLine(out))
}

// firstLine is probeFailure's success-path twin: no error to fall back on.
func firstLine(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// dockerServerUsable reads the probe's "<api version> <server os>" line. A
// windows daemon answers `docker version` and then fails every run, because a
// sandbox image is a linux image. Saying so here lets auto keep looking.
func dockerServerUsable(versionLine string) error {
	fields := strings.Fields(versionLine)
	if len(fields) < 2 {
		return nil // an older client prints no server OS; the run is the test
	}
	if serverOS := fields[len(fields)-1]; serverOS == "windows" {
		return fmt.Errorf("docker: the daemon serves windows containers, and a sandbox image is a linux image")
	}
	return nil
}

func probeFailure(out []byte, err error) string {
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return err.Error()
}

type sandboxPlan struct {
	backend SandboxMode
	// proc is the /proc shape the bwrap probe settled on (bwrap only).
	proc  procMode
	image string // docker only
	// refusedImage is the file's own `image:` when the operator pinned a different image on the command line.
	refusedImage string
	network      bool
	work         string
	coverDir     string
	workdir      string
	// tmp is the command's scratch directory, inside work (seatbelt only).
	tmp string
	// ssh runs this file's commands on another machine, with no sandbox there.
	ssh *SSHConfig
	// refusedSSH is the file's own target when a typed target outranked it.
	refusedSSH string
	// remoteBase is the file's temp directory ON the target, mirroring work.
	remoteBase string
}

func (p *sandboxPlan) describe() string {
	if p == nil {
		return ""
	}
	if p.ssh != nil {
		desc := "none -- ssh " + p.ssh.Target + " (commands run on the remote host)"
		if p.refusedSSH != "" {
			desc += fmt.Sprintf(" (--ssh; file asked for %s)", p.refusedSSH)
		}
		return desc
	}
	desc := string(p.backend)
	if p.backend == SandboxBwrap && p.proc == procShared {
		desc += " (shared /proc)"
	}
	if p.backend == SandboxDocker {
		desc += " " + p.image
		if p.refusedImage != "" {
			desc += fmt.Sprintf(" (--sandbox-image; file asked for %s)", p.refusedImage)
		}
	}
	if !p.network {
		desc += " (no network)"
	}
	return desc
}

func (r *Runner) newSandboxPlan(spec *schema.SandboxSpec, workDir string) (*sandboxPlan, error) {
	if r.ssh != nil {
		if r.Sandbox != nil && r.Sandbox.Mode != SandboxAuto && r.Sandbox.Mode != SandboxNone && r.Sandbox.Mode != "" {
			return nil, fmt.Errorf("--sandbox=%s cannot be combined with --ssh: commands run on %s, and dats does not install a sandbox there", r.Sandbox.Mode, r.ssh.Target)
		}
		return &sandboxPlan{ssh: r.ssh, refusedSSH: r.refusedSSH, remoteBase: r.remoteBase, work: workDir}, nil
	}
	if r.Sandbox == nil || r.Sandbox.Mode == SandboxNone || r.Sandbox.Mode == "" {
		return nil, nil
	}
	backend, err := r.Sandbox.Backend()
	if err != nil {
		return nil, err
	}
	if backend == SandboxNone {
		return nil, nil
	}

	plan := &sandboxPlan{
		backend: backend,
		proc:    r.Sandbox.proc,
		image:   r.Sandbox.Image,
		network: spec.NetworkEnabled(),
		work:    workDir,
	}
	if fileImage := spec.ImageName(); fileImage != "" {
		if plan.image == "" {
			plan.image = fileImage
		} else if fileImage != plan.image {
			plan.refusedImage = fileImage
		}
	}
	if plan.image == "" {
		plan.image = DefaultSandboxImage
	}
	if backend == SandboxSeatbelt {
		// bwrap mounts a private writable /tmp; seatbelt mounts nothing.
		tmp := filepath.Join(workDir, sandboxTmpDirName)
		if err := os.MkdirAll(tmp, 0o755); err != nil {
			return nil, fmt.Errorf("sandbox: creating the scratch directory %q: %w", tmp, err)
		}
		plan.tmp = tmp
	}
	if r.CoverDir != "" {
		abs, err := filepath.Abs(r.CoverDir)
		if err != nil {
			return nil, fmt.Errorf("sandbox: resolving coverage directory %q: %w", r.CoverDir, err)
		}
		plan.coverDir = abs
	}
	if wd, err := os.Getwd(); err == nil {
		plan.workdir = wd
	}
	return plan, nil
}

// withSSH copies the plan onto another host, for a test that overrode the file's target.
func (p *sandboxPlan) withSSH(cfg *SSHConfig, remoteBase string) *sandboxPlan {
	if p == nil {
		return nil
	}
	c := *p
	c.ssh, c.remoteBase = cfg, remoteBase
	return &c
}

type sandboxCommand struct {
	Argv []string
	Kill func()
}

// commandSeq names workloads that outlive the process we spawn, so Kill finds them.
var commandSeq atomic.Uint64

func (p *sandboxPlan) command(cmd string, extraEnv []string) sandboxCommand {
	if p == nil {
		return sandboxCommand{Argv: []string{"bash", "-c", cmd}}
	}
	if p.ssh != nil {
		id := fmt.Sprintf("dats-%d-%d", os.Getpid(), commandSeq.Add(1))
		env := append(inheritedEnv(), extraEnv...)
		script := sshRemoteCommand(path.Join(p.remoteBase, sshPidDirName), id, env, []string{"bash", "-c", cmd})
		return sandboxCommand{
			Argv: sshArgv(p.ssh.Target, p.ssh.controlPath, script),
			Kill: func() { p.ssh.KillRemote(p.remoteBase, id) },
		}
	}
	switch p.backend {
	case SandboxDocker:
		name := fmt.Sprintf("dats-%d-%d", os.Getpid(), commandSeq.Add(1))
		return sandboxCommand{
			Argv: p.dockerArgv(name, cmd, extraEnv),
			Kill: func() { killContainer(name) },
		}
	case SandboxSeatbelt:
		return sandboxCommand{Argv: p.seatbeltArgv(cmd)}
	}
	return sandboxCommand{Argv: p.bwrapArgv(cmd)}
}

var toolTreePaths = []string{
	"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32", "/etc", "/nix", "/opt",
}

func bwrapIsolationArgs(proc procMode) []string {
	args := make([]string, 0, 3*len(toolTreePaths)+16)
	for _, dir := range toolTreePaths {
		args = append(args, "--ro-bind-try", dir, dir)
	}
	if cache := os.Getenv("RUNNER_TOOL_CACHE"); cache != "" && !underToolTree(cache) {
		args = append(args, "--ro-bind-try", cache, cache)
	}
	if target, ok := resolvConfTarget(); ok {
		args = append(args, "--ro-bind-try", target, target)
	}
	args = append(args,
		"--unshare-user",
		"--unshare-pid",
		"--dev", "/dev",
	)
	// --unshare-pid stays in BOTH shapes.
	if proc == procShared {
		args = append(args, "--ro-bind", "/proc", "/proc")
	} else {
		args = append(args, "--proc", "/proc")
	}
	return append(args,
		"--tmpfs", "/tmp",
		"--die-with-parent",
	)
}

var resolvConfTarget = resolvConfTargetFS

func resolvConfTargetFS() (string, bool) {
	real, err := filepath.EvalSymlinks("/etc/resolv.conf")
	if err != nil || underToolTree(real) {
		return "", false
	}
	return real, true
}

// underToolTree reports whether path is already covered by a tool-tree bind.
func underToolTree(path string) bool {
	for _, dir := range toolTreePaths {
		if path == dir || strings.HasPrefix(path, dir+"/") {
			return true
		}
	}
	return false
}

// bwrapArgv builds the full bwrap invocation for cmd.
func (p *sandboxPlan) bwrapArgv(cmd string) []string {
	argv := append([]string{"bwrap"}, bwrapIsolationArgs(p.proc)...)
	if !p.network {
		argv = append(argv, "--unshare-net")
	}
	if p.workdir != "" {
		argv = append(argv, "--ro-bind-try", p.workdir, p.workdir)
	}
	for _, dir := range p.writablePaths() {
		argv = append(argv, "--bind", dir, dir)
	}
	// The counterpart of docker's `-w`: relative paths resolve as they do on the host.
	chdir := p.workdir
	if chdir == "" {
		chdir = "/"
	}
	argv = append(argv, "--chdir", chdir)
	return append(argv, "bash", "-c", cmd)
}

// dockerArgv builds the `docker run` invocation for cmd.
func (p *sandboxPlan) dockerArgv(name, cmd string, extraEnv []string) []string {
	argv := []string{
		"docker", "run",
		"--rm",   // no container corpses after the run
		"-i",     // stdin is an ordinary input for a test command
		"--init", // reap whatever the command forks and abandons
		"--name", name,
	}
	if !p.network {
		argv = append(argv, "--network", "none")
	}
	if uid := os.Getuid(); uid >= 0 {
		argv = append(argv, "--user", strconv.Itoa(uid)+":"+strconv.Itoa(os.Getgid()))
	}
	seen := set.New[string]()
	for _, dir := range p.writablePaths() {
		if !seen.Add(dir) {
			continue
		}
		argv = append(argv, "-v", dir+":"+dir)
	}
	if p.workdir != "" {
		if !seen.Contains(p.workdir) {
			argv = append(argv, "-v", p.workdir+":"+p.workdir+":ro")
		}
		argv = append(argv, "-w", p.workdir)
	}
	// The run's environment, then dats' own additions on top.
	for _, entry := range inheritedEnv() {
		argv = append(argv, "-e", entry)
	}
	for _, entry := range extraEnv {
		argv = append(argv, "-e", entry)
	}
	return append(argv, p.image, "bash", "-c", cmd)
}

// imageOwnedEnv are the variables a container defines for itself.
var imageOwnedEnv = set.Of(
	"PATH", "HOME", "HOSTNAME", "PWD", "OLDPWD",
	"SHLVL", "TMPDIR", "USER", "LOGNAME", "_",
)

func inheritedEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || imageOwnedEnv.Contains(name) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func (p *sandboxPlan) writablePaths() []string {
	paths := make([]string, 0, 2)
	if p.work != "" {
		paths = append(paths, p.work)
	}
	if p.coverDir != "" {
		paths = append(paths, p.coverDir)
	}
	return paths
}

func killContainer(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	_ = exec.CommandContext(ctx, "docker", "kill", name).Run()
}
