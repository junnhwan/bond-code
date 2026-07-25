package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte(`
model:
  provider: openai-compatible
  base_url: https://api.example.com/v1
  api_key_env: TEST_API_KEY
  model: test-model
  temperature: 0.2
  max_tokens: 2048
session:
  dir: sessions
safety:
  require_confirmation: true
  blocked_commands: ["rm -rf /"]
mcp:
  enabled: false
  servers: []
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Model.Model != "test-model" {
		t.Fatalf("expected model test-model, got %q", cfg.Model.Model)
	}
	if !cfg.Safety.RequireConfirmation {
		t.Fatal("expected confirmation enabled")
	}
}

func TestLoadConfigParsesContextSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte(`
model:
  provider: openai-compatible
session:
  dir: sessions
context:
  enabled: true
  max_tokens: 1234
  micro_compact_keep_recent: 7
  micro_compact_min_chars: 321
  tool_result_budget: 4567
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if !cfg.Context.Enabled {
		t.Fatal("expected context governance to be enabled")
	}
	if cfg.Context.MaxTokens != 1234 {
		t.Fatalf("expected max tokens 1234, got %d", cfg.Context.MaxTokens)
	}
	if cfg.Context.MicroCompactKeepRecent != 7 {
		t.Fatalf("expected keep recent 7, got %d", cfg.Context.MicroCompactKeepRecent)
	}
	if cfg.Context.MicroCompactMinChars != 321 {
		t.Fatalf("expected min chars 321, got %d", cfg.Context.MicroCompactMinChars)
	}
	if cfg.Context.ToolResultBudget != 4567 {
		t.Fatalf("expected tool result budget 4567, got %d", cfg.Context.ToolResultBudget)
	}
}

func TestLoadConfigParsesRuntimeSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte(`
agent:
  max_steps: 11
  max_repeated_tool_calls: 2
  max_tool_calls_per_step: 5
context:
  enabled: true
  max_tokens: 9876
  tool_result_preview_chars: 123
memory:
  enabled: false
  max_chars: 777
planning:
  enabled: true
  inject_tasks: false
subagent:
  enabled: true
  max_children_per_turn: 4
  max_depth: 2
  default_timeout_seconds: 33
mcp:
  enabled: true
  inject_tools: true
  namespace_tools: true
  servers:
    - name: fake
      command: fake-server
      args: ["--debug"]
      enabled: true
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Agent.MaxSteps != 11 || cfg.Agent.MaxRepeatedToolCalls != 2 || cfg.Agent.MaxToolCallsPerStep != 5 {
		t.Fatalf("unexpected agent config: %#v", cfg.Agent)
	}
	if cfg.Context.ToolResultPreviewChars != 123 {
		t.Fatalf("expected tool result preview chars 123, got %d", cfg.Context.ToolResultPreviewChars)
	}
	if cfg.Memory.Enabled {
		t.Fatal("expected memory enabled false to parse")
	}
	if cfg.Planning.Enabled != true || cfg.Planning.InjectTasks != false {
		t.Fatalf("unexpected planning config: %#v", cfg.Planning)
	}
	if cfg.Subagent.MaxChildrenPerTurn != 4 || cfg.Subagent.MaxDepth != 2 || cfg.Subagent.DefaultTimeoutSeconds != 33 {
		t.Fatalf("unexpected subagent config: %#v", cfg.Subagent)
	}
	if !cfg.MCP.Enabled || !cfg.MCP.InjectTools || !cfg.MCP.NamespaceTools {
		t.Fatalf("unexpected mcp config: %#v", cfg.MCP)
	}
	if len(cfg.MCP.Servers) != 1 || cfg.MCP.Servers[0].Name != "fake" || !cfg.MCP.Servers[0].Enabled {
		t.Fatalf("unexpected mcp servers: %#v", cfg.MCP.Servers)
	}
}

func TestLoadConfigParsesModelStreamIdleTimeoutSeconds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte(`
model:
  stream_idle_timeout_seconds: 37
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Model.StreamIdleTimeoutSeconds != 37 {
		t.Fatalf("stream idle timeout seconds = %d, want 37", cfg.Model.StreamIdleTimeoutSeconds)
	}
}
