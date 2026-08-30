package runner

import (
	"context"
	"os/exec"
	"strings"
	"sync"
)

// A daemon in WSL reads `D:\a` as a VOLUME NAME -- docs/sandbox-internals.md.
var (
	daemonPathOnce sync.Once
	daemonPathFn   func(string) string
	// dockerInfoOS is the seam a test answers instead of a live daemon.
	dockerInfoOS = liveDockerInfoOS
)

// daemonPath spells a host path the way the docker daemon reads it.
func daemonPath(host string) string {
	daemonPathOnce.Do(func() { daemonPathFn = resolveDaemonPath() })
	return daemonPathFn(host)
}

func resolveDaemonPath() func(string) string {
	identity := func(host string) string { return host }
	if hostGOOS != "windows" {
		return identity
	}
	if strings.Contains(dockerInfoOS(), "Docker Desktop") {
		return identity
	}
	return wslPath
}

// wslPath spells an NT path the way a daemon under WSL sees it: the drive is a
// directory under /mnt.
func wslPath(host string) string {
	p := strings.ReplaceAll(host, `\`, "/")
	if len(p) < 2 || p[1] != ':' {
		return p
	}
	return "/mnt/" + strings.ToLower(p[:1]) + p[2:]
}

// liveDockerInfoOS asks the daemon what it runs on. An unreachable daemon
// answers nothing, and the caller then treats the paths as untranslated.
func liveDockerInfoOS() string {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.OperatingSystem}}").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// mapCommandPaths rewrites the roots this plan binds into the daemon's own
// spelling, so a path the command carries names the directory that got mounted.
func (p *sandboxPlan) mapCommandPaths(cmd string) string {
	for _, dir := range append(p.writablePaths(), p.workdir) {
		if dir == "" {
			continue
		}
		mapped := daemonPath(dir)
		if mapped == dir {
			continue
		}
		cmd = strings.ReplaceAll(cmd, strings.ReplaceAll(dir, `\`, "/"), mapped)
		cmd = strings.ReplaceAll(cmd, dir, mapped)
	}
	return cmd
}
