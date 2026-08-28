package runner

import (
	"fmt"
	"sync"

	"github.com/wow-look-at-my/dats/schema"
)

type SSHManager struct {
	// Target is the operator's typed target; it outranks a file's own ssh:.
	Target string

	// Allow vets a target a FILE named, never Target; nil refuses them all.
	Allow func(datsPath, target string) error

	mu      sync.Mutex
	configs map[string]*SSHConfig
}

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
