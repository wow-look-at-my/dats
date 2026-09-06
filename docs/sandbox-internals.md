# Sandbox internals

How `runner/sandbox.go` and `runner/sandbox_seatbelt.go` build the argv each backend runs, and which of their details are load-bearing. The user-facing view — what the flags do, what a sandboxed command can reach — is [cli.md](cli.md). The file-level `sandbox` key is [file-format.md](file-format.md#sandbox).

## `sandbox.go`

Sandboxing: `SandboxMode`/`SandboxConfig` (mode + docker image, where `Image: ""` means the operator named none and a file's `image:` may pick — a typed one outranks the file. `Backend()` memoizes detection behind a sync.Once, so the host is probed at most once per process and only when a file needs a sandbox). A file's spec can only narrow it), and the argv builders.

bwrap: `--ro-bind-try` of the OS tool tree (`toolTreePaths`: /usr,/bin,/sbin,/lib*,/etc,/nix,/opt — /opt covers add-on toolchains such as GitHub Actions' hosted tool cache, whose absence loses a workflow's. NEVER `/`, which exposed the whole host and made the backends diverge) + the resolv.conf target when it is a symlink outside it + `--dev`/`--proc`/`--tmpfs /tmp` +.

docker: `--rm -i --init --name <n>` + `--user` + rw bind of the work dir (targets deduplicated, rw wins) + ro bind of the cwd + `-e` for the inherited run environment (`inheritedEnv`, minus `imageOwnedEnv`) then dats-added env.

### An NT host has no working backend yet

bwrap is Linux and seatbelt is macOS, so Windows falls to docker — and a Windows runner's own daemon serves WINDOWS containers (`docker info` reports `OSType: windows`). A Linux daemon under WSL1 is not the answer: it installs, starts, and answers `docker info`, but WSL1's emulated kernel cannot create a container at all. A daemon that does not share the host's filesystem will also need every bind source written in ITS spelling, since `-v D:\a\x:D:\a\x` gives a Linux daemon.

So the docker PROBE asks the daemon which OS it serves (`docker version --format '{{.Server.APIVersion}} {{.Server.Os}}'`, `dockerServerUsable`). A windows daemon answers that command and then fails every run, so reporting it usable turned an unusable backend into a runc error per test. A client too old to print the server OS decides nothing, and the run is the test.

That failure is also MARKED. `runner.ErrNoBackendOnHost` wraps the auto error on an NT host, so a library caller can tell "this host can never sandbox" from "bubblewrap is not installed", which an install cures and which must. The marker only classifies the error: it grants no suite anything, and a file still cannot turn its own sandbox off. What a caller does with it -- fail, or say so loudly and run on the host -- is the run-starter's decision, the same decision `--no-sandbox` is.

So on an NT host a suite needs `--no-sandbox` today. WSL2 runs a real kernel and is the candidate that can change that, but not on a GitHub-hosted Windows runner: those VMs are already nested. Reaching it takes a host that owns its own hypervisor, and what it will then need is the bind-spelling map above.

**The two backends expose the SAME host paths** (cwd ro + declared writable), pinned by `TestBwrapAndDockerExposeTheSameHostPaths`. Seatbelt still does not restrict reads (known gap). The ONLY writable paths are the file's temp dir and `--coverdir` (`writablePaths`) -- there is deliberately no `sandbox.writable` key and no `--writable` flag: scratch goes. And a command that needs the host needs a `--no-sandbox` run (that includes a self-rewriting binary such as an APE -- copy it into the temp dir and run it there). The returned `Kill` hook `docker kill`s the container, since killing the client will leave the workload running.

**Scratch space is where the backends had to be made to agree.** bwrap mounts a private writable `/tmp` (`--tmpfs /tmp`), so a command that writes through. Seatbelt mounts nothing and its profile denies every write outside `writablePaths`, so the host's `TMPDIR` -- which is outside that set -- left a command with nowhere. A seatbelt plan therefore creates `<work>/.dats-tmp` (`sandboxTmpDirName`) and runs the command under `env TMPDIR=... TMP=... TEMP=...`. It needs no profile rule of its own, because `work` already covers it. The symptom this removes is the worst kind: a suite that passes on linux and fails on darwin for a reason that is in neither. `examples/sandbox.dats` asserts the property, and the `native-backends` job runs that file under bwrap and under seatbelt. So the assertion is checked where the backends actually differ. That job runs THIS commit's binary, not the published one: `action-every-host` downloads what buildhost already serves, so a runner-side sandbox change cannot be proven by.

Auto order is bwrap -> seatbelt -> docker: the two native backends are platform-exclusive. So this reads as "the native sandbox for this OS, else docker".

## `sandbox_seatbelt.go`

The macOS backend: `probeSeatbelt` (compiles+applies a real profile, since sandbox-exec's mere presence proves nothing) and the SBPL generator. `sandbox-exec -p <profile> bash -c cmd` (inline, no temp file).

The profile is LAST-MATCH-WINS and that order is the policy: `(allow default)` -> `(deny file-write*)` -> `(allow file-write* (subpath ...))`, plus `(deny network*)` when the file cut the network, and the writable device.

`seatbeltWritablePaths` resolves symlinks BEFORE writing subpath rules -- macOS matches the real path and dats' temp dirs arrive via /tmp -> /private/tmp, so an unresolved rule matches.

Unlike bwrap there is no PID namespace: files and network are confined. The process table is not.
