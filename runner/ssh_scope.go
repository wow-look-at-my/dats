package runner

// Per-test ssh targets; the file's own stays HOME.

import (
	"context"

	"github.com/wow-look-at-my/dats/schema"
)

// remoteScope is a single host this file reaches: its connection and the temp directory allocated on it.
type remoteScope struct {
	cfg  *SSHConfig
	base string
}

// scopeFor returns the connection and remote base a single instance runs against.
func (r *Runner) scopeFor(ctx context.Context, spec *schema.SSHSpec, sharedDir string) (*SSHConfig, string, error) {
	if r.ssh == nil {
		return nil, "", nil
	}
	target := spec.TargetName()
	if target == "" || target == r.ssh.Target {
		return r.ssh, r.remoteBase, nil
	}
	scope, err := r.altScope(ctx, spec, sharedDir)
	if err != nil {
		return nil, "", err
	}
	return scope.cfg, scope.base, nil
}

// altScope builds and caches the scope for a host this file's tests overrode to.
func (r *Runner) altScope(ctx context.Context, spec *schema.SSHSpec, sharedDir string) (*remoteScope, error) {
	r.altMu.Lock()
	defer r.altMu.Unlock()

	target := spec.TargetName()
	if scope, ok := r.altScopes[target]; ok {
		return scope, nil
	}

	// An override needs its own approval: it is still a file naming a host.
	cfg, _, err := r.SSH.Resolve(r.datsPath, spec)
	if err != nil {
		return nil, err
	}
	if err := cfg.Connect(ctx); err != nil {
		return nil, err
	}
	base, err := cfg.AllocBase(ctx)
	if err != nil {
		return nil, err
	}
	// shared/ is read-only to tests, so mirroring it is free; writes are lost.
	if err := cfg.Push(ctx, sharedDir, remoteJoin(base, sharedDirName)); err != nil {
		cfg.RemoveBase(base)
		return nil, err
	}

	scope := &remoteScope{cfg: cfg, base: base}
	if r.altScopes == nil {
		r.altScopes = make(map[string]*remoteScope)
	}
	r.altScopes[target] = scope
	return scope, nil
}

// closeAltScopes removes the temp directories this file claimed on hosts other than its home target.
func (r *Runner) closeAltScopes() {
	r.altMu.Lock()
	defer r.altMu.Unlock()
	for _, scope := range r.altScopes {
		if !r.KeepTemp {
			scope.cfg.RemoveBase(scope.base)
		}
	}
	r.altScopes = nil
}
