package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const tmuxOwnerOption = "@bondcode_owner"

// Tmux is a native tmux adapter. It never invokes a shell and launches only
// the fixed teammate-client subcommand.
type Tmux struct {
	config externalConfig
}

func NewTmux(executor CommandExecutor, options ...ExternalOption) *Tmux {
	return &Tmux{config: newExternalConfig(executor, options)}
}

func (b *Tmux) Kind() Kind { return KindTmux }

func (b *Tmux) Detect(ctx context.Context) (Detection, error) {
	if b == nil {
		return Detection{}, fmt.Errorf("tmux backend is not configured")
	}
	if !tmuxPlatformSupported(b.config.goos) {
		return Detection{}, &UnsupportedError{
			Backend:  KindTmux,
			Platform: b.config.goos,
			Reason:   "native tmux is supported only on Unix hosts; WSL is not selected from PATH",
			Action:   "select the in-process backend or run BondCode on a supported native Unix host",
		}
	}
	if b.config.wsl {
		return Detection{}, &UnsupportedError{
			Backend:  KindTmux,
			Platform: "WSL",
			Reason:   "WSL tmux is deferred until a Windows/Linux path, executable, worktree, and IPC mapping design is implemented",
			Action:   "select the in-process backend; do not infer WSL tmux support from PATH",
		}
	}
	executable, err := b.config.executor.LookPath("tmux")
	if err != nil {
		return Detection{}, &UnsupportedError{
			Backend:  KindTmux,
			Platform: b.config.goos,
			Reason:   "tmux was not found on PATH",
			Action:   "install tmux and retry, or explicitly select the in-process backend",
		}
	}
	result, err := b.config.executor.Run(ctx, executable, "-V")
	if err != nil || result.ExitCode != 0 {
		detail := commandFailureDetail(result, err)
		return Detection{}, &UnsupportedError{
			Backend:  KindTmux,
			Platform: b.config.goos,
			Reason:   "tmux capability probe failed: " + detail,
			Action:   "verify tmux can run, or explicitly select the in-process backend",
		}
	}
	return Detection{
		Kind:       KindTmux,
		Available:  true,
		Executable: executable,
		Capabilities: Capabilities{
			External:     true,
			SendInput:    true,
			Attach:       true,
			GracefulStop: true,
			ForceStop:    true,
		},
	}, nil
}

func (b *Tmux) Launch(ctx context.Context, spec LaunchSpec) (Handle, error) {
	detection, err := b.Detect(ctx)
	if err != nil {
		return Handle{}, err
	}
	if err := validateExternalLaunchSpec(spec, b.config.goos); err != nil {
		return Handle{}, err
	}
	if strings.TrimSpace(b.config.clientExecutable) == "" {
		return Handle{}, fmt.Errorf("%w: BondCode teammate client executable is required", ErrInvalidLaunchSpec)
	}

	handle := Handle{
		Backend:     KindTmux,
		Namespace:   "bondcode-" + digestName(spec.OwnershipID, 12),
		ResourceID:  "bc-" + digestName(spec.TaskID+"\x00"+generationString(spec.Generation)+"\x00"+spec.OwnershipID, 16),
		TaskID:      spec.TaskID,
		Generation:  spec.Generation,
		OwnershipID: spec.OwnershipID,
	}
	prefix := []string{"-L", handle.Namespace}
	clientArgs := restrictedClientArgs(spec)
	args := append(append([]string{}, prefix...), "new-session", "-d", "-s", handle.ResourceID, "--", b.config.clientExecutable)
	args = append(args, clientArgs...)
	if err := requireCommandSuccess(b.config.executor.Run(ctx, detection.Executable, args...)); err != nil {
		return Handle{}, fmt.Errorf("launch tmux teammate session: %w", err)
	}

	ownershipTags := []struct {
		option string
		value  string
	}{
		{tmuxOwnerOption, spec.OwnershipID},
		{"@bondcode_task", spec.TaskID},
		{"@bondcode_generation", generationString(spec.Generation)},
	}
	for _, tag := range ownershipTags {
		optionArgs := append(append([]string{}, prefix...), "set-option", "-t", handle.ResourceID, tag.option, tag.value)
		if err := requireCommandSuccess(b.config.executor.Run(ctx, detection.Executable, optionArgs...)); err != nil {
			killArgs := append(append([]string{}, prefix...), "kill-session", "-t", handle.ResourceID)
			_, _ = b.config.executor.Run(ctx, detection.Executable, killArgs...)
			return Handle{}, fmt.Errorf("tag tmux teammate session ownership: %w", err)
		}
	}
	return handle, nil
}

func (b *Tmux) SendInput(ctx context.Context, handle Handle, input string) error {
	executable, exists, err := b.validateOwnedResource(ctx, handle)
	if err != nil {
		return err
	}
	if !exists {
		return ErrResourceNotFound
	}
	if containsControl(input) {
		return fmt.Errorf("input contains control characters")
	}
	prefix := []string{"-L", handle.Namespace}
	literal := append(append([]string{}, prefix...), "send-keys", "-t", handle.ResourceID, "-l", "--", input)
	if err := requireCommandSuccess(b.config.executor.Run(ctx, executable, literal...)); err != nil {
		return fmt.Errorf("send literal tmux input: %w", err)
	}
	enter := append(append([]string{}, prefix...), "send-keys", "-t", handle.ResourceID, "Enter")
	if err := requireCommandSuccess(b.config.executor.Run(ctx, executable, enter...)); err != nil {
		return fmt.Errorf("submit tmux input: %w", err)
	}
	return nil
}

func (b *Tmux) Status(ctx context.Context, handle Handle) (Status, error) {
	_, exists, err := b.validateOwnedResource(ctx, handle)
	if err != nil {
		return Status{}, err
	}
	if !exists {
		return Status{State: StateStopped, Healthy: true, Generation: handle.Generation}, nil
	}
	return Status{State: StateRunning, Healthy: true, Generation: handle.Generation}, nil
}

func (b *Tmux) Attach(ctx context.Context, handle Handle) error {
	executable, exists, err := b.validateOwnedResource(ctx, handle)
	if err != nil {
		return err
	}
	if !exists {
		return ErrResourceNotFound
	}
	args := []string{"-L", handle.Namespace, "attach-session", "-t", handle.ResourceID}
	return requireCommandSuccess(b.config.executor.Run(ctx, executable, args...))
}

func (b *Tmux) Show(context.Context, Handle) error {
	return &UnsupportedOperationError{Backend: KindTmux, Operation: "show", Action: "attach to the owned tmux session"}
}

func (b *Tmux) Hide(context.Context, Handle) error {
	return &UnsupportedOperationError{Backend: KindTmux, Operation: "hide", Action: "detach from tmux using the tmux client"}
}

func (b *Tmux) Stop(ctx context.Context, handle Handle, mode StopMode) error {
	if mode == StopForce {
		return b.Cleanup(ctx, handle)
	}
	if mode != StopGraceful {
		return fmt.Errorf("unknown stop mode %q", mode)
	}
	executable, exists, err := b.validateOwnedResource(ctx, handle)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	args := []string{"-L", handle.Namespace, "send-keys", "-t", handle.ResourceID, "C-c"}
	return requireCommandSuccess(b.config.executor.Run(ctx, executable, args...))
}

func (b *Tmux) Cleanup(ctx context.Context, handle Handle) error {
	executable, exists, err := b.validateOwnedResource(ctx, handle)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	args := []string{"-L", handle.Namespace, "kill-session", "-t", handle.ResourceID}
	if err := requireCommandSuccess(b.config.executor.Run(ctx, executable, args...)); err != nil {
		return fmt.Errorf("clean up owned tmux session: %w", err)
	}
	return nil
}

func (b *Tmux) validateOwnedResource(ctx context.Context, handle Handle) (string, bool, error) {
	if b == nil {
		return "", false, ErrResourceNotFound
	}
	if handle.Backend != KindTmux || strings.TrimSpace(handle.Namespace) == "" || strings.TrimSpace(handle.ResourceID) == "" || strings.TrimSpace(handle.OwnershipID) == "" || handle.Generation == 0 {
		return "", false, fmt.Errorf("%w: invalid tmux handle", ErrOwnershipMismatch)
	}
	detection, err := b.Detect(ctx)
	if err != nil {
		return "", false, err
	}
	prefix := []string{"-L", handle.Namespace}
	hasArgs := append(append([]string{}, prefix...), "has-session", "-t", handle.ResourceID)
	has, runErr := b.config.executor.Run(ctx, detection.Executable, hasArgs...)
	if runErr != nil {
		return "", false, fmt.Errorf("query tmux session: %w", runErr)
	}
	if has.ExitCode != 0 {
		return detection.Executable, false, nil
	}
	showArgs := append(append([]string{}, prefix...), "show-options", "-v", "-t", handle.ResourceID, tmuxOwnerOption)
	owner, runErr := b.config.executor.Run(ctx, detection.Executable, showArgs...)
	if runErr != nil {
		return "", false, fmt.Errorf("query tmux ownership: %w", runErr)
	}
	if owner.ExitCode != 0 {
		return "", false, fmt.Errorf("%w: tmux session has no BondCode ownership tag", ErrOwnershipMismatch)
	}
	if strings.TrimSpace(owner.Stdout) != handle.OwnershipID {
		return "", false, fmt.Errorf("%w: refusing to operate on tmux session %q", ErrOwnershipMismatch, handle.ResourceID)
	}
	return detection.Executable, true, nil
}

func restrictedClientArgs(spec LaunchSpec) []string {
	return []string{
		"teammate-client",
		"--parent-endpoint", spec.ParentEndpoint,
		"--launch-token-file", spec.TokenFile,
		"--task-id", spec.TaskID,
		"--session-id", spec.SessionID,
		"--team-id", spec.TeamID,
		"--member-id", spec.MemberID,
		"--generation", generationString(spec.Generation),
		"--backend-ownership-id", spec.OwnershipID,
	}
}

func tmuxPlatformSupported(goos string) bool {
	switch goos {
	case "aix", "darwin", "dragonfly", "freebsd", "linux", "netbsd", "openbsd", "solaris":
		return true
	default:
		return false
	}
}

func digestName(value string, bytes int) string {
	digest := sha256.Sum256([]byte(value))
	encoded := hex.EncodeToString(digest[:])
	if bytes > len(encoded) {
		return encoded
	}
	return encoded[:bytes]
}

func commandFailureDetail(result CommandResult, err error) string {
	if err != nil {
		return err.Error()
	}
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	if detail == "" {
		detail = fmt.Sprintf("exit code %d", result.ExitCode)
	}
	return detail
}

func requireCommandSuccess(result CommandResult, err error) error {
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("command failed: %s", commandFailureDetail(result, nil))
	}
	return nil
}
