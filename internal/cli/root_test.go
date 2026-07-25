package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/memory"
	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/junnhwan/bond-code/internal/tui"
)

func TestRootCommandHelpShowsMainCommands(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	help := out.String()
	for _, want := range []string{"interactive workspace", "chat", "config"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help output missing %q:\n%s", want, help)
		}
	}
	// Hidden debug commands must not appear in the Available Commands list.
	// Match the cobra command-line form ("\n  <name> ") so a flag whose
	// description happens to contain the word (e.g. --debug "per-session trace")
	// is not mistaken for an unhidden command.
	for _, hidden := range []string{"session", "mcp", "status", "context", "compact", "permissions"} {
		if strings.Contains(help, "\n  "+hidden+" ") {
			t.Fatalf("help output should hide debug command %q:\n%s", hidden, help)
		}
	}
}

func TestRootCommandWithoutArgsStartsTUI(t *testing.T) {
	var tuiStarted bool
	cmd := newRootCommandWithBootstrapAndTUI(func(opts app.Options) (*app.App, error) {
		return &app.App{
			Config: &config.Config{Model: config.ModelConfig{Model: "test-model"}},
			Tools:  tool.NewRegistry(),
			Policy: safety.Policy{RequireConfirmation: true},
		}, nil
	}, func(ctx context.Context, application *app.App) error {
		tuiStarted = true
		return nil
	})
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !tuiStarted {
		t.Fatal("expected root command without args to start the TUI workspace")
	}
}

func TestRootFlagsPropagateDebugAndResume(t *testing.T) {
	var captured app.Options
	cmd := newRootCommandWithBootstrapAndTUI(func(opts app.Options) (*app.App, error) {
		captured = opts
		return &app.App{}, nil
	}, func(ctx context.Context, application *app.App) error {
		return nil
	})
	cmd.SetArgs([]string{"--debug=full", "--resume", "session-xyz"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if captured.Debug != observe.VerboseFull {
		t.Errorf("expected root --debug=full to reach Options as VerboseFull, got %v", captured.Debug)
	}
	if captured.ResumeSessionID != "session-xyz" {
		t.Errorf("expected root --resume to reach Options as session-xyz, got %q", captured.ResumeSessionID)
	}
}

func TestChatHelpHidesOnceFlagFromNormalHelp(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"chat", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	help := out.String()
	if strings.Contains(help, "--once") {
		t.Fatalf("chat help should hide --once developer flag:\n%s", help)
	}
}

func TestRootHelpKeepsCobraHelpCommand(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"help"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	for _, want := range []string{"Available Commands", "chat", "config"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected root help to contain %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Ctrl+P") {
		t.Fatalf("root help should not be replaced by slash /help output:\n%s", got)
	}
}

func TestRootStatusCommandUsesBootstrappedToolCount(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(&namedTool{name: "alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(&namedTool{name: "beta"}); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCommandWithBootstrap(func(opts app.Options) (*app.App, error) {
		return &app.App{
			Config: &config.Config{Model: config.ModelConfig{Model: "test-model"}},
			Tools:  registry,
			Policy: safety.Policy{RequireConfirmation: true},
		}, nil
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"status"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"model: test-model", "tools: 2", "permission mode: confirm"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected status output to contain %q, got %q", want, got)
		}
	}
}

func TestMCPListShowsStatusHeader(t *testing.T) {
	cmd := newMCPCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute mcp list: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "SERVER") && !strings.Contains(got, "no MCP servers") {
		t.Fatalf("expected mcp list output, got %q", got)
	}
}

func TestChatCommandWithFakeLLMPrintsAnswer(t *testing.T) {
	// Isolate BONDCODE_HOME so the run writes session/memory/trust state into
	// a throwaway dir instead of the user's real ~/.bondcode (mirrors the app
	// tests). Keep it in a SEPARATE temp dir from the chdir cwd below so the
	// session data files never land inside the cwd dir — on Windows that avoids
	// a TempDir RemoveAll race where the OS briefly holds a data-file handle.
	t.Setenv("BONDCODE_HOME", t.TempDir())
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"chat", "--fake", "--once", "hello"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "hello from fake llm") {
		t.Fatalf("unexpected chat output %q", out.String())
	}
}

func TestChatCommandWithoutPromptStartsTUI(t *testing.T) {
	var tuiStarted bool
	cmd := newRootCommandWithBootstrapAndTUI(func(opts app.Options) (*app.App, error) {
		return &app.App{
			Agent: agent.NewLoop(
				agent.LoopConfig{},
				llm.NewFakeClient([]llm.Chunk{{Content: "ok", Done: true}}),
				tool.NewRegistry(),
				safety.Policy{},
				nil,
			),
		}, nil
	}, func(ctx context.Context, application *app.App) error {
		tuiStarted = true
		return nil
	})
	cmd.SetArgs([]string{"chat", "--fake"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !tuiStarted {
		t.Fatal("expected chat without a prompt to start the TUI workspace")
	}
}

func TestChatCommandOnceKeepsNonInteractivePath(t *testing.T) {
	var tuiStarted bool
	cmd := newRootCommandWithBootstrapAndTUI(func(opts app.Options) (*app.App, error) {
		return &app.App{
			Agent: agent.NewLoop(
				agent.LoopConfig{},
				llm.NewFakeClient([]llm.Chunk{{Content: "ok", Done: true}}),
				tool.NewRegistry(),
				safety.Policy{},
				nil,
			),
		}, nil
	}, func(ctx context.Context, application *app.App) error {
		tuiStarted = true
		return nil
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"chat", "--fake", "--once", "hello"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if tuiStarted {
		t.Fatal("expected --once to bypass the TUI workspace")
	}
	if !strings.Contains(out.String(), "ok") {
		t.Fatalf("expected non-interactive answer, got %q", out.String())
	}
}

func TestChatCommandOnceExpandsPathMentions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello cli mention"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	fake := llm.NewFakeClient([]llm.Chunk{{Content: "ok", Done: true}})
	cmd := newRootCommandWithBootstrap(func(opts app.Options) (*app.App, error) {
		return &app.App{
			Agent: agent.NewLoop(
				agent.LoopConfig{},
				fake,
				tool.NewRegistry(),
				safety.Policy{},
				nil,
			),
		}, nil
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"chat", "--once", "inspect", "@README.md"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	messages := fake.LastMessages()
	if len(messages) < 2 || !strings.Contains(messages[1].Content, `<file path="README.md">`) || !strings.Contains(messages[1].Content, "hello cli mention") {
		t.Fatalf("expected CLI prompt mention to be expanded, got %#v", messages)
	}
}

func TestChatCommandOnceRunsSlashCommandLocally(t *testing.T) {
	fake := llm.NewFakeClient([]llm.Chunk{{Content: "model should not run", Done: true}})
	registry := tool.NewRegistry()
	cmd := newRootCommandWithBootstrap(func(opts app.Options) (*app.App, error) {
		return &app.App{
			Config: &config.Config{
				Model: config.ModelConfig{Model: "test-model"},
			},
			Tools:     registry,
			SessionID: "session-test",
			Policy:    safety.Policy{RequireConfirmation: true},
			Agent:     agent.NewLoop(agent.LoopConfig{}, fake, registry, safety.Policy{}, nil),
			Confirmer: safety.StaticConfirmer(true),
			LLM:       fake,
			Sessions:  nil,
		}, nil
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"chat", "--once", "/status"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"model: test-model", "tools: 0", "permission mode: confirm"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected slash status output to contain %q, got %q", want, got)
		}
	}
	if len(fake.LastMessages()) != 0 {
		t.Fatalf("expected slash command not to call model, got messages %#v", fake.LastMessages())
	}
}

func TestChatCommandOnceRunsMemorySlashCommandLocally(t *testing.T) {
	fake := llm.NewFakeClient([]llm.Chunk{{Content: "model should not run", Done: true}})
	store, err := memory.NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(memory.MemoryFile{
		Type: memory.TypeFeedback, Name: "CLI memory", Description: "CLI memory command works",
		Body: "CLI memory command works.",
	}); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCommandWithBootstrap(func(opts app.Options) (*app.App, error) {
		return &app.App{
			Config:         &config.Config{Model: config.ModelConfig{Model: "test-model"}},
			Tools:          tool.NewRegistry(),
			SessionID:      "session-test",
			Policy:         safety.Policy{RequireConfirmation: true},
			Agent:          agent.NewLoop(agent.LoopConfig{}, fake, tool.NewRegistry(), safety.Policy{}, nil),
			Confirmer:      safety.StaticConfirmer(true),
			LLM:            fake,
			MemoryStore:    store,
			MemoryMaxChars: 4000,
		}, nil
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"chat", "--once", "/memory"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "CLI memory") {
		t.Fatalf("expected memory slash output, got %q", out.String())
	}
	if len(fake.LastMessages()) != 0 {
		t.Fatalf("expected slash command not to call model, got messages %#v", fake.LastMessages())
	}
}

func TestAutoYesTUIConfirmerCanBeUnwrapped(t *testing.T) {
	confirmer := tui.NewConfirmer()
	wrapped := safety.AutoApproveConfirmer{MaxRisk: "medium", Fallback: confirmer}

	if got := asTUIConfirmer(wrapped); got != confirmer {
		t.Fatalf("expected to unwrap TUI confirmer fallback, got %#v", got)
	}
}

func TestCurrentGitBranchDetectsRepositoryBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "feature/tui")

	branch := currentGitBranch(dir)
	if branch != "feature/tui" {
		t.Fatalf("expected branch feature/tui, got %q", branch)
	}

	nested := filepath.Join(dir, "internal", "tui")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	branch = currentGitBranch(nested)
	if branch != "feature/tui" {
		t.Fatalf("expected nested repo branch feature/tui, got %q", branch)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

type namedTool struct {
	name string
}

func (t *namedTool) Name() string        { return t.name }
func (t *namedTool) Description() string { return t.name }
func (t *namedTool) Schema() any         { return map[string]any{"type": "object"} }
func (t *namedTool) Risk(json.RawMessage) tool.RiskLevel {
	return tool.RiskLow
}
func (t *namedTool) Execute(context.Context, json.RawMessage) (*tool.Result, error) {
	return &tool.Result{ToolName: t.name, Output: "ok", OK: true}, nil
}

func TestChatCommandUsesTerminalConfirmationInput(t *testing.T) {
	var captured app.Options
	cmd := newRootCommandWithBootstrap(func(opts app.Options) (*app.App, error) {
		captured = opts
		return &app.App{
			Agent: agent.NewLoop(
				agent.LoopConfig{},
				llm.NewFakeClient([]llm.Chunk{{Content: "ok", Done: true}}),
				tool.NewRegistry(),
				safety.Policy{},
				nil,
			),
		}, nil
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetArgs([]string{"chat", "write file"})

	_ = cmd.Execute()

	if captured.Confirmer == nil {
		t.Fatal("expected chat command to pass a terminal confirmer")
	}
	approved, err := captured.Confirmer.Confirm(context.Background(), safety.ConfirmationRequest{
		Risk:    "medium",
		Summary: "write file",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !approved {
		t.Fatal("expected terminal confirmer to approve y input")
	}
	if !strings.Contains(out.String(), "Execute write file? [y/N]") {
		t.Fatalf("expected confirmation prompt, got %q", out.String())
	}
}

func TestRootRegistersRestrictedTeammateClient(t *testing.T) {
	root := newRootCommandWithBootstrap(func(app.Options) (*app.App, error) {
		t.Fatal("teammate client must not bootstrap app runtime")
		return nil, nil
	})
	command, _, err := root.Find([]string{"teammate-client"})
	if err != nil {
		t.Fatal(err)
	}
	if command == root || command.Name() != "teammate-client" || !command.Hidden {
		t.Fatalf("command = %#v", command)
	}
	if err := command.Args(command, nil); err == nil {
		t.Fatal("expected required identity arguments")
	}
}
