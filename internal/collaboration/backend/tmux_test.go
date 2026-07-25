package backend

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
)

type commandCall struct {
	name string
	args []string
}

type recordingExecutor struct {
	mu      sync.Mutex
	paths   map[string]string
	calls   []commandCall
	run     func(name string, args []string) (CommandResult, error)
	lookups []string
}

func (e *recordingExecutor) LookPath(file string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lookups = append(e.lookups, file)
	if path := e.paths[file]; path != "" {
		return path, nil
	}
	return "", fmt.Errorf("%s not found", file)
}

func (e *recordingExecutor) Run(_ context.Context, name string, args ...string) (CommandResult, error) {
	e.mu.Lock()
	e.calls = append(e.calls, commandCall{name: name, args: append([]string(nil), args...)})
	run := e.run
	e.mu.Unlock()
	if run != nil {
		return run(name, args)
	}
	return CommandResult{}, nil
}

func (e *recordingExecutor) snapshotCalls() []commandCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	calls := make([]commandCall, len(e.calls))
	for i, call := range e.calls {
		calls[i] = commandCall{name: call.name, args: append([]string(nil), call.args...)}
	}
	return calls
}

func TestTmuxDetectionRejectsWindowsWithoutProbingPath(t *testing.T) {
	executor := &recordingExecutor{paths: map[string]string{"tmux": `C:\tools\tmux.exe`}}
	backend := NewTmux(executor, WithPlatform("windows"))
	_, err := backend.Detect(context.Background())
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "select the in-process backend") {
		t.Fatalf("Detect error = %v", err)
	}
	if len(executor.lookups) != 0 || len(executor.snapshotCalls()) != 0 {
		t.Fatalf("unsupported platform was probed: lookups=%v calls=%v", executor.lookups, executor.snapshotCalls())
	}
}

func TestTmuxDetectionRejectsWSLExplicitly(t *testing.T) {
	executor := &recordingExecutor{paths: map[string]string{"tmux": "/usr/bin/tmux"}}
	backend := NewTmux(executor, WithPlatform("linux"), WithWSL(true))
	_, err := backend.Detect(context.Background())
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "WSL") || !strings.Contains(err.Error(), "mapping design") {
		t.Fatalf("Detect error = %v", err)
	}
	if len(executor.lookups) != 0 {
		t.Fatalf("WSL detection must not infer support from PATH: %v", executor.lookups)
	}
}

func TestTmuxDetectionReportsCapabilities(t *testing.T) {
	executor := workingTmuxExecutor()
	backend := NewTmux(executor, WithPlatform("linux"), WithClientExecutable("/opt/bondcode"))
	detection, err := backend.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !detection.Available || detection.Executable != "/usr/bin/tmux" || !detection.Capabilities.External || !detection.Capabilities.Attach || !detection.Capabilities.SendInput {
		t.Fatalf("detection = %#v", detection)
	}
}

func TestTmuxLaunchUsesArgumentVectorAndOmitsParentOwnedWork(t *testing.T) {
	executor := workingTmuxExecutor()
	backend := NewTmux(executor, WithPlatform("linux"), WithClientExecutable("/opt/Bond Code/bondcode"))
	spec := validExternalSpec()
	spec.TaskID = "task;$(touch should-not-run)"
	spec.OwnershipID = "owner ' quoted ; value"
	spec.ParentEndpoint = "unix:///tmp/parent socket;$x"
	spec.TokenFile = "/tmp/token file;$(cat secret)"
	spec.Prompt = "PARENT-ONLY-PROMPT"

	handle, err := backend.Launch(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if handle.Backend != KindTmux || handle.OwnershipID != spec.OwnershipID || handle.Generation != spec.Generation {
		t.Fatalf("handle = %#v", handle)
	}
	if safeName := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`); !safeName.MatchString(handle.ResourceID) || !safeName.MatchString(handle.Namespace) {
		t.Fatalf("unsafe tmux names: %#v", handle)
	}

	var launch *commandCall
	for _, call := range executor.snapshotCalls() {
		if containsArg(call.args, "new-session") {
			copyCall := call
			launch = &copyCall
			break
		}
	}
	if launch == nil {
		t.Fatal("new-session call not found")
	}
	if launch.name != "/usr/bin/tmux" {
		t.Fatalf("launch executable = %q", launch.name)
	}
	for _, value := range []string{"/opt/Bond Code/bondcode", spec.TaskID, spec.ParentEndpoint, spec.TokenFile, spec.OwnershipID, generationString(spec.Generation)} {
		if !containsArg(launch.args, value) {
			t.Fatalf("launch args do not preserve %q as one argument: %#v", value, launch.args)
		}
	}
	joined := strings.Join(launch.args, " ")
	if strings.Contains(joined, spec.Prompt) || containsSubsequence(launch.args, []string{"sh", "-c"}) || containsSubsequence(launch.args, []string{"bash", "-c"}) {
		t.Fatalf("unsafe or parent-owned launch args: %#v", launch.args)
	}
}

func TestTmuxCleanupValidatesOwnershipAndIsIdempotent(t *testing.T) {
	owner := "owner-cleanup"
	killed := false
	executor := workingTmuxExecutor()
	executor.run = func(_ string, args []string) (CommandResult, error) {
		switch {
		case containsArg(args, "-V"):
			return CommandResult{Stdout: "tmux 3.4\n"}, nil
		case containsArg(args, "has-session") && killed:
			return CommandResult{ExitCode: 1, Stderr: "session not found"}, nil
		case containsArg(args, "has-session"):
			return CommandResult{}, nil
		case containsArg(args, "show-options"):
			return CommandResult{Stdout: owner + "\n"}, nil
		case containsArg(args, "kill-session"):
			killed = true
			return CommandResult{}, nil
		default:
			return CommandResult{}, nil
		}
	}
	backend := NewTmux(executor, WithPlatform("linux"), WithClientExecutable("/opt/bondcode"))
	spec := validExternalSpec()
	spec.OwnershipID = owner
	handle, err := backend.Launch(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Cleanup(context.Background(), handle); err != nil {
		t.Fatal(err)
	}
	if err := backend.Cleanup(context.Background(), handle); err != nil {
		t.Fatalf("second cleanup = %v", err)
	}
	killCount := 0
	for _, call := range executor.snapshotCalls() {
		if containsArg(call.args, "kill-session") {
			killCount++
		}
	}
	if killCount != 1 {
		t.Fatalf("kill-session calls = %d, want 1", killCount)
	}
}

func TestTmuxCleanupRefusesForeignOwnership(t *testing.T) {
	executor := workingTmuxExecutor()
	executor.run = func(_ string, args []string) (CommandResult, error) {
		if containsArg(args, "-V") {
			return CommandResult{Stdout: "tmux 3.4\n"}, nil
		}
		if containsArg(args, "show-options") {
			return CommandResult{Stdout: "another-runtime\n"}, nil
		}
		return CommandResult{}, nil
	}
	backend := NewTmux(executor, WithPlatform("linux"), WithClientExecutable("/opt/bondcode"))
	handle, err := backend.Launch(context.Background(), validExternalSpec())
	if err != nil {
		t.Fatal(err)
	}
	before := len(executor.snapshotCalls())
	if err := backend.Cleanup(context.Background(), handle); !errors.Is(err, ErrOwnershipMismatch) {
		t.Fatalf("Cleanup error = %v", err)
	}
	for _, call := range executor.snapshotCalls()[before:] {
		if containsArg(call.args, "kill-session") {
			t.Fatalf("foreign session was killed: %#v", call)
		}
	}
}

func TestTmuxSendInputUsesLiteralMode(t *testing.T) {
	executor := workingTmuxExecutor()
	owner := validExternalSpec().OwnershipID
	executor.run = func(_ string, args []string) (CommandResult, error) {
		if containsArg(args, "-V") {
			return CommandResult{Stdout: "tmux 3.4\n"}, nil
		}
		if containsArg(args, "show-options") {
			return CommandResult{Stdout: owner + "\n"}, nil
		}
		return CommandResult{}, nil
	}
	backend := NewTmux(executor, WithPlatform("linux"), WithClientExecutable("/opt/bondcode"))
	handle, err := backend.Launch(context.Background(), validExternalSpec())
	if err != nil {
		t.Fatal(err)
	}
	input := "hello; $(touch should-not-run) ' quoted"
	if err := backend.SendInput(context.Background(), handle, input); err != nil {
		t.Fatal(err)
	}
	want := []string{"send-keys", "-t", handle.ResourceID, "-l", "--", input}
	found := false
	for _, call := range executor.snapshotCalls() {
		if len(call.args) >= 2 && reflect.DeepEqual(call.args[2:], want) {
			found = true
		}
	}
	if !found {
		t.Fatalf("literal send-keys call not found in %#v", executor.snapshotCalls())
	}
}

func workingTmuxExecutor() *recordingExecutor {
	return &recordingExecutor{
		paths: map[string]string{"tmux": "/usr/bin/tmux"},
		run: func(_ string, args []string) (CommandResult, error) {
			if containsArg(args, "-V") {
				return CommandResult{Stdout: "tmux 3.4\n"}, nil
			}
			return CommandResult{}, nil
		},
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func containsSubsequence(values, want []string) bool {
	if len(want) == 0 || len(want) > len(values) {
		return false
	}
	for i := 0; i <= len(values)-len(want); i++ {
		if reflect.DeepEqual(values[i:i+len(want)], want) {
			return true
		}
	}
	return false
}
