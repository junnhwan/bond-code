package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/junnhwan/bond-code/internal/tool"
)

type toolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema any            `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

// adapterTool is a single MCP tools/call bound to a process client.
// Name is always the namespaced mcp__server__tool form (Claude Code style).
type adapterTool struct {
	client     *processClient
	serverName string
	info       toolInfo
	name       string
}

func newAdapterTool(client *processClient, serverName string, info toolInfo) tool.Tool {
	return adapterTool{
		client:     client,
		serverName: serverName,
		info:       info,
		name:       BuildToolName(serverName, info.Name),
	}
}

func (t adapterTool) Name() string { return t.name }
func (t adapterTool) Description() string {
	return fmt.Sprintf("MCP tool %s from server %s. External tool risk is derived from MCP annotations when present and defaults to medium.", t.info.Name, t.serverName)
}
func (t adapterTool) Schema() any { return t.info.InputSchema }
func (t adapterTool) Risk(json.RawMessage) tool.RiskLevel {
	return riskFromAnnotations(t.info)
}
func (t adapterTool) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	var args map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, err
		}
	}
	var resp struct {
		Content any  `json:"content"`
		IsError bool `json:"isError"`
	}
	err := t.client.call(ctx, "tools/call", map[string]any{"name": t.info.Name, "arguments": args}, &resp)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(resp.Content)
	out := string(b)
	if resp.IsError {
		return tool.ErrorResult(t.Name(), "mcp tool returned isError", out), nil
	}
	return tool.Success(t.Name(), "mcp tool completed", out), nil
}

// NamespaceTool wraps an already-constructed tool with an mcp__server__ prefix.
func NamespaceTool(serverName string, inner tool.Tool) tool.Tool {
	if _, _, ok := ParseToolName(inner.Name()); ok {
		return inner
	}
	return namespacedTool{
		name:        BuildToolName(serverName, inner.Name()),
		description: fmt.Sprintf("MCP tool %s from server %s. External tool risk is derived from MCP annotations when present and defaults to medium.", inner.Name(), serverName),
		inner:       inner,
	}
}

type namespacedTool struct {
	name        string
	description string
	inner       tool.Tool
}

func (t namespacedTool) Name() string        { return t.name }
func (t namespacedTool) Description() string { return t.description }
func (t namespacedTool) Schema() any         { return t.inner.Schema() }
func (t namespacedTool) Risk(input json.RawMessage) tool.RiskLevel {
	return t.inner.Risk(input)
}
func (t namespacedTool) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	result, err := t.inner.Execute(ctx, input)
	if err != nil {
		return nil, err
	}
	result = tool.NormalizeResult(result, t.name)
	result.ToolName = t.name
	return result, nil
}
