package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/tool"
)

// RefreshOptions controls how MCP tools are registered.
type RefreshOptions struct {
	// NamespaceTools forces mcp__server__tool names. Always applied for inject.
	NamespaceTools bool
	// ReplaceExisting unregisters previous mcp__ tools before registering.
	ReplaceExisting bool
}

// RefreshRegistry lists tools from all connected servers and registers them.
// Tool names are always fully qualified: mcp__{server}__{tool}.
func RefreshRegistry(ctx context.Context, manager *ProcessManager, registry *tool.Registry, opts RefreshOptions) (int, error) {
	if manager == nil || registry == nil {
		return 0, nil
	}
	if opts.ReplaceExisting {
		registry.UnregisterPrefix("mcp__")
	}
	registered := 0
	var firstErr error
	for _, status := range manager.Status() {
		if status.State != ServerStateConnected {
			continue
		}
		tools, err := manager.ListToolsForServer(ctx, status.Name)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("list tools for %q: %w", status.Name, err)
			}
			continue
		}
		for _, mcpTool := range tools {
			name := mcpTool.Name()
			if !strings.HasPrefix(name, "mcp__") {
				mcpTool = NamespaceTool(status.Name, mcpTool)
				name = mcpTool.Name()
			}
			if _, exists := registry.Get(name); exists {
				registry.Unregister(name)
			}
			if err := registry.Register(mcpTool); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			registered++
		}
	}
	return registered, firstErr
}

// UnregisterServerTools drops all tools for one server from the registry.
func UnregisterServerTools(registry *tool.Registry, serverName string) int {
	if registry == nil {
		return 0
	}
	return registry.UnregisterPrefix(ToolPrefix(serverName))
}
