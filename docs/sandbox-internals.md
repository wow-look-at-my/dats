# Sandbox internals

How `runner/sandbox.go` and `runner/sandbox_seatbelt.go` build the argv each backend runs, and
which of their details are load-bearing. The user-facing view — what the flags do, what a
sandboxed command can reach — is [cli.md](cli.md); the file-level `sandbox` key is
[file-format.md](file-format.md#sandbox).

## `sandbox.go`

Sandboxing: `SandboxMode`/`SandboxConfig` (mode + docker image, where `Image: ""` means the
operator named none and a file's `image:` may pick — a typed one outranks the file, and
`plan.refusedImage` puts that on the `# sandbox:` line instead of silently swapping images;
`Backend()` memoizes detection behind a sync.Once, so the host is probed at most once per process
and only when a file needs a sandbox), the probes (all EXERCISE the backend — bwrap is often
installed on kernels denying it a userns, the docker CLI often has no daemon), `newSandboxPlan`
(CLI choice is the outer bound; a file's spec can only narrow it), and the argv builders.

bwrap: `--ro-bind-try` of the OS tool tree (`toolTreePaths`: /usr,/bin,/sbin,/lib*,/etc,/nix,/opt
— /opt covers add-on toolchains such as GitHub Actions' hosted tool cache, whose absence loses a
workflow's setup-go/-node/-python interpreter; NEVER `/`, which exposed the whole host and made
the backends diverge) + the resolv.conf target when it is a symlink outside it +
`--dev`/`--proc`/`--tmpfs /tmp` + `--unshare-pid --die-with-parent`, then ro bind of the cwd, then
per-file `--bind`s, then exactly ONE `--chdir` (a second makes bwrap warn into the command's
captured stderr), `--unshare-net` when network is off (ORDER IS LOAD-BEARING: the tmpfs must
precede a work dir under /tmp, and the writable binds follow the cwd bind so a writable path
inside it wins).

docker: `--rm -i --init --name <n>` + `--user` + rw bind of the work dir (targets deduplicated, rw
wins) + ro bind of the cwd + `-e` for the inherited run environment (`inheritedEnv`, minus
`imageOwnedEnv`) then dats-added env, ending in `image bash -c cmd`.

### An NT host has no working backend yet

bwrap is Linux and seatbelt is macOS, so Windows falls to docker — and a Windows runner's own daemon
serves WINDOWS containers (`docker info` reports `OSType: windows`). A Linux daemon under WSL1 is not
the answer: it installs, starts, and answers `docker info`, but WSL1's emulated kernel cannot create
a container at all (MEASURED on windows-latest: `docker run --rm debian:stable-slim true` fails in
runc with `error during container init: fetch packet length from socket: recvfrom: invalid
argument`). A daemon that does not share the host's filesystem would also need every bind source
written in ITS spelling, since `-v D:\a\x:D:\a\x` gives a Linux daemon a source with no leading
slash, which docker reads as a VOLUME NAME and silently mounts as an empty directory.

So the docker PROBE asks the daemon which OS it serves (`docker version --format '{{.Server.APIVersion}}
{{.Server.Os}}'`, `dockerServerUsable`). A windows daemon answers that command and then fails every run,
so reporting it usable turned an unusable backend into a runc error per test rather than a line saying
auto found no backend. A client too old to print the server OS decides nothing, and the run is the test.

So on an NT host a suite needs `--no-sandbox` today. WSL2 runs a real kernel and is the candidate that
could change that, but not on a GitHub-hosted Windows runner: those VMs are already nested, and GitHub
states nested virtualization cannot be enabled on them, which is what WSL2 needs. Reaching it takes a
host that owns its own hypervisor, and what it would then need is the bind-spelling map above.

**The two backends expose the SAME host paths** (cwd ro + declared writable), pinned by
`TestBwrapAndDockerExposeTheSameHostPaths`; seatbelt still does not restrict reads (known gap).
The ONLY writable paths are the file's temp dir and `--coverdir` (`writablePaths`) -- there is
deliberately no `sandbox.writable` key and no `--writable` flag: scratch goes in the temp dir, and
a command that needs the host needs a `--no-sandbox` run (that includes a self-rewriting binary
such as an APE -- copy it into the temp dir and run it there); the returned `Kill` hook `docker
kill`s the container, since killing the client would leave the workload running.

Auto order is bwrap -> seatbelt -> docker: the two native backends are platform-exclusive, so this
reads as "the native sandbox for this OS, else docker".

## `sandbox_seatbelt.go`

The macOS backend: `probeSeatbelt` (compiles+applies a real profile, since sandbox-exec's mere
presence proves nothing) and the SBPL generator. `sandbox-exec -p <profile> bash -c cmd` (inline,
no temp file).

The profile is LAST-MATCH-WINS and that order is the policy: `(allow default)` -> `(deny
file-write*)` -> `(allow file-write* (subpath ...))`, plus `(deny network*)` when the file cut the
network, and the writable device nodes a shell needs (/dev/null, /dev/fd, tty).

`seatbeltWritablePaths` resolves symlinks BEFORE writing subpath rules -- macOS matches the real
path and dats' temp dirs arrive via /tmp -> /private/tmp, so an unresolved rule matches nothing and
every fixture write is denied.

Unlike bwrap there is no PID namespace: files and network are confined, the process table is not.
