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

	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/config"
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
	for _, want := range []string{"interactive workspace", "config"} {
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
	for _, want := range []string{"Available Commands", "config", "headless"} {
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
