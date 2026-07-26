package command

import (
	"reflect"
	"testing"
)

func TestSurfaceDescriptorsDefineCanonicalPolicy(t *testing.T) {
	tests := []struct {
		id          string
		name        string
		aliases     []string
		visibility  SurfaceVisibility
		shortcut    string
		description string
		target      ExecutionTargetID
	}{
		{"command.help", "help", nil, SurfaceDiscoverable, "/help", "Show TUI commands and keys", "registry.help"},
		{"command.clear", "clear", nil, SurfaceDiscoverable, "/clear", "Clear the current transcript and start fresh", "registry.clear"},
		{"command.resume", "resume", []string{"sessions"}, SurfaceDiscoverable, "/resume", "List sessions or switch to a session by id", "registry.resume"},
		{"command.compact", "compact", nil, SurfaceDiscoverable, "/compact", "Compact prompt context", "tui-local.compact"},
		{"command.status", "status", nil, SurfaceDiscoverable, "/status", "Show current runtime status", "registry.status"},
		{"command.context", "context", nil, SurfaceDiscoverable, "/context", "Show context window usage breakdown", "registry.context"},
		{"command.memory", "memory", nil, SurfaceDiscoverable, "/memory", "Show or compact long-term memory", "registry.memory"},
		{"command.skills", "skills", nil, SurfaceDiscoverable, "/skills", "List or show local SKILL.md skills", "registry.skills"},
		{"command.undo", "undo", nil, SurfaceDiscoverable, "/undo", "Revert the most recent file write/edit (restore pre-write content)", "registry.undo"},
		{"command.export", "export", nil, SurfaceDiscoverable, "/export", "Export the current session to a text file", "registry.export"},
		{"command.copy", "copy", nil, SurfaceDiscoverable, "/copy", "Copy latest output", "tui-local.copy"},
		{"command.mouse", "mouse", nil, SurfaceDiscoverable, "/mouse", "Toggle mouse capture (off = terminal drag-select/copy)", "tui-local.mouse"},
		{"command.retry", "retry", nil, SurfaceDiscoverable, "/retry", "Rerun latest failed turn", "tui-local.retry"},
		{"command.exit", "exit", []string{"quit", "q"}, SurfaceDiscoverable, "/exit", "Quit BondCode", "exit"},
		{"command.model", "model", nil, SurfaceCompatibilityOnly, "/model", "Switch the active model without restarting (/model <name>; no arg shows current)", "registry.model"},
		{"command.permissions", "permissions", nil, SurfaceCompatibilityOnly, "/permissions", "Show permission mode", "registry.permissions"},
		{"command.diff", "diff", nil, SurfaceCompatibilityOnly, "/diff", "Review session file changes (diff viewer)", "overlay.diff"},
		{"command.history", "history", nil, SurfaceCompatibilityOnly, "/history", "Browse session timeline (fork-resume)", "overlay.history"},
		{"command.new", "new", nil, SurfaceCompatibilityOnly, "/new", "Start a fresh empty session", "registry.new"},
		{"command.session", "session", nil, SurfaceCompatibilityOnly, "/session", "Show current session details", "registry.session"},
		{"command.cost", "cost", nil, SurfaceCompatibilityOnly, "/cost", "Show cumulative model token usage", "registry.cost"},
		{"command.theme", "theme", nil, SurfaceCompatibilityOnly, "/theme", "Switch accent color", "tui-local.theme"},
	}

	got := SurfaceDescriptors()
	if len(got) != len(tests) {
		t.Fatalf("descriptor count = %d, want %d", len(got), len(tests))
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			descriptor := got[i]
			if descriptor.ID != tt.id {
				t.Errorf("ID = %q, want %q", descriptor.ID, tt.id)
			}
			if descriptor.Name != tt.name {
				t.Errorf("Name = %q, want %q", descriptor.Name, tt.name)
			}
			if !reflect.DeepEqual(descriptor.Aliases, tt.aliases) {
				t.Errorf("Aliases = %#v, want %#v", descriptor.Aliases, tt.aliases)
			}
			if descriptor.Visibility != tt.visibility {
				t.Errorf("Visibility = %q, want %q", descriptor.Visibility, tt.visibility)
			}
			if descriptor.Shortcut != tt.shortcut {
				t.Errorf("Shortcut = %q, want %q", descriptor.Shortcut, tt.shortcut)
			}
			if descriptor.Description != tt.description {
				t.Errorf("Description = %q, want %q", descriptor.Description, tt.description)
			}
			if descriptor.ExecutionTarget != tt.target {
				t.Errorf("ExecutionTarget = %q, want %q", descriptor.ExecutionTarget, tt.target)
			}
		})
	}
}

func TestSurfaceDiscoverableDescriptorsHaveExactOrder(t *testing.T) {
	want := []string{
		"help", "clear", "resume", "compact",
		"status", "context", "memory", "skills", "undo", "export",
		"copy", "mouse", "retry", "exit",
	}

	gotDescriptors := DiscoverableSurfaceDescriptors()
	got := make([]string, len(gotDescriptors))
	for i, descriptor := range gotDescriptors {
		got[i] = descriptor.Name
		if descriptor.Visibility != SurfaceDiscoverable {
			t.Errorf("descriptor %q visibility = %q, want %q", descriptor.Name, descriptor.Visibility, SurfaceDiscoverable)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverable names = %#v, want %#v", got, want)
	}
}

func TestSurfaceLookupResolvesCanonicalAndCompatibilityNames(t *testing.T) {
	tests := []struct {
		lookup string
		want   string
	}{
		{"help", "help"},
		{"/clear", "clear"},
		{"sessions", "resume"},
		{"session", "session"},
		{"quit", "exit"},
		{"q", "exit"},
		{"new", "new"},
		{"cost", "cost"},
		{"theme", "theme"},
	}

	for _, tt := range tests {
		t.Run(tt.lookup, func(t *testing.T) {
			descriptor, ok := LookupSurfaceDescriptor(tt.lookup)
			if !ok {
				t.Fatalf("LookupSurfaceDescriptor(%q) did not find a descriptor", tt.lookup)
			}
			if descriptor.Name != tt.want {
				t.Fatalf("LookupSurfaceDescriptor(%q).Name = %q, want %q", tt.lookup, descriptor.Name, tt.want)
			}
		})
	}
	if _, ok := LookupSurfaceDescriptor("unknown"); ok {
		t.Fatal("unknown surface name unexpectedly resolved")
	}
}

func TestSurfaceExecutionTargetsAreClassifiable(t *testing.T) {
	for _, descriptor := range SurfaceDescriptors() {
		t.Run(descriptor.Name, func(t *testing.T) {
			class := ClassifyExecutionTarget(descriptor.ExecutionTarget)
			switch class {
			case ExecutionTargetRegistry, ExecutionTargetTUILocal, ExecutionTargetOverlay, ExecutionTargetExit:
			default:
				t.Fatalf("target %q has unclassified target class %q", descriptor.ExecutionTarget, class)
			}
		})
	}
}

func TestSurfaceCustomDescriptorIsDiscoverableAndRegistryTargeted(t *testing.T) {
	descriptor := CustomSurfaceDescriptor("review", "Review this change")
	if descriptor.ID != "custom.review" {
		t.Errorf("ID = %q, want %q", descriptor.ID, "custom.review")
	}
	if descriptor.Name != "review" {
		t.Errorf("Name = %q, want %q", descriptor.Name, "review")
	}
	if descriptor.Visibility != SurfaceCustom {
		t.Errorf("Visibility = %q, want %q", descriptor.Visibility, SurfaceCustom)
	}
	if descriptor.Shortcut != "/review" {
		t.Errorf("Shortcut = %q, want %q", descriptor.Shortcut, "/review")
	}
	if descriptor.Description != "Review this change" {
		t.Errorf("Description = %q, want %q", descriptor.Description, "Review this change")
	}
	if descriptor.ExecutionTarget != "registry.review" {
		t.Errorf("ExecutionTarget = %q, want %q", descriptor.ExecutionTarget, "registry.review")
	}
	if class := ClassifyExecutionTarget(descriptor.ExecutionTarget); class != ExecutionTargetRegistry {
		t.Errorf("target class = %q, want %q", class, ExecutionTargetRegistry)
	}
}

func TestSurfaceDescriptorListsReturnIndependentCopies(t *testing.T) {
	first := SurfaceDescriptors()
	first[0].Name = "mutated"
	first[2].Aliases[0] = "mutated"

	second := SurfaceDescriptors()
	if second[0].Name != "help" {
		t.Fatalf("descriptor mutation leaked into policy: first name = %q", second[0].Name)
	}
	if second[2].Aliases[0] != "sessions" {
		t.Fatalf("alias mutation leaked into policy: first alias = %q", second[2].Aliases[0])
	}
}
