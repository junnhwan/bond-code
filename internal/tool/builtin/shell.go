package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
)

type RunCommandInput struct {
	Command    string `json:"command"`
	Dir        string `json:"dir"`
	Timeout    int    `json:"timeout_seconds"`
	Background bool   `json:"background"`
}

type runCommandTool struct{}

func NewRunCommandTool() tool.Tool { return runCommandTool{} }

func (runCommandTool) Name() string { return tool.RunCommand }
func (runCommandTool) Description() string {
	return "Run a local shell command for git, tests, builds, formatters, package managers, and other process work. " +
		"Do not use for reading local files; use read_file, list_dir, or search_text instead. " +
		"Avoid shell pipelines unless explicitly requested. May mutate the workspace depending on command. Output is combined stdout/stderr or an error envelope."
}
func (runCommandTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command":         map[string]any{"type": "string"},
			"dir":             map[string]any{"type": "string"},
			"timeout_seconds": map[string]any{"type": "integer"},
		},
		"required": []string{"command"},
	}
}
func (runCommandTool) Risk(raw json.RawMessage) tool.RiskLevel {
	var input RunCommandInput
	_ = json.Unmarshal(raw, &input)
	return classifyCommandRisk(input.Command)
}
func (t runCommandTool) Execute(ctx context.Context, raw json.RawMessage) (*tool.Result, error) {
	var input RunCommandInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	result, err := RunCommand(ctx, input)
	if err != nil {
		return nil, err
	}
	result.ToolName = t.Name()
	tool.NormalizeResult(result, t.Name())
	return result, nil
}

func RunCommand(ctx context.Context, input RunCommandInput) (*tool.Result, error) {
	if input.Command == "" {
		return nil, fmt.Errorf("command is required")
	}
	if path, ok := localFileReadFallbackPath(input.Command); ok {
		b, err := os.ReadFile(path)
		if err != nil {
			return tool.ErrorResult("", "file read fallback failed", err.Error()), nil
		}
		return tool.Success("", "read local file without shell", string(b)), nil
	}
	if reason := safety.DangerousCommandReason(input.Command); reason != "" {
		return tool.Blocked("", "command blocked", reason), nil
	}
	timeout := input.Timeout
	if timeout <= 0 {
		timeout = 120
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, resolveWindowsShell(), "-NoProfile", "-Command", input.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", input.Command)
	}
	if input.Dir != "" {
		cmd.Dir = input.Dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := strings.TrimSpace(stdout.String() + stderr.String())
	if ctx.Err() == context.DeadlineExceeded {
		result := tool.ErrorResult("", "command timed out", "command timed out")
		result.Output = output
		return result, nil
	}
	if err != nil {
		result := tool.ErrorResult("", "command failed", err.Error())
		result.Output = output
		return result, nil
	}
	return tool.Success("", "command completed", output), nil
}

// windowsShell caches the resolved Windows shell program for the lifetime of
// the process. Which shells are installed does not change mid-session, so the
// LookPath is run exactly once.
var windowsShell struct {
	once sync.Once
	exe  string
}

// resolveWindowsShell picks the shell used to execute agent commands on
// Windows: PowerShell 7 (pwsh) when it is on PATH — so commands run in the
// same shell the user is likely driving from and PS7-only syntax works —
// falling back to Windows PowerShell 5.1 (powershell). Both are invoked with
// -NoProfile (see RunCommand) so user-profile aliases/functions cannot change
// what a command does.
func resolveWindowsShell() string {
	windowsShell.once.Do(func() {
		if _, err := exec.LookPath("pwsh"); err == nil {
			windowsShell.exe = "pwsh"
			return
		}
		windowsShell.exe = "powershell"
	})
	return windowsShell.exe
}

func classifyCommandRisk(command string) tool.RiskLevel {
	cmd := strings.ToLower(strings.TrimSpace(command))
	if cmd == "" {
		return tool.RiskLow
	}
	if _, ok := localFileReadFallbackPath(command); ok {
		return tool.RiskLow
	}
	// High risk = commands that delete or push outward. Pipes (`|`) and plain
	// downloads (curl/wget) used to force RiskHigh too, but pipes are benign on
	// their own and the genuinely dangerous forms (`curl | sh`, `rm -rf /`,
	// `sudo …`) are hard-Blocked by command_guard before this classification
	// runs — so they now fall through to the default allow instead of
	// prompting on every call. rm/del/rmdir/git push stay high (delete + push).
	highPrefixes := []string{"rm ", "rm\t", "del ", "rmdir ", "git push"}
	for _, prefix := range highPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return tool.RiskHigh
		}
	}
	mediumPrefixes := []string{"go mod tidy", "git checkout", "git add"}
	for _, prefix := range mediumPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return tool.RiskMedium
		}
	}
	lowPrefixes := []string{"pwd", "ls", "dir", "go test", "go build", "gofmt", "git status", "git diff", "go version", "echo", "write-output"}
	for _, prefix := range lowPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return tool.RiskLow
		}
	}
	return tool.RiskMedium
}

func localFileReadFallbackPath(command string) (string, bool) {
	lower := strings.ToLower(command)
	if !strings.Contains(lower, "get-content") && !strings.Contains(lower, "readalltext") {
		return "", false
	}
	path := firstSingleQuotedValue(command)
	if path == "" || strings.Contains(path, "\n") || strings.Contains(path, "\r") {
		return "", false
	}
	return path, true
}

func firstSingleQuotedValue(input string) string {
	start := strings.IndexByte(input, '\'')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(input[start+1:], '\'')
	if end < 0 {
		return ""
	}
	return input[start+1 : start+1+end]
}
