package backend

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// CommandResult separates a process invocation failure from a command's exit
// status, allowing platform adapters to treat "resource missing" as an
// idempotent lifecycle result without parsing localized error text.
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// CommandExecutor is the only OS-process boundary used by native terminal
// backends. Arguments are always passed as a vector; implementations must not
// invoke a command shell.
type CommandExecutor interface {
	LookPath(string) (string, error)
	Run(context.Context, string, ...string) (CommandResult, error)
}

type osCommandExecutor struct{}

func (osCommandExecutor) LookPath(file string) (string, error) { return exec.LookPath(file) }

func (osCommandExecutor) Run(ctx context.Context, name string, args ...string) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	return result, err
}
