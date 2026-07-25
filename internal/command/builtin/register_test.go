package builtin

import (
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/command"
)

func TestCommandSurfaceRegisterAllCoversRegistryTargets(t *testing.T) {
	registry := command.NewRegistry()
	if err := RegisterAll(registry); err != nil {
		t.Fatal(err)
	}

	for _, descriptor := range command.SurfaceDescriptors() {
		if command.ClassifyExecutionTarget(descriptor.ExecutionTarget) != command.ExecutionTargetRegistry {
			continue
		}
		name := strings.TrimPrefix(string(descriptor.ExecutionTarget), "registry.")
		if _, ok := registry.Get(name); !ok {
			t.Errorf("descriptor %q targets missing registry command %q", descriptor.Name, name)
		}
	}
	for _, name := range []string{"sessions", "session", "new", "cost"} {
		if _, ok := registry.Get(name); !ok {
			t.Errorf("missing compatibility registry command %q", name)
		}
	}
}
