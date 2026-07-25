package command

import "strings"

// SurfaceVisibility controls whether an interaction is shown by discovery
// surfaces. Compatibility-only descriptors remain lookupable for legacy input,
// while custom descriptors represent prompt templates loaded at runtime.
type SurfaceVisibility string

const (
	SurfaceDiscoverable      SurfaceVisibility = "discoverable"
	SurfaceCompatibilityOnly SurfaceVisibility = "compatibility-only"
	SurfaceCustom            SurfaceVisibility = "custom"
)

// ExecutionTargetID identifies a command destination without importing the
// registry, TUI, overlay, or Bubble Tea execution layers.
type ExecutionTargetID string

// ExecutionTargetClass is the dependency-neutral dispatch family encoded by an
// ExecutionTargetID.
type ExecutionTargetClass string

const (
	ExecutionTargetUnknown  ExecutionTargetClass = "unknown"
	ExecutionTargetRegistry ExecutionTargetClass = "registry"
	ExecutionTargetTUILocal ExecutionTargetClass = "tui-local"
	ExecutionTargetOverlay  ExecutionTargetClass = "overlay"
	ExecutionTargetExit     ExecutionTargetClass = "exit"
)

// SurfaceDescriptor is the canonical, execution-neutral description of one
// slash interaction. Aliases are accepted compatibility names and are never
// separate discovery entries.
type SurfaceDescriptor struct {
	ID              string
	Name            string
	Aliases         []string
	Visibility      SurfaceVisibility
	Shortcut        string
	Description     string
	ExecutionTarget ExecutionTargetID
}

var builtinSurfaceDescriptors = []SurfaceDescriptor{
	{ID: "command.help", Name: "help", Visibility: SurfaceDiscoverable, Shortcut: "/help", Description: "Show TUI commands and keys", ExecutionTarget: "registry.help"},
	{ID: "command.clear", Name: "clear", Visibility: SurfaceDiscoverable, Shortcut: "/clear", Description: "Clear the current transcript and start fresh", ExecutionTarget: "registry.clear"},
	{ID: "command.resume", Name: "resume", Aliases: []string{"sessions"}, Visibility: SurfaceDiscoverable, Shortcut: "/resume", Description: "List sessions or switch to a session by id", ExecutionTarget: "registry.resume"},
	{ID: "command.model", Name: "model", Visibility: SurfaceDiscoverable, Shortcut: "/model", Description: "Switch the active model without restarting (/model <name>; no arg shows current)", ExecutionTarget: "registry.model"},
	{ID: "command.permissions", Name: "permissions", Visibility: SurfaceDiscoverable, Shortcut: "/permissions", Description: "Show permission mode", ExecutionTarget: "registry.permissions"},
	{ID: "command.compact", Name: "compact", Visibility: SurfaceDiscoverable, Shortcut: "/compact", Description: "Compact prompt context", ExecutionTarget: "tui-local.compact"},
	{ID: "command.status", Name: "status", Visibility: SurfaceDiscoverable, Shortcut: "/status", Description: "Show current runtime status", ExecutionTarget: "registry.status"},
	{ID: "command.context", Name: "context", Visibility: SurfaceDiscoverable, Shortcut: "/context", Description: "Show context window usage breakdown", ExecutionTarget: "registry.context"},
	{ID: "command.memory", Name: "memory", Visibility: SurfaceDiscoverable, Shortcut: "/memory", Description: "Show or compact long-term memory", ExecutionTarget: "registry.memory"},
	{ID: "command.skills", Name: "skills", Visibility: SurfaceDiscoverable, Shortcut: "/skills", Description: "List or show local SKILL.md skills", ExecutionTarget: "registry.skills"},
	{ID: "command.undo", Name: "undo", Visibility: SurfaceDiscoverable, Shortcut: "/undo", Description: "Revert the most recent file write/edit (restore pre-write content)", ExecutionTarget: "registry.undo"},
	{ID: "command.export", Name: "export", Visibility: SurfaceDiscoverable, Shortcut: "/export", Description: "Export the current session to a text file", ExecutionTarget: "registry.export"},
	{ID: "command.copy", Name: "copy", Visibility: SurfaceDiscoverable, Shortcut: "/copy", Description: "Copy latest output", ExecutionTarget: "tui-local.copy"},
	{ID: "command.mouse", Name: "mouse", Visibility: SurfaceDiscoverable, Shortcut: "/mouse", Description: "Toggle mouse capture (off = terminal drag-select/copy)", ExecutionTarget: "tui-local.mouse"},
	{ID: "command.retry", Name: "retry", Visibility: SurfaceDiscoverable, Shortcut: "/retry", Description: "Rerun latest failed turn", ExecutionTarget: "tui-local.retry"},
	{ID: "command.diff", Name: "diff", Visibility: SurfaceDiscoverable, Shortcut: "/diff", Description: "Review session file changes (diff viewer)", ExecutionTarget: "overlay.diff"},
	{ID: "command.history", Name: "history", Visibility: SurfaceDiscoverable, Shortcut: "/history", Description: "Browse session timeline (fork-resume)", ExecutionTarget: "overlay.history"},
	{ID: "command.exit", Name: "exit", Aliases: []string{"quit", "q"}, Visibility: SurfaceDiscoverable, Shortcut: "/exit", Description: "Quit BondCode", ExecutionTarget: "exit"},
	{ID: "command.new", Name: "new", Visibility: SurfaceCompatibilityOnly, Shortcut: "/new", Description: "Start a fresh empty session", ExecutionTarget: "registry.new"},
	{ID: "command.session", Name: "session", Visibility: SurfaceCompatibilityOnly, Shortcut: "/session", Description: "Show current session details", ExecutionTarget: "registry.session"},
	{ID: "command.cost", Name: "cost", Visibility: SurfaceCompatibilityOnly, Shortcut: "/cost", Description: "Show cumulative model token usage", ExecutionTarget: "registry.cost"},
	{ID: "command.theme", Name: "theme", Visibility: SurfaceCompatibilityOnly, Shortcut: "/theme", Description: "Switch accent color", ExecutionTarget: "tui-local.theme"},
}

// SurfaceDescriptors returns the canonical builtin policy in stable order.
// The returned descriptors and alias slices are independent copies.
func SurfaceDescriptors() []SurfaceDescriptor {
	return cloneSurfaceDescriptors(builtinSurfaceDescriptors)
}

// DiscoverableSurfaceDescriptors returns only canonical builtin entries shown
// by slash discovery, preserving their policy order.
func DiscoverableSurfaceDescriptors() []SurfaceDescriptor {
	descriptors := make([]SurfaceDescriptor, 0, len(builtinSurfaceDescriptors))
	for _, descriptor := range builtinSurfaceDescriptors {
		if descriptor.Visibility == SurfaceDiscoverable {
			descriptors = append(descriptors, cloneSurfaceDescriptor(descriptor))
		}
	}
	return descriptors
}

// LookupSurfaceDescriptor resolves a canonical builtin name or compatibility
// alias. A leading slash is accepted for callers working with shortcuts.
func LookupSurfaceDescriptor(name string) (SurfaceDescriptor, bool) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	for _, descriptor := range builtinSurfaceDescriptors {
		if descriptor.Name == name {
			return cloneSurfaceDescriptor(descriptor), true
		}
		for _, alias := range descriptor.Aliases {
			if alias == name {
				return cloneSurfaceDescriptor(descriptor), true
			}
		}
	}
	return SurfaceDescriptor{}, false
}

// CustomSurfaceDescriptor describes a runtime prompt-template command using the
// same neutral policy shape as builtins.
func CustomSurfaceDescriptor(name, description string) SurfaceDescriptor {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	return SurfaceDescriptor{
		ID:              "custom." + name,
		Name:            name,
		Visibility:      SurfaceCustom,
		Shortcut:        "/" + name,
		Description:     description,
		ExecutionTarget: ExecutionTargetID("registry." + name),
	}
}

// ClassifyExecutionTarget returns the later dispatch family encoded by target.
func ClassifyExecutionTarget(target ExecutionTargetID) ExecutionTargetClass {
	value := string(target)
	switch {
	case value == "exit":
		return ExecutionTargetExit
	case hasExecutionTargetPrefix(value, string(ExecutionTargetRegistry)):
		return ExecutionTargetRegistry
	case hasExecutionTargetPrefix(value, string(ExecutionTargetTUILocal)):
		return ExecutionTargetTUILocal
	case hasExecutionTargetPrefix(value, string(ExecutionTargetOverlay)):
		return ExecutionTargetOverlay
	default:
		return ExecutionTargetUnknown
	}
}

func hasExecutionTargetPrefix(target, class string) bool {
	return strings.HasPrefix(target, class+".") && len(target) > len(class)+1
}

func cloneSurfaceDescriptors(descriptors []SurfaceDescriptor) []SurfaceDescriptor {
	cloned := make([]SurfaceDescriptor, len(descriptors))
	for i, descriptor := range descriptors {
		cloned[i] = cloneSurfaceDescriptor(descriptor)
	}
	return cloned
}

func cloneSurfaceDescriptor(descriptor SurfaceDescriptor) SurfaceDescriptor {
	descriptor.Aliases = append([]string(nil), descriptor.Aliases...)
	return descriptor
}
