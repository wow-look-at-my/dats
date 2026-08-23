package runner

// Sandboxed command execution. Every command a .dats file runs -- test
// instances and file-level setup/teardown hooks alike -- can be wrapped in an
// OS-level sandbox instead of being handed straight to the host's bash.
//
// Three backends are supported, in preference order:
//
//	bwrap     bubblewrap (Linux): the OS's tool tree is bound read-only, the
//	          working directory read-only, the file's temp directory writable,
//	          /tmp is a private tmpfs, and the command runs in its own PID
//	          namespace. Of the HOST it exposes exactly what the docker
//	          backend exposes -- the working directory, and the temp directory
//	          it may write -- so a suite reaches the same places under either.
//	seatbelt  sandbox-exec (macOS): a generated SBPL profile instead of
//	          namespaces -- writes confined to the same directories, but reads
//	          NOT yet restricted, so it does not confine the host the way the
//	          other two now do (see docs/cli.md#sandboxing---sandbox). The
//	          macOS counterpart of bwrap; the two are mutually exclusive in
//	          practice, since each tool exists on exactly one platform.
//	docker    the command runs inside a container instead: the same host
//	          paths are mounted (temp directory read-write, working directory
//	          read-only) and the run's environment is forwarded, but the tools
//	          available are the IMAGE's, not the host's. A fallback for
//	          machines with neither native backend, not an equivalent.
//
// Backend selection is lazy and cached: the probe runs at most once per
// process, and only when a file actually needs a sandbox -- so a run that
// opted out never needs a backend installed at all, and `dats syntax` never
// probes.
//
// Sandboxing is the operator's call and only theirs. A .dats file can narrow
// its own sandbox (cut the network, name a docker image); it has no way to
// turn one off, because the person running an unfamiliar file is the one who
// would pay for that.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wow-look-at-my/dats/schema"
	"github.com/wow-look-at-my/go-containers/set"
)

// SandboxMode is the requested sandbox backend: a concrete backend, automatic
// selection, or no sandbox at all.
type SandboxMode string

const (
	// SandboxAuto picks the first usable backend: bwrap, then seatbelt, then
	// docker. The two native backends are platform-exclusive, so in practice
	// this reads as "the native sandbox for this OS, else docker".
	SandboxAuto SandboxMode = "auto"
	// SandboxBwrap requires bubblewrap; an unusable bwrap is an error rather
	// than a silent fallback.
	SandboxBwrap SandboxMode = "bwrap"
	// SandboxSeatbelt requires macOS's sandbox-exec, likewise.
	SandboxSeatbelt SandboxMode = "seatbelt"
	// SandboxDocker requires a reachable docker daemon, likewise.
	SandboxDocker SandboxMode = "docker"
	// SandboxNone runs commands directly on the host -- the opt-out.
	SandboxNone SandboxMode = "none"
)

// DefaultSandboxImage is the container image the docker backend runs commands
// in when neither the CLI nor the file names one. It is a small image that
// still ships bash (the shell every command is run through) and coreutils.
const DefaultSandboxImage = "debian:stable-slim"

// probeTimeout bounds each backend probe: a missing tool fails instantly, but
// an unresponsive docker daemon must not hang the run before a single test
// has started.
const probeTimeout = 20 * time.Second

// ParseSandboxMode converts the --sandbox flag's value into a SandboxMode,
// naming the accepted values on anything else.
func ParseSandboxMode(s string) (SandboxMode, error) {
	switch SandboxMode(s) {
	case SandboxAuto, SandboxBwrap, SandboxSeatbelt, SandboxDocker, SandboxNone:
		return SandboxMode(s), nil
	}
	return "", fmt.Errorf("unknown sandbox mode %q (use auto, bwrap, seatbelt, docker, or none)", s)
}

// SandboxConfig is the run-wide sandbox selection: which backend to use and,
// for docker, which image. A nil *SandboxConfig means no sandboxing -- an
// explicit opt-out, never a default anyone falls into: dats.Run and the CLI
// both pass a config unless the caller asked for none.
//
// Backend resolution is memoized: Backend probes at most once per config, no
// matter how many files or how many concurrent workers ask for it.
type SandboxConfig struct {
	Mode SandboxMode
	// Image is the operator's docker image, and empty means they named none.
	// That distinction is what lets a file's `image:` pick one without ever
	// overruling an image the operator typed out.
	Image string

	once    sync.Once
	backend SandboxMode
	err     error

	// probe reports whether a concrete backend is usable here. Swapped out by
	// tests, which must not depend on the host having bwrap or docker.
	probe func(SandboxMode) error
}

// NewSandboxConfig builds a config for mode. An empty image means the operator
// said nothing, which leaves the choice to the file (and DefaultSandboxImage
// when it says nothing either); a non-empty one is their pin, and it outranks
// any file's `image:`. The value is only ever consulted by the docker backend.
func NewSandboxConfig(mode SandboxMode, image string) *SandboxConfig {
	return &SandboxConfig{Mode: mode, Image: image, probe: probeBackend}
}

// Backend resolves the mode into the concrete backend to use, probing the
// host on the first call and reusing the answer afterwards. It returns an
// error when the requested backend is unusable (or, under auto, when no
// backend is) -- deliberately an error rather than a silent unsandboxed run:
// running unsandboxed is a choice the operator makes, never one the tool
// makes on their behalf.
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
			if err := probe(c.Mode); err != nil {
				c.err = fmt.Errorf("--sandbox=%s is not usable here: %w\n%s", c.Mode, err, sandboxOptOutHint)
				return
			}
			c.backend = c.Mode
		default: // SandboxAuto
			// Native backends first, in the order they can possibly succeed:
			// bwrap and seatbelt are platform-exclusive, so at most one of
			// them can pass anywhere, and each fails instantly (its tool is
			// simply absent) on the other platform. Docker is last: it is the
			// heaviest and the least equivalent.
			var failures []string
			for _, backend := range []SandboxMode{SandboxBwrap, SandboxSeatbelt, SandboxDocker} {
				err := probe(backend)
				if err == nil {
					c.backend = backend
					return
				}
				failures = append(failures, err.Error())
			}
			c.err = fmt.Errorf("no usable sandbox backend: %s\n%s", strings.Join(failures, "; "), sandboxOptOutHint)
		}
	})
	return c.backend, c.err
}

// sandboxOptOutHint is appended to every backend-resolution failure: the
// error is only actionable if it says how to run without a sandbox.
const sandboxOptOutHint = "install bubblewrap (Linux), or start docker, or opt out with --no-sandbox"

// probeBackend reports whether backend is usable on this host. Presence on
// $PATH is not enough for any of them -- bwrap is routinely installed on
// systems whose kernel denies it the user namespace it needs, sandbox-exec
// ships on every mac but is refused inside some hardened contexts, and the
// docker CLI is routinely installed with no daemon to talk to -- so every
// probe actually exercises the thing it will later depend on.
func probeBackend(backend SandboxMode) error {
	switch backend {
	case SandboxBwrap:
		return probeBwrap()
	case SandboxSeatbelt:
		return probeSeatbelt()
	case SandboxDocker:
		return probeDocker()
	}
	return fmt.Errorf("%s: not a concrete sandbox backend", backend)
}

func probeBwrap() error {
	path, err := exec.LookPath("bwrap")
	if err != nil {
		return fmt.Errorf("bwrap: not found in $PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	// The probe uses the same isolation primitives a real run does (the
	// namespace setup is what fails on a locked-down kernel), minus the
	// per-file binds. It chdirs to `/` because dats' own working directory is
	// one of those per-file binds: without it bwrap would refuse to enter an
	// inherited cwd that the confined mount set does not contain.
	args := append(bwrapIsolationArgs(), "--chdir", "/", "true")
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
	// Server-side format: the client alone answers `docker version` even with
	// no daemon behind it, and a client without a daemon cannot run anything.
	out, err := exec.CommandContext(ctx, path, "version", "--format", "{{.Server.APIVersion}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker: %s", probeFailure(out, err))
	}
	return nil
}

// probeFailure renders a probe's failure as one line: the tool's own first
// line of output when it said anything, else the process error.
func probeFailure(out []byte, err error) string {
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return err.Error()
}

// sandboxPlan is a resolved sandbox for one .dats file: the backend to run
// under, what the file may write, and whether it keeps the network. Nil means
// the file's commands run directly on the host.
type sandboxPlan struct {
	backend SandboxMode
	image   string // docker only
	// refusedImage is the file's own `image:` when the operator pinned a
	// different one on the command line. Kept so describe can say the file
	// asked and did not get it: a suite silently running in an image it did
	// not ask for fails later, somewhere that never mentions the image.
	refusedImage string
	network      bool
	// work is the file's temp directory -- the shared directory and every
	// per-instance test directory live under it, so binding it read-write is
	// exactly the access a passing test needs.
	work string
	// coverDir is the one host path made writable on top of work: --coverdir,
	// where instrumented binaries write coverage data that must SURVIVE the
	// run. There is deliberately no general "extra writable paths" knob --
	// scratch belongs in the sandbox's own writable temp directory, and
	// commands that genuinely need the host are run with --no-sandbox.
	coverDir string
	// workdir is the process working directory, exposed to the command so
	// relative paths keep resolving as they do on the host (read-only under
	// docker; already covered by the read-only root under bwrap).
	workdir string
}

// describe renders the plan for the run's output: the backend, the image it
// runs commands in when that is a choice (docker), and any narrowing the file
// asked for. Empty for a nil plan -- there is nothing to announce when
// commands run on the host.
func (p *sandboxPlan) describe() string {
	if p == nil {
		return ""
	}
	desc := string(p.backend)
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

// newSandboxPlan resolves the run-wide config and one file's sandbox block
// into the plan for that file, or nil when the file's commands run on the
// host. It returns an error only when a sandbox is required but no backend
// can provide it -- the file then fails outright rather than quietly running
// unsandboxed.
//
// The CLI's choice is the outer bound: --sandbox=none disables sandboxing for
// every file. A file's block can narrow that choice (cut the network) or
// adjust it (image); nothing in a file can widen it, and nothing in a file can
// switch the sandbox off.
func (r *Runner) newSandboxPlan(spec *schema.SandboxSpec, workDir string) (*sandboxPlan, error) {
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
		image:   r.Sandbox.Image,
		network: spec.NetworkEnabled(),
		work:    workDir,
	}
	// A typed --sandbox-image is the operator choosing what runs on their
	// machine, so a file cannot swap it out underneath them. It only picks the
	// image when they named none -- and when both name one, the file's is
	// refused OUT LOUD (describe), never dropped on the floor.
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
	// Coverage data is written by the sandboxed process itself, into a host
	// directory that is deliberately outside the temp tree -- it has to
	// outlive the run, which is what separates it from scratch.
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

// sandboxCommand is a command rewritten to run under a sandbox: the argv to
// spawn, plus an optional kill hook for backends whose workload outlives the
// process we spawned (docker's client is not the container).
type sandboxCommand struct {
	Argv []string
	Kill func()
}

// containerSeq numbers the docker containers this process starts, so each one
// gets a name we can kill by even when many run concurrently.
var containerSeq atomic.Uint64

// command rewrites `bash -c <cmd>` into the sandboxed argv that runs it.
// extraEnv holds the environment entries dats adds on top of the parent
// environment (a test's inputs.env, plus GOCOVERDIR): under bwrap the child
// inherits the environment as usual, but a container starts from the image's
// environment, so those entries have to be handed over explicitly.
func (p *sandboxPlan) command(cmd string, extraEnv []string) sandboxCommand {
	if p == nil {
		return sandboxCommand{Argv: []string{"bash", "-c", cmd}}
	}
	switch p.backend {
	case SandboxDocker:
		name := fmt.Sprintf("dats-%d-%d", os.Getpid(), containerSeq.Add(1))
		return sandboxCommand{
			Argv: p.dockerArgv(name, cmd, extraEnv),
			Kill: func() { killContainer(name) },
		}
	case SandboxSeatbelt:
		return sandboxCommand{Argv: p.seatbeltArgv(cmd)}
	}
	return sandboxCommand{Argv: p.bwrapArgv(cmd)}
}

// toolTreePaths is the OS tree a bwrap command runs against: the directories
// holding the system's executables, libraries and their configuration, and
// nothing else. It is the bwrap counterpart of the docker backend's IMAGE --
// where the tools come from -- and it is deliberately a LIST, not `/`.
//
// Binding `/` read-only, as this backend used to, is not a sandbox: it hands
// every command the whole host -- $HOME and its credentials, /var, every other
// checkout on the machine -- and it makes the two backends expose completely
// different filesystems, so a suite passing under one can be reading something
// under the other that does not exist there at all. What a command reaches is
// now the same set on both: this tool tree (the image, under docker) plus the
// working directory and the file's declared paths, and nothing else.
//
// Missing entries are fine -- each bind is a `-try` -- so one list covers
// merged-/usr and split-/usr distributions alike. /nix covers NixOS, where the
// tools live nowhere else, and /opt covers add-on toolchains installed outside
// the distribution's own tree -- notably GitHub Actions' HOSTED-runner tool
// cache (/opt/hostedtoolcache), which is where actions/setup-go, -node and
// -python put the toolchain they just put on PATH. Leaving it out does not
// make a command safer, it makes it fail to find the interpreter the workflow
// installed for it, several steps away from any mention of a sandbox.
//
// A self-hosted runner's tool cache is NOT under /opt -- it defaults to
// <runner-dir>/_work/_tool (confirmed live: exit 127, "command not found",
// for every command needing a setup-node-installed interpreter or an
// npm-linked binary, on a runner whose RUNNER_TOOL_CACHE was
// /home/runner/_work/_tool). bwrapIsolationArgs binds that path too, from the
// same env var actions/setup-* itself reads, so this covers any runner's
// actual tool-cache location instead of only the hosted-runner default.
var toolTreePaths = []string{
	"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32", "/etc", "/nix", "/opt",
}

// bwrapIsolationArgs are the backend's fixed arguments, shared by the probe
// and by every sandboxed command:
//
//	--ro-bind-try <tool tree>  the OS's executables, libraries and config,
//	                           readable but not writable -- and NOTHING else
//	                           of the host (see toolTreePaths)
//	--unshare-user             the namespace the others are nested inside
//	--unshare-pid              commands cannot see or signal host processes
//	--dev /dev                 a minimal device tree (null, zero, random, tty)
//	--proc /proc               a fresh procfs for the new PID namespace
//	--tmpfs /tmp               a private /tmp, so temp files never touch the host's
//	--die-with-parent          the sandbox dies with dats, never outliving the run
//
// --unshare-user is load-bearing on an unprivileged container. Creating a
// mount or PID namespace DIRECTLY requires CAP_SYS_ADMIN; creating one nested
// inside a user namespace does not, because the process holds full
// capabilities within that namespace. Without it bwrap asks the kernel for the
// PID namespace directly and is refused wherever that capability is absent,
// reporting "Creating new namespace failed: Operation not permitted" -- which
// names no namespace, so the argv looks correct while the sandbox cannot be
// built. Measured inside a slim CI runner: `unshare --user` exits 0 while
// `unshare --pid` and `unshare --mount` each fail EPERM, and a tmpfs mount
// performed inside a user namespace succeeds.
//
// Order matters and is load-bearing: the read-only tree comes first and the
// overlays after it, and per-file binds are appended after these so a writable
// path under /tmp survives the tmpfs.
func bwrapIsolationArgs() []string {
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
	return append(args,
		// The namespaces come FIRST. bwrap applies its arguments in order, so
		// `--proc /proc` mounts a procfs at the point it is read: before the
		// PID namespace exists, that is a mount of the HOST's procfs, which an
		// unprivileged container is refused ("Can't mount proc on
		// /newroot/proc: Operation not permitted"). After --unshare-pid it is
		// the new namespace's own procfs, which needs no privilege.
		"--unshare-user",
		"--unshare-pid",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--die-with-parent",
	)
}

// resolvConfTarget returns the file /etc/resolv.conf really points at, when
// that target sits outside the tool tree -- on systemd-resolved hosts it is a
// symlink into /run, and a dangling one leaves the sandbox with no DNS at all.
// It is the ONE host path bound beyond the tool tree and the file's own set,
// and it is a single FILE, not a directory tree: the resolver's configuration,
// which the docker backend gets from the container runtime instead.
// resolvConfTarget is a seam: tests drive the systemd-resolved shape (a
// symlink into /run) on a host whose /etc/resolv.conf is a plain file.
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

// bwrapArgv builds the full bwrap invocation for cmd. Beyond the fixed
// isolation args it exposes exactly what the docker backend exposes of the
// host and nothing more: the working directory read-only, and the file's temp
// directory (plus --coverdir) read-write. The writable binds come last so a
// path that is (or is inside) the working directory ends up writable instead
// of pinned read-only -- the same precedence docker's mount dedup gives it.
func (p *sandboxPlan) bwrapArgv(cmd string) []string {
	argv := append([]string{"bwrap"}, bwrapIsolationArgs()...)
	if !p.network {
		argv = append(argv, "--unshare-net")
	}
	if p.workdir != "" {
		argv = append(argv, "--ro-bind-try", p.workdir, p.workdir)
	}
	for _, dir := range p.writablePaths() {
		argv = append(argv, "--bind", dir, dir)
	}
	// The counterpart of docker's `-w`: relative paths resolve as they do on
	// the host. It comes after the binds that create the directory, and there
	// is exactly ONE --chdir in the argv -- bwrap warns on stderr about every
	// earlier one, which lands in the command's captured output and breaks
	// stderr assertions. Without a working directory, `/` is the one path
	// guaranteed to exist inside the sandbox.
	chdir := p.workdir
	if chdir == "" {
		chdir = "/"
	}
	argv = append(argv, "--chdir", chdir)
	return append(argv, "bash", "-c", cmd)
}

// dockerArgv builds the `docker run` invocation for cmd. The container is
// named so a timeout or a Ctrl-C can kill it: killing the client we spawned
// would otherwise leave the workload running.
func (p *sandboxPlan) dockerArgv(name, cmd string, extraEnv []string) []string {
	argv := []string{
		"docker", "run",
		"--rm",   // no container corpses after the run
		"-i",     // stdin is a first-class input for a test command
		"--init", // reap whatever the command forks and abandons
		"--name", name,
	}
	if !p.network {
		argv = append(argv, "--network", "none")
	}
	// Run as the invoking user so files landing in the bind-mounted temp
	// directory are owned by them, not by root.
	if uid := os.Getuid(); uid >= 0 {
		argv = append(argv, "--user", strconv.Itoa(uid)+":"+strconv.Itoa(os.Getgid()))
	}
	// Mount targets are deduplicated by path, read-write winning: an explicit
	// writable path that happens to be (or contain) the working directory
	// must not be demoted to the read-only working-directory mount, and a
	// repeated path would make docker refuse to start at all.
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
	// The run's environment, then dats' own additions on top. A command must
	// see the same variables under both backends -- bwrap's child inherits
	// them as a matter of course, and a container that started from the
	// image's environment alone made every caller-exported variable read as
	// empty under docker and non-empty under bwrap, from the same suite.
	for _, entry := range inheritedEnv() {
		argv = append(argv, "-e", entry)
	}
	for _, entry := range extraEnv {
		argv = append(argv, "-e", entry)
	}
	return append(argv, p.image, "bash", "-c", cmd)
}

// imageOwnedEnv are the variables a container defines for itself. Forwarding
// the host's values would point the container's shell at paths that exist only
// outside it -- a host PATH with no matching binaries, a $HOME nobody mounted
// -- so the image's own values win. Everything else the run exported is the
// caller's data and travels.
var imageOwnedEnv = set.Of(
	"PATH", "HOME", "HOSTNAME", "PWD", "OLDPWD",
	"SHLVL", "TMPDIR", "USER", "LOGNAME", "_",
)

// inheritedEnv is the parent environment minus the image-owned names, as
// KEY=VALUE entries, so the docker backend can hand a command the same
// variables the bwrap backend gives it by inheritance.
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

// writablePaths returns the paths the sandbox exposes read-write: the file's
// own temp directory, and --coverdir when the run collects coverage. Nothing
// else is writable, by design -- a command that needs scratch has the temp
// directory, and one that needs the host is not a sandboxed command.
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

// killContainer stops a container started by this process, best-effort: the
// command that owned it is already being torn down (timeout, cancellation),
// and a container that has exited on its own is not an error worth surfacing.
func killContainer(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	_ = exec.CommandContext(ctx, "docker", "kill", name).Run()
}
