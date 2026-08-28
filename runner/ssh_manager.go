package runner

import (
	"fmt"
	"sync"

	"github.com/wow-look-at-my/dats/schema"
)

// SSHManager decides, per file, which machine that file's commands run on,
// and owns one connection per distinct target for the whole run.
type SSHManager struct {
	// Target is the operator's typed target. It applies to every file and
	// outranks any file's own ssh:, the way a typed --sandbox-image outranks
	// a file's image:.
	Target string

	// Allow is consulted for a target a FILE named, never for Target. A
	// non-nil error refuses the file, and its text is what the operator
	// sees. Nil refuses every file-declared target: a library caller that
	// says nothing must not dial out on a file's say-so.
	Allow func(datsPath, target string) error

	mu      sync.Mutex
	configs map[string]*SSHConfig
}

// Resolve returns the connection for one file, the file's own target when it
// was refused in favour of a typed one (so the run can announce it rather
// than swap silently), or nil when the file's commands run here.
func (m *SSHManager) Resolve(datsPath string, spec *schema.SSHSpec) (*SSHConfig, string, error) {
	if m == nil {
		return nil, "", nil
	}
	fileTarget := spec.TargetName()

	if m.Target != "" {
		refused := ""
		if fileTarget != "" && fileTarget != m.Target {
			refused = fileTarget
		}
		return m.config(m.Target), refused, nil
	}
	if fileTarget == "" {
		return nil, "", nil
	}
	if err := ValidateSSHTarget(fileTarget); err != nil {
		return nil, "", err
	}
	if m.Allow == nil {
		return nil, "", fmt.Errorf("ssh: %s asks to run its commands on %s, which is not approved -- approve it with `dats trust add %s %s`", datsPath, fileTarget, datsPath, fileTarget)
	}
	if err := m.Allow(datsPath, fileTarget); err != nil {
		return nil, "", err
	}
	return m.config(fileTarget), "", nil
}

// config memoizes one connection per target: several files may share one,
// and a run must not open a second connection for the same machine.
func (m *SSHManager) config(target string) *SSHConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.configs == nil {
		m.configs = make(map[string]*SSHConfig)
	}
	if c, ok := m.configs[target]; ok {
		return c
	}
	c := NewSSHConfig(target)
	m.configs[target] = c
	return c
}

// Close tears down every connection the run opened.
func (m *SSHManager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.configs {
		c.Close()
	}
	m.configs = nil
}
