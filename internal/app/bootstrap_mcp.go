package app

import (
	"context"
	"fmt"
	"time"

	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/mcp"
	"github.com/junnhwan/bond-code/internal/tool"
)

func injectConfiguredMCPTools(ctx context.Context, registry *tool.Registry, cfg config.MCPConfig) (*mcp.ProcessManager, error) {
	if !cfg.Enabled || !cfg.InjectTools {
		return nil, nil
	}
	manager := mcp.NewProcessManager()
	if cfg.CallTimeoutSeconds > 0 {
		manager.SetCallTimeout(time.Duration(cfg.CallTimeoutSeconds) * time.Second)
	}
	servers := make([]mcp.ServerConfig, 0, len(cfg.Servers))
	for _, server := range cfg.Servers {
		servers = append(servers, mcp.ServerConfig{
			Name:    server.Name,
			Command: server.Command,
			Args:    append([]string(nil), server.Args...),
			Enabled: server.Enabled,
		})
	}
	// Connect with per-server isolation: one bad server does not abort bootstrap.
	_, connectErrs := manager.ConnectAll(ctx, servers)
	_, refreshErr := mcp.RefreshRegistry(ctx, manager, registry, mcp.RefreshOptions{
		NamespaceTools:  true, // always mcp__server__tool (CC)
		ReplaceExisting: true,
	})
	if len(connectErrs) > 0 && managerHasNoConnected(manager) {
		_ = manager.Close()
		return nil, fmt.Errorf("mcp: all servers failed to connect: %v", connectErrs)
	}
	// Partial success is OK — errors stay on manager.Status() for /status and mcp list.
	_ = refreshErr
	_ = connectErrs
	return manager, nil
}

func managerHasNoConnected(manager *mcp.ProcessManager) bool {
	if manager == nil {
		return true
	}
	for _, s := range manager.Status() {
		if s.State == mcp.ServerStateConnected {
			return false
		}
	}
	return true
}
