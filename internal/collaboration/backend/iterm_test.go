package backend

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestITermDetectionRejectsNonDarwinWithoutProbing(t *testing.T) {
	executor := &recordingExecutor{paths: map[string]string{"osascript": "/usr/bin/osascript"}}
	backend := NewITerm(executor, WithPlatform("windows"))
	_, err := backend.Detect(context.Background())
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "select the in-process backend") {
		t.Fatalf("Detect error = %v", err)
	}
	if len(executor.lookups) != 0 || len(executor.snapshotCalls()) != 0 {
		t.Fatalf("unsupported platform was probed: lookups=%v calls=%v", executor.lookups, executor.snapshotCalls())
	}
}

func TestITermDetectionUsesSupportedIntegrationProbe(t *testing.T) {
	executor := workingITermExecutor()
	backend := NewITerm(executor, WithPlatform("darwin"), WithClientExecutable("/Applications/BondCode.app/bondcode"))
	detection, err := backend.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !detection.Available || detection.Executable != "/usr/bin/osascript" || !detection.Capabilities.External || !detection.Capabilities.Show || !detection.Capabilities.Attach {
		t.Fatalf("detection = %#v", detection)
	}
	calls := executor.snapshotCalls()
	if len(calls) != 1 || len(calls[0].args) != 2 || calls[0].args[0] != "-e" || calls[0].args[1] != itermDetectScript {
		t.Fatalf("detection calls = %#v", calls)
	}
}

func TestITermLaunchKeepsAppleScriptStaticAndShellQuotesClientArguments(t *testing.T) {
	executor := workingITermExecutor()
	backend := NewITerm(executor, WithPlatform("darwin"), WithClientExecutable("/Applications/Bond Code/bondcode"))
	spec := validExternalSpec()
	spec.TaskID = "task '; $(touch should-not-run)"
	spec.OwnershipID = "owner ' quoted ; value"
	spec.ParentEndpoint = "unix:///tmp/parent socket;$x"
	spec.TokenFile = "/tmp/token file;$(cat secret)"
	spec.Prompt = "PARENT-ONLY-PROMPT"

	handle, err := backend.Launch(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if handle.Backend != KindITerm || handle.ResourceID != "iterm-session-42" || handle.OwnershipID != spec.OwnershipID {
		t.Fatalf("handle = %#v", handle)
	}

	var launch *commandCall
	for _, call := range executor.snapshotCalls() {
		if len(call.args) >= 2 && call.args[0] == "-e" && call.args[1] == itermLaunchScript {
			copyCall := call
			launch = &copyCall
			break
		}
	}
	if launch == nil {
		t.Fatal("iTerm launch call not found")
	}
	if launch.name != "/usr/bin/osascript" || len(launch.args) != 5 || launch.args[2] != "--" || launch.args[4] != spec.OwnershipID {
		t.Fatalf("launch call = %#v", launch)
	}
	if strings.Contains(launch.args[1], spec.TaskID) || strings.Contains(launch.args[1], spec.TokenFile) || strings.Contains(launch.args[1], spec.OwnershipID) {
		t.Fatalf("dynamic value was interpolated into AppleScript: %q", launch.args[1])
	}
	command := launch.args[3]
	for _, value := range append([]string{"/Applications/Bond Code/bondcode"}, restrictedClientArgs(spec)...) {
		if !strings.Contains(command, shellQuote(value)) {
			t.Fatalf("command %q does not safely quote %q as %q", command, value, shellQuote(value))
		}
	}
	if strings.Contains(command, spec.Prompt) {
		t.Fatalf("parent-owned prompt leaked into iTerm command: %q", command)
	}
}

func TestITermCleanupValidatesOwnershipAndIsIdempotent(t *testing.T) {
	owner := "owner-iterm-cleanup"
	closed := false
	executor := workingITermExecutor()
	executor.run = func(_ string, args []string) (CommandResult, error) {
		switch {
		case len(args) >= 2 && args[1] == itermDetectScript:
			return CommandResult{Stdout: "com.googlecode.iterm2\n"}, nil
		case len(args) >= 2 && args[1] == itermLaunchScript:
			return CommandResult{Stdout: "iterm-session-42\n"}, nil
		case len(args) >= 2 && args[1] == itermInspectScript && closed:
			return CommandResult{Stdout: itermMissingMarker + "\n"}, nil
		case len(args) >= 2 && args[1] == itermInspectScript:
			return CommandResult{Stdout: owner + "\n"}, nil
		case len(args) >= 2 && args[1] == itermCloseScript:
			closed = true
			return CommandResult{}, nil
		default:
			return CommandResult{}, nil
		}
	}
	backend := NewITerm(executor, WithPlatform("darwin"), WithClientExecutable("/opt/bondcode"))
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
	closeCount := 0
	for _, call := range executor.snapshotCalls() {
		if len(call.args) >= 2 && call.args[1] == itermCloseScript {
			closeCount++
		}
	}
	if closeCount != 1 {
		t.Fatalf("close calls = %d, want 1", closeCount)
	}
}

func TestITermCleanupRefusesForeignOwnership(t *testing.T) {
	executor := workingITermExecutor()
	executor.run = func(_ string, args []string) (CommandResult, error) {
		switch {
		case len(args) >= 2 && args[1] == itermDetectScript:
			return CommandResult{Stdout: "com.googlecode.iterm2\n"}, nil
		case len(args) >= 2 && args[1] == itermLaunchScript:
			return CommandResult{Stdout: "iterm-session-42\n"}, nil
		case len(args) >= 2 && args[1] == itermInspectScript:
			return CommandResult{Stdout: "another-runtime\n"}, nil
		default:
			return CommandResult{}, nil
		}
	}
	backend := NewITerm(executor, WithPlatform("darwin"), WithClientExecutable("/opt/bondcode"))
	handle, err := backend.Launch(context.Background(), validExternalSpec())
	if err != nil {
		t.Fatal(err)
	}
	before := len(executor.snapshotCalls())
	if err := backend.Cleanup(context.Background(), handle); !errors.Is(err, ErrOwnershipMismatch) {
		t.Fatalf("Cleanup error = %v", err)
	}
	for _, call := range executor.snapshotCalls()[before:] {
		if len(call.args) >= 2 && call.args[1] == itermCloseScript {
			t.Fatalf("foreign iTerm session was closed: %#v", call)
		}
	}
}

func workingITermExecutor() *recordingExecutor {
	return &recordingExecutor{
		paths: map[string]string{"osascript": "/usr/bin/osascript"},
		run: func(_ string, args []string) (CommandResult, error) {
			if len(args) >= 2 && args[1] == itermDetectScript {
				return CommandResult{Stdout: "com.googlecode.iterm2\n"}, nil
			}
			if len(args) >= 2 && args[1] == itermLaunchScript {
				return CommandResult{Stdout: "iterm-session-42\n"}, nil
			}
			return CommandResult{}, nil
		},
	}
}
