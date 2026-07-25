package tool

import (
	"fmt"
	"sort"
	"strings"
)

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

func (r *Registry) Register(t Tool) error {
	if t == nil {
		return fmt.Errorf("tool is nil")
	}
	name := t.Name()
	if name == "" {
		return fmt.Errorf("tool name is empty")
	}
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q already registered", name)
	}
	r.tools[name] = t
	return nil
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Unregister removes a tool by name. Returns true if it was present.
func (r *Registry) Unregister(name string) bool {
	if r == nil || r.tools == nil {
		return false
	}
	if _, ok := r.tools[name]; !ok {
		return false
	}
	delete(r.tools, name)
	return true
}

// UnregisterPrefix removes every tool whose name has the given prefix.
func (r *Registry) UnregisterPrefix(prefix string) int {
	if r == nil || r.tools == nil || prefix == "" {
		return 0
	}
	n := 0
	for name := range r.tools {
		if strings.HasPrefix(name, prefix) {
			delete(r.tools, name)
			n++
		}
	}
	return n
}

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
