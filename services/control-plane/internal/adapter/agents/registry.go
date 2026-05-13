// Package agents hosts one Provider subpackage per L1/L2 agent + a Registry
// that dispatches by name. Phase 5 ships the 5 L1 providers (architect,
// backend, qa, devops, data_engineer); Phase 6 fills in the L2 set.
package agents

import (
	"context"
	"fmt"
	"sync"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Registry maps an AgentName to its concrete domain.Agent implementation.
// Lazy-init friendly: NewRegistry takes a map keyed by name.
type Registry struct {
	mu     sync.RWMutex
	byName map[domain.AgentName]domain.Agent
}

// NewRegistry constructs an immutable Registry from the supplied map. The
// map is copied so callers can safely mutate the original.
func NewRegistry(agents map[domain.AgentName]domain.Agent) *Registry {
	cp := make(map[domain.AgentName]domain.Agent, len(agents))
	for k, v := range agents {
		cp[k] = v
	}
	return &Registry{byName: cp}
}

// Names returns the registered agent names — used by main.go's log line.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byName))
	for n := range r.byName {
		out = append(out, string(n))
	}
	return out
}

// Get returns the Agent registered under n, or an error.
func (r *Registry) Get(n domain.AgentName) (domain.Agent, error) {
	r.mu.RLock()
	a, ok := r.byName[n]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", n)
	}
	return a, nil
}

// Run is a convenience wrapper used by activities.go.
func (r *Registry) Run(ctx context.Context, n domain.AgentName, in domain.AgentInput) (domain.AgentOutput, error) {
	a, err := r.Get(n)
	if err != nil {
		return domain.AgentOutput{}, err
	}
	return a.Run(ctx, in)
}
