package collaboration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/junnhwan/bond-code/internal/agenttask"
	execution "github.com/junnhwan/bond-code/internal/collaboration/backend"
	"github.com/junnhwan/bond-code/internal/collaboration/backendipc"
)

var (
	ErrBackendLeaseExpired = errors.New("external backend client lease expired")
	ErrBackendRunNotActive = errors.New("backend task generation is not active")
)

type backendRunKey struct {
	taskID     string
	generation uint64
}
type activeBackendRun struct {
	backend  execution.Backend
	handle   execution.Handle
	identity backendipc.Identity
}

// BackendRunner keeps model/tool execution in the parent while an external
// backend supplies only an observable terminal client lifecycle.
type BackendRunner struct {
	parent     agenttask.Runner
	registry   *execution.Registry
	supervisor *backendipc.Supervisor
	mu         sync.Mutex
	active     map[backendRunKey]activeBackendRun
}

func NewBackendRunner(parent agenttask.Runner, registry *execution.Registry, supervisor *backendipc.Supervisor) *BackendRunner {
	runner := &BackendRunner{parent: parent, registry: registry, supervisor: supervisor, active: map[backendRunKey]activeBackendRun{}}
	if supervisor != nil {
		supervisor.SetInputHandler(func(ctx context.Context, identity backendipc.Identity, input string) error {
			return parent.SendInput(ctx, identity.TaskID, identity.Generation, input)
		})
	}
	return runner
}
func (r *BackendRunner) Run(ctx context.Context, req agenttask.RunRequest) agenttask.RunResult {
	if r == nil || r.parent == nil || r.registry == nil {
		return agenttask.RunResult{Err: errors.New("backend runner dependencies are required")}
	}
	selected, _, err := r.registry.Resolve(ctx, req.Backend)
	if err != nil {
		return agenttask.RunResult{Err: err}
	}
	if selected.Kind() == execution.KindInProcess {
		return r.parent.Run(ctx, req)
	}
	if r.supervisor == nil {
		return agenttask.RunResult{Err: errors.New("external backend supervisor is required")}
	}
	identity := backendipc.Identity{TaskID: req.TaskID, SessionID: req.SessionID, TeamID: req.TeamID, MemberID: req.MemberID, Generation: req.Generation, OwnershipID: req.OwnershipID}
	launch, lease, err := r.supervisor.Prepare(identity)
	if err != nil {
		return agenttask.RunResult{Err: err}
	}
	handle, err := selected.Launch(ctx, execution.LaunchSpec{TaskID: req.TaskID, SessionID: req.SessionID, TeamID: req.TeamID, MemberID: req.MemberID, Generation: req.Generation, OwnershipID: req.OwnershipID, ParentEndpoint: launch.Endpoint, TokenFile: launch.TokenFile, Description: req.Description, Prompt: req.Prompt, Profile: req.Profile})
	if err != nil {
		r.supervisor.Release(identity)
		return agenttask.RunResult{Err: err}
	}
	key := backendRunKey{req.TaskID, req.Generation}
	active := activeBackendRun{selected, handle, identity}
	r.mu.Lock()
	r.active[key] = active
	r.mu.Unlock()
	defer func() {
		r.supervisor.Release(identity)
		r.mu.Lock()
		delete(r.active, key)
		r.mu.Unlock()
		_ = selected.Cleanup(context.Background(), handle)
	}()
	_ = r.supervisor.Publish(identity, "running", "task started", "")
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan agenttask.RunResult, 1)
	go func() { results <- r.parent.Run(runCtx, req) }()
	select {
	case result := <-results:
		state := "completed"
		detail := result.ErrorText
		if result.Err != nil {
			state = "failed"
			if detail == "" {
				detail = result.Err.Error()
			}
		}
		output := result.Summary
		if output == "" && detail != "" {
			output = detail
		}
		_ = r.supervisor.Publish(identity, state, output, detail)
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-lease.TerminalSeen():
			timer.Stop()
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
		}
		return result
	case <-lease.Done():
		cancel()
		_ = r.parent.Stop(context.Background(), req.TaskID, req.Generation)
		_ = r.supervisor.Publish(identity, "interrupted", "", ErrBackendLeaseExpired.Error())
		return agenttask.RunResult{ErrorText: ErrBackendLeaseExpired.Error(), Err: ErrBackendLeaseExpired}
	case <-ctx.Done():
		cancel()
		_ = r.parent.Stop(context.Background(), req.TaskID, req.Generation)
		_ = r.supervisor.Publish(identity, "canceled", "", ctx.Err().Error())
		return agenttask.RunResult{ErrorText: ctx.Err().Error(), Err: ctx.Err()}
	}
}
func (r *BackendRunner) Stop(ctx context.Context, taskID string, generation uint64) error {
	key := backendRunKey{taskID, generation}
	r.mu.Lock()
	active, ok := r.active[key]
	r.mu.Unlock()
	if ok {
		if err := active.backend.Stop(ctx, active.handle, execution.StopGraceful); err != nil {
			return fmt.Errorf("stop external backend: %w", err)
		}
	}
	return r.parent.Stop(ctx, taskID, generation)
}
func (r *BackendRunner) SendInput(ctx context.Context, taskID string, generation uint64, input string) error {
	return r.parent.SendInput(ctx, taskID, generation, input)
}

func (r *BackendRunner) activeRun(taskID string, generation uint64) (activeBackendRun, error) {
	if r == nil {
		return activeBackendRun{}, ErrBackendRunNotActive
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	active, ok := r.active[backendRunKey{taskID: taskID, generation: generation}]
	if !ok {
		return activeBackendRun{}, ErrBackendRunNotActive
	}
	return active, nil
}

func (r *BackendRunner) BackendStatus(ctx context.Context, taskID string, generation uint64) (agenttask.BackendView, error) {
	active, err := r.activeRun(taskID, generation)
	if err != nil {
		return agenttask.BackendView{}, err
	}
	status, err := active.backend.Status(ctx, active.handle)
	if err != nil {
		return agenttask.BackendView{}, err
	}
	detection, err := active.backend.Detect(ctx)
	if err != nil {
		return agenttask.BackendView{}, err
	}
	return agenttask.BackendView{Backend: string(active.backend.Kind()), State: string(status.State), Healthy: status.Healthy, Detail: status.Detail, Attach: detection.Capabilities.Attach, Show: detection.Capabilities.Show, Hide: detection.Capabilities.Hide}, nil
}
func (r *BackendRunner) BackendAttach(ctx context.Context, taskID string, generation uint64) error {
	active, err := r.activeRun(taskID, generation)
	if err != nil {
		return err
	}
	return active.backend.Attach(ctx, active.handle)
}
func (r *BackendRunner) BackendShow(ctx context.Context, taskID string, generation uint64) error {
	active, err := r.activeRun(taskID, generation)
	if err != nil {
		return err
	}
	return active.backend.Show(ctx, active.handle)
}
func (r *BackendRunner) BackendHide(ctx context.Context, taskID string, generation uint64) error {
	active, err := r.activeRun(taskID, generation)
	if err != nil {
		return err
	}
	return active.backend.Hide(ctx, active.handle)
}
