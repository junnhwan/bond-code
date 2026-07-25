package backend

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/agenttask"
)

type controlledAgentTaskRunner struct {
	started chan agenttask.RunRequest
	finish  chan agenttask.RunResult

	mu       sync.Mutex
	stops    []runnerStop
	inputs   []runnerInput
	stopErr  error
	inputErr error
}

type runnerStop struct {
	taskID     string
	generation uint64
}

type runnerInput struct {
	taskID     string
	generation uint64
	input      string
}

func newControlledAgentTaskRunner() *controlledAgentTaskRunner {
	return &controlledAgentTaskRunner{
		started: make(chan agenttask.RunRequest, 1),
		finish:  make(chan agenttask.RunResult, 1),
	}
}

func (r *controlledAgentTaskRunner) Run(ctx context.Context, req agenttask.RunRequest) agenttask.RunResult {
	r.started <- req
	select {
	case result := <-r.finish:
		return result
	case <-ctx.Done():
		return agenttask.RunResult{Err: ctx.Err()}
	}
}

func (r *controlledAgentTaskRunner) Stop(_ context.Context, taskID string, generation uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stops = append(r.stops, runnerStop{taskID: taskID, generation: generation})
	return r.stopErr
}

func (r *controlledAgentTaskRunner) SendInput(_ context.Context, taskID string, generation uint64, input string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inputs = append(r.inputs, runnerInput{taskID: taskID, generation: generation, input: input})
	return r.inputErr
}

func TestInProcessDetectsEverywhere(t *testing.T) {
	backend := NewInProcess(newControlledAgentTaskRunner())
	detection, err := backend.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !detection.Available || detection.Kind != KindInProcess || detection.Capabilities.External {
		t.Fatalf("detection = %#v", detection)
	}
	if !detection.Capabilities.SendInput || !detection.Capabilities.GracefulStop || !detection.Capabilities.ForceStop {
		t.Fatalf("capabilities = %#v", detection.Capabilities)
	}
}

func TestInProcessLaunchAdaptsAgentTaskRunner(t *testing.T) {
	runner := newControlledAgentTaskRunner()
	backend := NewInProcess(runner)
	spec := LaunchSpec{
		TaskID: "task-1", SessionID: "session-1", Generation: 3, OwnershipID: "owner-1",
		Description: "implement backend", Prompt: "write tests first", Profile: "coder",
	}
	handle, err := backend.Launch(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	request := <-runner.started
	if request.TaskID != spec.TaskID || request.Generation != spec.Generation || request.SessionID != spec.SessionID || request.Prompt != spec.Prompt || request.Profile != spec.Profile {
		t.Fatalf("request = %#v", request)
	}
	if handle.Backend != KindInProcess || handle.OwnershipID != spec.OwnershipID || handle.Generation != spec.Generation {
		t.Fatalf("handle = %#v", handle)
	}

	runner.finish <- agenttask.RunResult{Summary: "done", ResultPath: "/tmp/result", LegacyAlias: "legacy-1"}
	status := waitForInProcessState(t, backend, handle, StateStopped)
	if !status.Healthy || status.Result == nil || status.Result.Summary != "done" || status.Result.LegacyAlias != "legacy-1" {
		t.Fatalf("status = %#v", status)
	}
}

func TestInProcessForwardsInputAndStopWithGeneration(t *testing.T) {
	runner := newControlledAgentTaskRunner()
	backend := NewInProcess(runner)
	handle, err := backend.Launch(context.Background(), LaunchSpec{
		TaskID: "task-2", SessionID: "session-1", Generation: 7, OwnershipID: "owner-2", Prompt: "wait",
	})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	if err := backend.SendInput(context.Background(), handle, "focus on cleanup"); err != nil {
		t.Fatal(err)
	}
	if err := backend.Stop(context.Background(), handle, StopGraceful); err != nil {
		t.Fatal(err)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.inputs) != 1 || runner.inputs[0] != (runnerInput{taskID: "task-2", generation: 7, input: "focus on cleanup"}) {
		t.Fatalf("inputs = %#v", runner.inputs)
	}
	if len(runner.stops) != 1 || runner.stops[0] != (runnerStop{taskID: "task-2", generation: 7}) {
		t.Fatalf("stops = %#v", runner.stops)
	}
}

func TestInProcessRejectsForeignHandleAndCleanupIsIdempotent(t *testing.T) {
	runner := newControlledAgentTaskRunner()
	backend := NewInProcess(runner)
	handle, err := backend.Launch(context.Background(), LaunchSpec{
		TaskID: "task-3", SessionID: "session-1", Generation: 1, OwnershipID: "owner-3", Prompt: "wait",
	})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	foreign := handle
	foreign.OwnershipID = "another-runtime"
	if _, err := backend.Status(context.Background(), foreign); !errors.Is(err, ErrOwnershipMismatch) {
		t.Fatalf("foreign status error = %v", err)
	}
	if err := backend.Cleanup(context.Background(), handle); err != nil {
		t.Fatal(err)
	}
	if err := backend.Cleanup(context.Background(), handle); err != nil {
		t.Fatalf("second cleanup = %v", err)
	}
}

func waitForInProcessState(t *testing.T, backend *InProcess, handle Handle, want State) Status {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err := backend.Status(context.Background(), handle)
		if err != nil {
			t.Fatal(err)
		}
		if status.State == want {
			return status
		}
		runtime.Gosched()
	}
	t.Fatalf("backend did not reach state %s", want)
	return Status{}
}
