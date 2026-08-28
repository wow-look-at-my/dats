package runner

// Per-test ssh targets. A file's own target is HOME: shared/, setup and
// teardown only ever run there. A test that overrides gets its own temp
// directory on its own host, plus a push-only mirror of shared/ so
// {shared.X} still resolves.
//
// The wrinkle is worth stating plainly: SETUP PREPARES ONLY THE HOME HOST. A
// file whose setup starts a service has prepared one machine, and an
// overriding test runs against an unprepared one. The alternative -- setup
// once per distinct target -- silently redefines "runs once per file",
// breaks every non-idempotent setup, and makes teardown's failure semantics
// ambiguous across N hosts. A documented gap beats a redefined promise.

import (
	"context"

	"github.com/wow-look-at-my/dats/schema"
)

// remoteScope is one host this file reaches: its connection and the temp
// directory allocated on it.
type remoteScope struct {
	cfg  *SSHConfig
	base string
}

// scopeFor returns the connection and remote base one instance runs against.
// A nil connection means the instance runs here.
func (r *Runner) scopeFor(ctx context.Context, spec *schema.SSHSpec, sharedDir string) (*SSHConfig, string, error) {
	// No remote run at all: a per-test target cannot start one, because
	// ParseFile refuses it without a file-level target, and that target is
	// what would have set r.ssh.
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

// altScope builds (once) the scope for a host this file's tests overrode to.
// Instances run concurrently, so the whole build is under the lock: two
// instances naming one host must share a base, never race to allocate two.
func (r *Runner) altScope(ctx context.Context, spec *schema.SSHSpec, sharedDir string) (*remoteScope, error) {
	r.altMu.Lock()
	defer r.altMu.Unlock()

	target := spec.TargetName()
	if scope, ok := r.altScopes[target]; ok {
		return scope, nil
	}

	// The override goes through the same approval as the file's own target:
	// it is still a file naming a host.
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
	// shared/ is read-only to tests, so mirroring it is free. Writes an
	// overriding test makes to it are lost, which the format already calls
	// undefined under parallelism.
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

// closeAltScopes removes the temp directories this file claimed on hosts
// other than its home target. The connections belong to the run's manager
// and outlive the file.
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
