package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/mcp"
	"github.com/junnhwan/bond-code/internal/tool"

	"github.com/spf13/cobra"
)

func newMCPCommand() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage MCP servers and tools",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.PersistentFlags().StringVar(&configPath, "config", "", "path to config YAML")
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List configured MCP server status",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "no MCP servers connected")
				return nil
			}
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			manager := mcpManagerForConfig(cfg.MCP)
			defer disconnectAllMCP(cmd.Context(), manager)
			connectConfiguredMCP(cmd.Context(), manager, cfg.MCP, false)
			printMCPStatus(cmd, manager.Status())
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:                "connect <name> <command> [args...]",
		Short:              "Connect to an MCP server and list tools",
		Args:               cobra.MinimumNArgs(2),
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := mcp.NewProcessManager()
			if err := manager.Connect(cmd.Context(), args[0], args[1], args[2:]); err != nil {
				return err
			}
			defer manager.Disconnect(cmd.Context(), args[0])
			tools, err := manager.ListToolsForServer(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "connected %s tools=%d\n", args[0], len(tools))
			for _, t := range tools {
				t = mcp.NamespaceTool(args[0], t)
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", t.Name(), t.Description())
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "disconnect <name>",
		Short: "Validate a configured MCP server can be disconnected",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				fmt.Fprintf(cmd.OutOrStdout(), "disconnected %s\n", args[0])
				return nil
			}
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			manager := mcpManagerForConfig(cfg.MCP)
			connectConfiguredMCP(cmd.Context(), manager, cfg.MCP, false)
			if err := manager.Disconnect(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "disconnected %s\n", args[0])
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "reload",
		Short: "Validate configured MCP servers and refresh a temporary tool registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				return fmt.Errorf("mcp reload requires --config")
			}
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			manager := mcpManagerForConfig(cfg.MCP)
			defer disconnectAllMCP(cmd.Context(), manager)
			if err := connectConfiguredMCP(cmd.Context(), manager, cfg.MCP, true); err != nil {
				return err
			}
			registry := tool.NewRegistry()
			count, err := mcp.RefreshRegistry(cmd.Context(), manager, registry, mcp.RefreshOptions{NamespaceTools: cfg.MCP.NamespaceTools})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "reloaded mcp tools=%d\n", count)
			return nil
		},
	})
	return cmd
}

func mcpManagerForConfig(cfg config.MCPConfig) *mcp.ProcessManager {
	manager := mcp.NewProcessManager()
	if cfg.CallTimeoutSeconds > 0 {
		manager.SetCallTimeout(time.Duration(cfg.CallTimeoutSeconds) * time.Second)
	}
	return manager
}

func connectConfiguredMCP(ctx context.Context, manager *mcp.ProcessManager, cfg config.MCPConfig, failFast bool) error {
	for _, server := range cfg.Servers {
		if !server.Enabled {
			continue
		}
		if err := manager.Connect(ctx, server.Name, server.Command, server.Args); err != nil {
			if failFast {
				return err
			}
			continue
		}
		if _, err := manager.ListToolsForServer(ctx, server.Name); err != nil && failFast {
			return err
		}
	}
	return nil
}

func disconnectAllMCP(ctx context.Context, manager *mcp.ProcessManager) {
	for _, status := range manager.Status() {
		_ = manager.Disconnect(ctx, status.Name)
	}
}

func printMCPStatus(cmd *cobra.Command, statuses []mcp.ServerStatus) {
	if len(statuses) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no MCP servers connected")
		return
	}
	fmt.Fprintln(cmd.OutOrStdout(), "SERVER\tSTATE\tTOOLS\tLAST ERROR")
	for _, status := range statuses {
		lastError := strings.ReplaceAll(status.LastError, "\n", " ")
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%d\t%s\n", status.Name, status.State, status.ToolCount, lastError)
	}
}
