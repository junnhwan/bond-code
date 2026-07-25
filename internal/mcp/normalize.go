package mcp

import (
	"strings"
	"unicode"
)

// NormalizeName sanitizes a server or tool segment for mcp__server__tool names
// (Claude Code normalizeNameForMCP). Letters, digits, underscore, and hyphen
// are kept; everything else becomes underscore.
func NormalizeName(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return "unnamed"
	}
	var b strings.Builder
	b.Grow(len(input))
	for _, r := range input {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	out := b.String()
	if out == "" {
		return "unnamed"
	}
	return out
}

// ToolPrefix returns "mcp__{server}__" for a server name.
func ToolPrefix(serverName string) string {
	return "mcp__" + NormalizeName(serverName) + "__"
}

// BuildToolName returns the fully qualified MCP tool name.
func BuildToolName(serverName, toolName string) string {
	return ToolPrefix(serverName) + NormalizeName(toolName)
}

// ParseToolName extracts server and tool from "mcp__server__tool".
func ParseToolName(full string) (server, tool string, ok bool) {
	if !strings.HasPrefix(full, "mcp__") {
		return "", "", false
	}
	rest := strings.TrimPrefix(full, "mcp__")
	server, tool, found := strings.Cut(rest, "__")
	if !found || server == "" || tool == "" {
		return "", "", false
	}
	return server, tool, true
}
