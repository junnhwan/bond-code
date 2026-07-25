package backend

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/junnhwan/bond-code/internal/agenttask"
)

// InProcess adapts the existing AgentTask runner without creating another
// agent execution authority.
type InProcess struct {
	runner agenttask.Runner

	mu   sync.Mutex
	runs map[string]*inProcessRun
}

type inProcessRun struct {
	handle Handle
	cancel context.CancelFunc
	state  State
	result *Result
}

func NewInProcess(runner agenttask.Runner) *InProcess {
	return &InProcess{runner: runner, runs: make(map[string]*inProcessRun)}
}

func (b *InProcess) Kind() Kind { return KindInProcess }

func (b *InProcess) Detect(context.Context) (Detection, error) {
	if b == nil || b.runner == nil {
		return Detection{}, fmt.Errorf("in-process backend requires an agent task runner")
	}
	return Detection{
		Kind:      KindInProcess,
		Available: true,
		Capabilities: Capabilities{
			SendInput:    true,
			GracefulStop: true,
			ForceStop:    true,
		},
	}, nil
}

func (b *InProcess) Launch(ctx context.Context, spec LaunchSpec) (Handle, error) {
	if b == nil || b.runner == nil {
		return Handle{}, fmt.Errorf("in-process backend requires an agent task runner")
	}
	if err := validateCommonLaunchSpec(spec); err != nil {
		return Handle{}, err
	}

	handle := Handle{
		Backend:     KindInProcess,
		ResourceID:  resourceKey(spec.TaskID, spec.Generation),
		TaskID:      spec.TaskID,
		Generation:  spec.Generation,
		OwnershipID: spec.OwnershipID,
	}
	key := handle.ResourceID
	runCtx, cancel := context.WithCancel(ctx)
	run := &inProcessRun{handle: handle, cancel: cancel, state: StateStarting}

	b.mu.Lock()
	if _, exists := b.runs[key]; exists {
		b.mu.Unlock()
		cancel()
		return Handle{}, fmt.Errorf("%w: task %s generation %d", ErrAlreadyLaunched, spec.TaskID, spec.Generation)
	}
	b.runs[key] = run
	b.mu.Unlock()

	request := agenttask.RunRequest{
		TaskID:      spec.TaskID,
		Generation:  spec.Generation,
		SessionID:   spec.SessionID,
		Description: spec.Description,
		Prompt:      spec.Prompt,
		Profile:     spec.Profile,
	}
	go b.run(runCtx, key, request)
	return handle, nil
}

func (b *InProcess) run(ctx context.Context, key string, request agenttask.RunRequest) {
	b.mu.Lock()
	run, ok := b.runs[key]
	if !ok {
		b.mu.Unlock()
		return
	}
	run.state = StateRunning
	b.mu.Unlock()

	runResult := b.runner.Run(ctx, request)
	result := &Result{
		Summary:     runResult.Summary,
		ResultPath:  runResult.ResultPath,
		ErrorText:   runResult.ErrorText,
		LegacyAlias: runResult.LegacyAlias,
		Err:         runResult.Err,
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	run, ok = b.runs[key]
	if !ok {
		return
	}
	run.result = result
	if runResult.Err == nil || (run.state == StateStopping && errors.Is(runResult.Err, context.Canceled)) {
		run.state = StateStopped
		return
	}
	run.state = StateFailed
}

func (b *InProcess) SendInput(ctx context.Context, handle Handle, input string) error {
	run, err := b.lookup(handle)
	if err != nil {
		return err
	}
	if run.state != StateStarting && run.state != StateRunning {
		return fmt.Errorf("cannot send input while in-process backend is %s", run.state)
	}
	return b.runner.SendInput(ctx, handle.TaskID, handle.Generation, input)
}

func (b *InProcess) Status(_ context.Context, handle Handle) (Status, error) {
	run, err := b.lookup(handle)
	if err != nil {
		return Status{}, err
	}
	status := Status{
		State:      run.state,
		Healthy:    run.state != StateFailed,
		Generation: handle.Generation,
	}
	if run.result != nil {
		copyResult := *run.result
		status.Result = &copyResult
		status.Detail = copyResult.ErrorText
		if status.Detail == "" && copyResult.Err != nil {
			status.Detail = copyResult.Err.Error()
		}
	}
	return status, nil
}

func (b *InProcess) Attach(context.Context, Handle) error {
	return &UnsupportedOperationError{Backend: KindInProcess, Operation: "attach", Action: "observe the task through BondCode task output"}
}

func (b *InProcess) Show(context.Context, Handle) error {
	return &UnsupportedOperationError{Backend: KindInProcess, Operation: "show", Action: "observe the task through BondCode task output"}
}

func (b *InProcess) Hide(context.Context, Handle) error {
	return &UnsupportedOperationError{Backend: KindInProcess, Operation: "hide"}
}

func (b *InProcess) Stop(ctx context.Context, handle Handle, mode StopMode) error {
	run, err := b.lookup(handle)
	if err != nil {
		return err
	}
	if mode != StopGraceful && mode != StopForce {
		return fmt.Errorf("unknown stop mode %q", mode)
	}
	if run.state == StateStopped || run.state == StateFailed {
		return nil
	}

	b.mu.Lock()
	if current, ok := b.runs[handle.ResourceID]; ok {
		current.state = StateStopping
	}
	b.mu.Unlock()

	if err := b.runner.Stop(ctx, handle.TaskID, handle.Generation); err != nil {
		return err
	}
	if mode == StopForce {
		run.cancel()
	}
	return nil
}

func (b *InProcess) Cleanup(ctx context.Context, handle Handle) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	run, ok := b.runs[handle.ResourceID]
	if !ok {
		b.mu.Unlock()
		return nil
	}
	if err := validateHandle(run.handle, handle, KindInProcess); err != nil {
		b.mu.Unlock()
		return err
	}
	delete(b.runs, handle.ResourceID)
	b.mu.Unlock()

	_ = b.runner.Stop(ctx, handle.TaskID, handle.Generation)
	run.cancel()
	return nil
}

func (b *InProcess) lookup(handle Handle) (*inProcessRun, error) {
	if b == nil {
		return nil, ErrResourceNotFound
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	run, ok := b.runs[handle.ResourceID]
	if !ok {
		return nil, ErrResourceNotFound
	}
	if err := validateHandle(run.handle, handle, KindInProcess); err != nil {
		return nil, err
	}
	copyRun := *run
	if run.result != nil {
		result := *run.result
		copyRun.result = &result
	}
	return &copyRun, nil
}

func validateHandle(owned, provided Handle, kind Kind) error {
	if provided.Backend != kind || provided.ResourceID != owned.ResourceID || provided.TaskID != owned.TaskID || provided.Generation != owned.Generation || provided.OwnershipID != owned.OwnershipID {
		return fmt.Errorf("%w: refusing to use resource %q", ErrOwnershipMismatch, provided.ResourceID)
	}
	return nil
}

func resourceKey(taskID string, generation uint64) string {
	return fmt.Sprintf("%s:%d", taskID, generation)
}
