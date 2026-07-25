package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/agent"
	execution "github.com/junnhwan/bond-code/internal/collaboration/backend"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/session"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/todo"
	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/junnhwan/bond-code/internal/tool/builtin"
	"github.com/junnhwan/bond-code/internal/undo"
)

func TestBootstrapRegistersBuiltInTools(t *testing.T) {
	t.Setenv("BONDCODE_HOME", t.TempDir())
	application, err := Bootstrap(Options{UseFakeLLM: true})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"agent_task", "ask_user", "edit_file",
		"list_dir", "mailbox_broadcast", "mailbox_inbox", "mailbox_read", "mailbox_send", "memory_save", "memory_search", "read_file",
		"run_command", "search_text", "skill", "task", "task_backend", "task_input", "task_list", "task_output", "task_resume", "task_stop", "team_add_member", "team_assign", "team_create", "team_delete", "team_list", "team_shutdown", "todo_read", "todo_write", "write_file",
	}
	if got := application.Tools.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("default tool registration = %#v, want %#v", got, want)
	}
}

func TestCoreBuiltinToolsExposeStableRegistrationSet(t *testing.T) {
	want := []string{
		"edit_file", "list_dir",
		"read_file", "run_command", "search_text", "write_file",
	}
	core, err := coreBuiltinTools(builtin.NewObservationStore(), undo.NewStore(4))
	if err != nil {
		t.Fatal(err)
	}
	if got := registeredToolNames(core); !reflect.DeepEqual(got, want) {
		t.Fatalf("core builtins = %#v, want %#v", got, want)
	}
}

func registeredToolNames(tools []tool.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, candidate := range tools {
		names = append(names, candidate.Name())
	}
	sort.Strings(names)
	return names
}

func TestBootstrapAppliesDefaultMemoryConfigWhenConfigOmitsMemoryAndContext(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfig(t, dataDir)
	app, err := Bootstrap(Options{ConfigPath: configPath, UseFakeLLM: true})
	if err != nil {
		t.Fatal(err)
	}

	if app.MemoryStore == nil {
		t.Fatal("expected Bootstrap to expose a memory store")
	}
	if app.MemoryMaxChars != 4000 {
		t.Fatalf("expected default memory max chars 4000, got %d", app.MemoryMaxChars)
	}
	if !app.Config.Memory.Enabled {
		t.Fatal("expected memory to default enabled")
	}
	if app.Config.Memory.MaxRelevant != 5 {
		t.Fatalf("expected default max_relevant 5, got %d", app.Config.Memory.MaxRelevant)
	}
}

func TestBootstrapWiresContextManager(t *testing.T) {
	t.Setenv("BONDCODE_HOME", t.TempDir())
	app, err := Bootstrap(Options{UseFakeLLM: true})
	if err != nil {
		t.Fatal(err)
	}

	if app.ContextManager == nil {
		t.Fatal("expected Bootstrap to create a context manager")
	}
	if app.MaxContextTokens <= 0 {
		t.Fatalf("expected positive max context tokens, got %d", app.MaxContextTokens)
	}
	if app.ContextSummary == nil {
		t.Fatal("expected Bootstrap to create a context summary store")
	}
}

func TestBootstrapWiresSubagentManagerAndDefaults(t *testing.T) {
	t.Setenv("BONDCODE_HOME", t.TempDir())
	app, err := Bootstrap(Options{UseFakeLLM: true})
	if err != nil {
		t.Fatal(err)
	}

	if app.SubagentManager == nil {
		t.Fatal("expected Bootstrap to expose a subagent manager")
	}
	if !app.Config.Subagent.Enabled {
		t.Fatal("expected subagent support to default enabled")
	}
	if app.Config.Subagent.MaxChildrenPerTurn != 3 {
		t.Fatalf("expected max children per turn default 3, got %d", app.Config.Subagent.MaxChildrenPerTurn)
	}
	if app.Config.Subagent.MaxDepth != 1 {
		t.Fatalf("expected max depth default 1, got %d", app.Config.Subagent.MaxDepth)
	}
	if app.Config.Subagent.DefaultTimeoutSeconds != 600 {
		t.Fatalf("expected default timeout 600s, got %d", app.Config.Subagent.DefaultTimeoutSeconds)
	}
}

func TestBootstrapWiresSkillsIntoToolsStatusAndPrompt(t *testing.T) {
	dataDir := t.TempDir()
	skillsRoot := filepath.Join(dataDir, "skills")
	writeBootstrapSkill(t, skillsRoot, "debugging", "debug systematically", "# Debugging\n\nReproduce first.")
	configPath := writeBootstrapTestConfigWithSkills(t, dataDir, skillsRoot)
	client := llm.NewFakeClient([]llm.Chunk{{Content: "skills available", Done: true}})

	application, err := Bootstrap(Options{
		ConfigPath: configPath,
		Client:     client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := application.Tools.Get("skill"); !ok {
		t.Fatalf("expected skill tool to be registered, got %#v", application.Tools.Names())
	}
	if _, ok := application.Tools.Get("skill_list"); ok {
		t.Fatal("skill_list must be removed")
	}
	if _, ok := application.Tools.Get("skill_load"); ok {
		t.Fatal("skill_load must be removed")
	}
	snap := application.StatusSnapshot()
	if !snap.Skills.Enabled || snap.Skills.Count != 1 {
		t.Fatalf("expected one enabled skill in status, got %#v", snap.Skills)
	}

	if _, err := application.Chat(context.Background(), "which skills are available?"); err != nil {
		t.Fatal(err)
	}
	messages := client.LastMessages()
	// Skill listing is in the user-turn system-reminder, not the system prompt.
	foundListing := false
	for _, msg := range messages {
		if strings.Contains(msg.Content, "## Available Skills") && strings.Contains(msg.Content, "debugging") {
			foundListing = true
		}
		if strings.Contains(msg.Content, "Reproduce first.") {
			t.Fatalf("expected full skill body to stay out of initial prompt, got:\n%s", msg.Content)
		}
	}
	if !foundListing {
		t.Fatalf("expected skill listing in dynamic reminder, got %#v", messages)
	}
}

func TestBootstrapHonorsContextConfig(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfigWithContext(t, dataDir, 1234)
	app, err := Bootstrap(Options{ConfigPath: configPath, UseFakeLLM: true})
	if err != nil {
		t.Fatal(err)
	}

	if app.ContextManager == nil {
		t.Fatal("expected configured context manager")
	}
	if app.MaxContextTokens != 1234 {
		t.Fatalf("expected max context tokens from config, got %d", app.MaxContextTokens)
	}
}

// TestBootstrapEnablesContextFromPartialSection reproduces the config that hid
// the header ctx %: a context section with max_tokens but no enabled: true.
// applyConfigDefaults must turn the governor on, otherwise EventContextUpdated
// never fires and the ctx segment stays blank.
func TestBootstrapEnablesContextFromPartialSection(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "model:\n" +
		"  provider: openai-compatible\n" +
		"  base_url: https://example.invalid\n" +
		"  api_key_env: BONDCODE_TEST_API_KEY\n" +
		"  model: test-model\n" +
		"session:\n" +
		"  dir: " + filepath.ToSlash(dataDir) + "\n" +
		"context:\n" +
		"  max_tokens: 1000000\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := Bootstrap(Options{ConfigPath: path, UseFakeLLM: true})
	if err != nil {
		t.Fatal(err)
	}
	if app.ContextManager == nil {
		t.Fatal("expected context manager enabled from a partial context section")
	}
	if app.MaxContextTokens != 1000000 {
		t.Fatalf("expected max tokens preserved, got %d", app.MaxContextTokens)
	}
}

func TestBootstrapHonorsExplicitlyDisabledContext(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "model:\n" +
		"  provider: openai-compatible\n" +
		"  base_url: https://example.invalid\n" +
		"  api_key_env: BONDCODE_TEST_API_KEY\n" +
		"  model: test-model\n" +
		"session:\n" +
		"  dir: " + filepath.ToSlash(dataDir) + "\n" +
		"context:\n" +
		"  enabled: false\n" +
		"  max_tokens: 32000\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := Bootstrap(Options{ConfigPath: path, UseFakeLLM: true})
	if err != nil {
		t.Fatal(err)
	}
	if app.ContextManager != nil {
		t.Fatal("expected explicitly disabled context manager to remain disabled")
	}
	if app.Config.Context.Enabled {
		t.Fatal("expected context.enabled=false to remain false")
	}
}

func TestBootstrapHonorsAgentLoopConfig(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfigWithAgent(t, dataDir, 1)
	readPath := filepath.Join(t.TempDir(), "agent-loop-input.txt")
	if err := os.WriteFile(readPath, []byte("loop config fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "call-read", Name: "read_file", Arguments: fmt.Sprintf(`{"path":%q}`, readPath)}, Done: true}},
		{{Content: "would finish on step two", Done: true}},
	})
	application, err := Bootstrap(Options{
		ConfigPath: configPath,
		Client:     client,
		AutoYes:    true,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = application.Chat(context.Background(), "read with one max step")
	if err == nil || !strings.Contains(err.Error(), "agent stopped after max steps") {
		t.Fatalf("expected configured max_steps=1 to stop before second model call, got %v", err)
	}
}

func TestBootstrapHonorsPlanningInjectTasksConfig(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfigWithPlanning(t, dataDir, false)
	client := llm.NewFakeClient([]llm.Chunk{{Content: "tasks hidden", Done: true}})
	application, err := Bootstrap(Options{
		ConfigPath: configPath,
		Client:     client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.TaskStore.ReplaceAll([]todo.Task{
		{ID: "1", Subject: "Hidden planning task", Status: todo.TaskStatusInProgress},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := application.Chat(context.Background(), "should tasks be injected?"); err != nil {
		t.Fatal(err)
	}

	messages := client.LastMessages()
	if len(messages) > 0 && strings.Contains(messages[0].Content, "Hidden planning task") {
		t.Fatalf("expected planning.inject_tasks=false to omit task prompt context, got %#v", messages[0])
	}
}

func TestBootstrapInjectsNamespacedMCPToolsWhenConfigured(t *testing.T) {
	dataDir := t.TempDir()
	server := buildBootstrapFakeMCPServer(t)
	configPath := writeBootstrapTestConfigWithMCP(t, dataDir, server)
	application, err := Bootstrap(Options{ConfigPath: configPath, UseFakeLLM: true})
	if err != nil {
		t.Fatal(err)
	}
	if application.MCPManager != nil {
		t.Cleanup(func() { _ = application.MCPManager.Disconnect(context.Background(), "fake") })
	}

	if _, ok := application.Tools.Get("mcp__fake__fake_echo"); !ok {
		t.Fatalf("expected namespaced MCP tool to be registered, got %#v", application.Tools.Names())
	}
}

func TestBootstrapRuntimeToolsCanBeCalledByAgent(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfig(t, dataDir)
	client := &bootstrapToolClient{}
	application, err := Bootstrap(Options{
		ConfigPath: configPath,
		Client:     client,
		AutoYes:    true,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := application.Chat(context.Background(), "use runtime tools")
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalAnswer != "runtime tools done" {
		t.Fatalf("unexpected final answer %q", result.FinalAnswer)
	}

	// memory_save writes a topic .md + MEMORY.md index (CC memdir), not items.jsonl.
	topicFiles, err := filepath.Glob(filepath.Join(dataDir, "memory", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	foundBody := false
	for _, p := range topicFiles {
		data, _ := os.ReadFile(p)
		if strings.Contains(string(data), "Remember runtime tools") {
			foundBody = true
			break
		}
	}
	if !foundBody {
		t.Fatalf("expected memory_save to persist a topic file, dir entries: %#v", topicFiles)
	}
	todoPath := filepath.Join(application.TaskStore.BaseDir(), "todos.json")
	if _, err := os.Stat(todoPath); err != nil {
		t.Fatalf("expected todo_write to persist todos.json: %v", err)
	}

	for _, name := range []string{"memory_save", "todo_write", "task"} {
		if !traceHasToolResult(result.Trace, name) {
			t.Fatalf("expected trace to include %s tool result, got %#v", name, result.Trace.Events)
		}
	}
}

func TestBootstrapUsesProvidedConfirmerByDefault(t *testing.T) {
	t.Setenv("BONDCODE_HOME", t.TempDir())
	confirmer := &recordingConfirmer{approve: true}
	app, err := Bootstrap(Options{UseFakeLLM: true, Confirmer: confirmer})
	if err != nil {
		t.Fatal(err)
	}

	approved, err := app.Confirmer.Confirm(context.Background(), safety.ConfirmationRequest{Risk: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if !approved {
		t.Fatal("expected provided confirmer approval")
	}
	if confirmer.calls != 1 {
		t.Fatalf("expected provided confirmer to be called once, got %d", confirmer.calls)
	}
}

func TestBootstrapAutoYesApprovesOnlyThroughMediumRisk(t *testing.T) {
	t.Setenv("BONDCODE_HOME", t.TempDir())
	app, err := Bootstrap(Options{UseFakeLLM: true, AutoYes: true})
	if err != nil {
		t.Fatal(err)
	}

	approvedMedium, err := app.Confirmer.Confirm(context.Background(), safety.ConfirmationRequest{Risk: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	if !approvedMedium {
		t.Fatal("expected auto yes to approve medium risk")
	}
	approvedHigh, err := app.Confirmer.Confirm(context.Background(), safety.ConfirmationRequest{Risk: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if approvedHigh {
		t.Fatal("expected auto yes not to approve high risk")
	}
}

type recordingConfirmer struct {
	approve bool
	calls   int
}

func (r *recordingConfirmer) Confirm(ctx context.Context, req safety.ConfirmationRequest) (bool, error) {
	r.calls++
	return r.approve, nil
}

type bootstrapToolClient struct{}

func (c *bootstrapToolClient) Stream(ctx context.Context, messages []llm.Message, tools []llm.ToolSpec) (<-chan llm.Chunk, <-chan error) {
	chunks := make(chan llm.Chunk, 3)
	errs := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(errs)
		if isSubagentTask(messages) {
			chunks <- llm.Chunk{Content: "subagent complete", Done: true}
			errs <- nil
			return
		}
		if hasToolResultMessage(messages) {
			chunks <- llm.Chunk{Content: "runtime tools done", Done: true}
			errs <- nil
			return
		}
		chunks <- llm.Chunk{ToolCall: &llm.ToolCall{ID: "call-memory", Name: "memory_save", Arguments: `{"type":"project","name":"Runtime wiring","description":"Bootstrap wires runtime tools","content":"Remember runtime tools are wired through Bootstrap"}`}}
		chunks <- llm.Chunk{ToolCall: &llm.ToolCall{ID: "call-todo", Name: "todo_write", Arguments: `{"items":[{"id":"1","subject":"Wire runtime tools","status":"in_progress"}]}`}}
		chunks <- llm.Chunk{ToolCall: &llm.ToolCall{ID: "call-task", Name: "task", Arguments: `{"description":"background review","prompt":"subagent task","subagent_type":"research"}`}, Done: true}
		errs <- nil
	}()
	return chunks, errs
}

func isSubagentTask(messages []llm.Message) bool {
	for _, message := range messages {
		if message.Role == llm.RoleUser && strings.Contains(message.Content, "subagent task") {
			return true
		}
	}
	return false
}

func hasToolResultMessage(messages []llm.Message) bool {
	for _, message := range messages {
		if message.Role == llm.RoleTool {
			return true
		}
	}
	return false
}

func traceHasToolResult(trace agent.Trace, toolName string) bool {
	for _, event := range trace.Events {
		if event.Type == agent.EventToolResult && event.ToolName == toolName && event.Error == "" {
			return true
		}
	}
	return false
}

func writeBootstrapTestConfig(t *testing.T, dataDir string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "model:\n" +
		"  provider: openai-compatible\n" +
		"  base_url: https://example.invalid\n" +
		"  api_key_env: BONDCODE_TEST_API_KEY\n" +
		"  model: test-model\n" +
		"  temperature: 0.2\n" +
		"  max_tokens: 1024\n" +
		"session:\n" +
		"  dir: " + filepath.ToSlash(dataDir) + "\n" +
		"safety:\n" +
		"  require_confirmation: true\n" +
		"  blocked_commands: [\"rm -rf /\"]\n" +
		"mcp:\n" +
		"  enabled: false\n" +
		"  servers: []\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeBootstrapTestConfigWithCollaboration(t *testing.T, dataDir string, enabled bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	flag := "false"
	if enabled {
		flag = "true"
	}
	content := "model:\n" +
		"  provider: openai-compatible\n" +
		"  base_url: https://example.invalid\n" +
		"  api_key_env: BONDCODE_TEST_API_KEY\n" +
		"  model: test-model\n" +
		"session:\n" +
		"  dir: " + filepath.ToSlash(dataDir) + "\n" +
		"safety:\n" +
		"  require_confirmation: true\n" +
		"  blocked_commands: [\"rm -rf /\"]\n" +
		"collaboration:\n" +
		"  enabled: " + flag + "\n" +
		"mcp:\n" +
		"  enabled: false\n" +
		"  servers: []\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeBootstrapTestConfigWithContext(t *testing.T, dataDir string, maxTokens int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "model:\n" +
		"  provider: openai-compatible\n" +
		"  base_url: https://example.invalid\n" +
		"  api_key_env: BONDCODE_TEST_API_KEY\n" +
		"  model: test-model\n" +
		"  temperature: 0.2\n" +
		"  max_tokens: 1024\n" +
		"session:\n" +
		"  dir: " + filepath.ToSlash(dataDir) + "\n" +
		"safety:\n" +
		"  require_confirmation: true\n" +
		"  blocked_commands: [\"rm -rf /\"]\n" +
		"context:\n" +
		"  enabled: true\n" +
		"  max_tokens: " + strconv.Itoa(maxTokens) + "\n" +
		"  micro_compact_keep_recent: 10\n" +
		"  micro_compact_min_chars: 500\n" +
		"  tool_result_budget: 8000\n" +
		"mcp:\n" +
		"  enabled: false\n" +
		"  servers: []\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeBootstrapTestConfigWithAgent(t *testing.T, dataDir string, maxSteps int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "model:\n" +
		"  provider: openai-compatible\n" +
		"  base_url: https://example.invalid\n" +
		"  api_key_env: BONDCODE_TEST_API_KEY\n" +
		"  model: test-model\n" +
		"session:\n" +
		"  dir: " + filepath.ToSlash(dataDir) + "\n" +
		"safety:\n" +
		"  require_confirmation: false\n" +
		"agent:\n" +
		"  max_steps: " + strconv.Itoa(maxSteps) + "\n" +
		"  max_repeated_tool_calls: 3\n" +
		"  max_tool_calls_per_step: 8\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeBootstrapTestConfigWithPlanning(t *testing.T, dataDir string, injectTasks bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "model:\n" +
		"  provider: openai-compatible\n" +
		"  base_url: https://example.invalid\n" +
		"  api_key_env: BONDCODE_TEST_API_KEY\n" +
		"  model: test-model\n" +
		"session:\n" +
		"  dir: " + filepath.ToSlash(dataDir) + "\n" +
		"planning:\n" +
		"  enabled: true\n" +
		"  inject_tasks: " + strconv.FormatBool(injectTasks) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeBootstrapTestConfigWithSkills(t *testing.T, dataDir string, skillsRoot string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "model:\n" +
		"  provider: openai-compatible\n" +
		"  base_url: https://example.invalid\n" +
		"  api_key_env: BONDCODE_TEST_API_KEY\n" +
		"  model: test-model\n" +
		"session:\n" +
		"  dir: " + filepath.ToSlash(dataDir) + "\n" +
		"skills:\n" +
		"  enabled: true\n" +
		"  root: " + filepath.ToSlash(skillsRoot) + "\n" +
		"  max_chars: 4000\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeBootstrapSkill(t *testing.T, root, name, description, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeBootstrapTestConfigWithMCP(t *testing.T, dataDir string, server string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "model:\n" +
		"  provider: openai-compatible\n" +
		"  base_url: https://example.invalid\n" +
		"  api_key_env: BONDCODE_TEST_API_KEY\n" +
		"  model: test-model\n" +
		"session:\n" +
		"  dir: " + filepath.ToSlash(dataDir) + "\n" +
		"mcp:\n" +
		"  enabled: true\n" +
		"  inject_tools: true\n" +
		"  namespace_tools: true\n" +
		"  servers:\n" +
		"    - name: fake\n" +
		"      command: " + strconv.Quote(server) + "\n" +
		"      enabled: true\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func buildBootstrapFakeMCPServer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	code := `package main
import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)
func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &req)
		id := req["id"]
		method, _ := req["method"].(string)
		result := map[string]any{}
		if method == "tools/list" {
			result = map[string]any{"tools":[]any{map[string]any{"name":"fake_echo","description":"echo","inputSchema":map[string]any{"type":"object"}}}}
		}
		resp, _ := json.Marshal(map[string]any{"jsonrpc":"2.0","id":id,"result":result})
		fmt.Println(string(resp))
	}
}
`
	if err := os.WriteFile(source, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fake-mcp")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, source)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fake mcp: %v\n%s", err, string(out))
	}
	return bin
}

// TestBootstrapDebugLoggerWritesTrace verifies the opt-in debug trace closes
// the loop end to end: Options.Debug -> Bootstrap wires a per-session logger ->
// the agent loop records llm_req/llm_resp -> the file appears on disk.
func TestBootstrapDebugLoggerWritesTrace(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfig(t, dataDir)
	client := llm.NewFakeClient([]llm.Chunk{{Content: "debug trace answer", Done: true}})
	application, err := Bootstrap(Options{ConfigPath: configPath, Client: client, Debug: observe.VerboseDefault})
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	if _, err := application.Chat(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dataDir, application.SessionID+".debug.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected debug.jsonl after a --debug run: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `"llm_req"`) || !strings.Contains(body, `"llm_resp"`) {
		t.Fatalf("expected llm_req/llm_resp records in debug trace:\n%s", body)
	}
}

// TestBootstrapNoDebugWritesNoTrace ensures the trace stays opt-in: without
// Options.Debug, no debug.jsonl is created.
func TestBootstrapNoDebugWritesNoTrace(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfig(t, dataDir)
	client := llm.NewFakeClient([]llm.Chunk{{Content: "no trace", Done: true}})
	application, err := Bootstrap(Options{ConfigPath: configPath, Client: client})
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	if _, err := application.Chat(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, application.SessionID+".debug.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("expected no debug trace when Debug is off, got err=%v", err)
	}
}

// A hot /resume must move the debug trace with the active session. The trace is
// part of the session's diagnostic transcript, so writing post-switch records
// into the bootstrap session makes the two session artifacts disagree.
func TestBootstrapDebugLoggerFollowsSwitchSession(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfig(t, dataDir)
	client := llmfake.New([][]llm.Chunk{
		{{Content: "answer in initial session", Done: true}},
		{{Content: "answer in resumed session", Done: true}},
	})
	application, err := Bootstrap(Options{ConfigPath: configPath, Client: client, Debug: observe.VerboseDefault})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := application.Chat(context.Background(), "prompt-before-resume-marker"); err != nil {
		t.Fatal(err)
	}
	initialID := application.SessionID
	targetID := "session-resume-debug-target"
	if err := application.Sessions.Append(session.Event{
		SessionID: targetID,
		Type:      "message",
		Message:   &session.Message{Role: session.RoleUser, Content: "seed target"},
	}); err != nil {
		t.Fatalf("seed target session: %v", err)
	}
	if err := application.SwitchSession(targetID); err != nil {
		t.Fatalf("switch session: %v", err)
	}
	if _, err := application.Chat(context.Background(), "prompt-after-resume-marker"); err != nil {
		t.Fatal(err)
	}
	if err := application.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	initialTrace, err := os.ReadFile(filepath.Join(dataDir, initialID+".debug.jsonl"))
	if err != nil {
		t.Fatalf("read initial trace: %v", err)
	}
	targetTrace, err := os.ReadFile(filepath.Join(dataDir, targetID+".debug.jsonl"))
	if err != nil {
		t.Fatalf("read resumed trace: %v", err)
	}
	if strings.Contains(string(initialTrace), "prompt-after-resume-marker") {
		t.Fatalf("post-resume trace leaked into initial session:\n%s", initialTrace)
	}
	if !strings.Contains(string(targetTrace), "prompt-after-resume-marker") {
		t.Fatalf("resumed session did not receive post-resume trace:\n%s", targetTrace)
	}
}

func TestRegisterBuiltinToolsSharesFileObservationStore(t *testing.T) {
	t.Setenv("BONDCODE_HOME", t.TempDir())
	application, err := Bootstrap(Options{UseFakeLLM: true})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	readTool, ok := application.Tools.Get(tool.ReadFile)
	if !ok {
		t.Fatal("read_file missing")
	}
	writeTool, ok := application.Tools.Get(tool.WriteFile)
	if !ok {
		t.Fatal("write_file missing")
	}
	readRaw := json.RawMessage(fmt.Sprintf(`{"path":%q}`, path))
	if _, err := readTool.Execute(context.Background(), readRaw); err != nil {
		t.Fatal(err)
	}
	writeRaw := json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"after"}`, path))
	if _, err := writeTool.Execute(context.Background(), writeRaw); err != nil {
		t.Fatalf("shared observation rejected write: %v", err)
	}
}

func TestBootstrapWiresCollaborationByDefault(t *testing.T) {
	t.Setenv("BONDCODE_HOME", t.TempDir())
	application, err := Bootstrap(Options{UseFakeLLM: true})
	if err != nil {
		t.Fatal(err)
	}
	if !application.Config.Collaboration.IsEnabled() {
		t.Fatal("expected collaboration enabled by default")
	}
	if application.AgentTasks == nil {
		t.Fatal("expected unified agent task service")
	}
	if application.Collaboration == nil {
		t.Fatal("expected collaboration store")
	}
	for _, name := range []string{"agent_task", "task_output", "task_list", "task_stop", "task_resume", "task_input", "task_backend", "team_create", "mailbox_send"} {
		if _, ok := application.Tools.Get(name); !ok {
			t.Fatalf("missing collaboration tool %s", name)
		}
	}
}

func TestBootstrapOmitsCollaborationWhenDisabled(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfigWithCollaboration(t, dataDir, false)
	application, err := Bootstrap(Options{ConfigPath: configPath, UseFakeLLM: true})
	if err != nil {
		t.Fatal(err)
	}
	if application.Config.Collaboration.IsEnabled() {
		t.Fatal("expected collaboration disabled when collaboration.enabled: false")
	}
	if application.AgentTasks != nil || application.Collaboration != nil || application.ExecutionBackends != nil || application.BackendSupervisor != nil {
		t.Fatal("expected collaboration runtime to stay unwired when disabled")
	}
	for _, name := range []string{"agent_task", "task_output", "task_list", "task_stop", "task_resume", "task_input", "task_backend", "team_create", "mailbox_send"} {
		if _, ok := application.Tools.Get(name); ok {
			t.Fatalf("collaboration tool %s should not register when disabled", name)
		}
	}
}

func TestBootstrapRegistersSelectableExecutionBackendsByDefault(t *testing.T) {
	t.Setenv("BONDCODE_HOME", t.TempDir())
	application, err := Bootstrap(Options{UseFakeLLM: true})
	if err != nil {
		t.Fatal(err)
	}
	if application.ExecutionBackends == nil {
		t.Fatal("execution backend registry is nil")
	}
	if application.BackendSupervisor == nil {
		t.Fatal("backend IPC supervisor is nil")
	}
	selected, detection, err := application.ExecutionBackends.Resolve(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if selected.Kind() != execution.KindInProcess || detection.Kind != execution.KindInProcess || !detection.Available {
		t.Fatalf("default backend = %v, detection = %#v", selected.Kind(), detection)
	}
}
