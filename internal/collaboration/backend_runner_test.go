package collaboration

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/agenttask"
	execution "github.com/junnhwan/bond-code/internal/collaboration/backend"
	"github.com/junnhwan/bond-code/internal/collaboration/backendipc"
	"github.com/junnhwan/bond-code/internal/teammate"
)

type runnerBackend struct {
	kind     execution.Kind
	launched chan execution.LaunchSpec
	mu       sync.Mutex
	cleaned  bool
	launchFn func(execution.LaunchSpec)
}

func (b *runnerBackend) Kind() execution.Kind { return b.kind }
func (b *runnerBackend) Detect(context.Context) (execution.Detection, error) {
	return execution.Detection{Kind: b.kind, Available: true, Capabilities: execution.Capabilities{External: true, Attach: true, Show: true, Hide: true}}, nil
}
func (b *runnerBackend) Launch(_ context.Context, s execution.LaunchSpec) (execution.Handle, error) {
	b.launched <- s
	if b.launchFn != nil {
		b.launchFn(s)
	}
	return execution.Handle{Backend: b.kind, ResourceID: "r", TaskID: s.TaskID, Generation: s.Generation, OwnershipID: s.OwnershipID}, nil
}
func (*runnerBackend) SendInput(context.Context, execution.Handle, string) error { return nil }
func (*runnerBackend) Status(context.Context, execution.Handle) (execution.Status, error) {
	return execution.Status{State: execution.StateRunning, Healthy: true}, nil
}
func (*runnerBackend) Attach(context.Context, execution.Handle) error                   { return nil }
func (*runnerBackend) Show(context.Context, execution.Handle) error                     { return nil }
func (*runnerBackend) Hide(context.Context, execution.Handle) error                     { return nil }
func (*runnerBackend) Stop(context.Context, execution.Handle, execution.StopMode) error { return nil }
func (b *runnerBackend) Cleanup(context.Context, execution.Handle) error {
	b.mu.Lock()
	b.cleaned = true
	b.mu.Unlock()
	return nil
}

type blockingParentRunner struct{ stopped chan struct{} }

func (*blockingParentRunner) Run(ctx context.Context, _ agenttask.RunRequest) agenttask.RunResult {
	<-ctx.Done()
	return agenttask.RunResult{Err: ctx.Err()}
}
func (r *blockingParentRunner) Stop(context.Context, string, uint64) error {
	select {
	case <-r.stopped:
	default:
		close(r.stopped)
	}
	return nil
}
func (*blockingParentRunner) SendInput(context.Context, string, uint64, string) error { return nil }

func TestBackendRunnerFailsClosedWhenExternalClientLeaseExpires(t *testing.T) {
	parent := &blockingParentRunner{stopped: make(chan struct{})}
	external := &runnerBackend{kind: execution.KindTmux, launched: make(chan execution.LaunchSpec, 1)}
	registry, err := execution.NewRegistry(execution.NewInProcess(parent), external)
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := backendipc.Start(filepath.Join(t.TempDir(), "tokens"), 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.Close()
	runner := NewBackendRunner(parent, registry, supervisor)
	resultCh := make(chan agenttask.RunResult, 1)
	go func() {
		resultCh <- runner.Run(context.Background(), agenttask.RunRequest{TaskID: "task", SessionID: "s", Generation: 1, Backend: "tmux", OwnershipID: "owner"})
	}()
	spec := <-external.launched
	if spec.ParentEndpoint == "" || spec.TokenFile == "" || spec.Generation != 1 {
		t.Fatalf("launch spec = %#v", spec)
	}
	select {
	case result := <-resultCh:
		if !errors.Is(result.Err, ErrBackendLeaseExpired) {
			t.Fatalf("result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not fail closed")
	}
	select {
	case <-parent.stopped:
	case <-time.After(time.Second):
		t.Fatal("parent task was not stopped")
	}
	external.mu.Lock()
	cleaned := external.cleaned
	external.mu.Unlock()
	if !cleaned {
		t.Fatal("external resource was not cleaned")
	}
}

type completedParentRunner struct{}

func (*completedParentRunner) Run(context.Context, agenttask.RunRequest) agenttask.RunResult {
	return agenttask.RunResult{Summary: "parent result"}
}
func (*completedParentRunner) Stop(context.Context, string, uint64) error              { return nil }
func (*completedParentRunner) SendInput(context.Context, string, uint64, string) error { return nil }

func TestBackendRunnerKeepsExecutionInParentAndDeliversTerminalResult(t *testing.T) {
	parent := &completedParentRunner{}
	var output bytes.Buffer
	clientDone := make(chan error, 1)
	external := &runnerBackend{kind: execution.KindTmux, launched: make(chan execution.LaunchSpec, 1)}
	external.launchFn = func(spec execution.LaunchSpec) {
		go func() {
			clientDone <- teammate.Run(context.Background(), teammate.Config{ParentEndpoint: spec.ParentEndpoint, LaunchTokenFile: spec.TokenFile, TaskID: spec.TaskID, SessionID: spec.SessionID, TeamID: spec.TeamID, MemberID: spec.MemberID, Generation: spec.Generation, OwnershipID: spec.OwnershipID, PollInterval: time.Millisecond}, strings.NewReader(""), &output)
		}()
	}
	registry, err := execution.NewRegistry(execution.NewInProcess(parent), external)
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := backendipc.Start(filepath.Join(t.TempDir(), "tokens"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.Close()
	runner := NewBackendRunner(parent, registry, supervisor)
	result := runner.Run(context.Background(), agenttask.RunRequest{TaskID: "task", SessionID: "s", Generation: 1, Backend: "tmux", OwnershipID: "owner"})
	if result.Err != nil || result.Summary != "parent result" {
		t.Fatalf("result = %#v", result)
	}
	if err := <-clientDone; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "parent result") {
		t.Fatalf("client output = %q", output.String())
	}
}

func TestBackendRunnerControlsOnlyActiveTaskGeneration(t *testing.T) {
	parent := &blockingParentRunner{stopped: make(chan struct{})}
	external := &runnerBackend{kind: execution.KindTmux, launched: make(chan execution.LaunchSpec, 1)}
	registry, err := execution.NewRegistry(execution.NewInProcess(parent), external)
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := backendipc.Start(filepath.Join(t.TempDir(), "tokens"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.Close()
	runner := NewBackendRunner(parent, registry, supervisor)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan agenttask.RunResult, 1)
	go func() {
		done <- runner.Run(ctx, agenttask.RunRequest{TaskID: "task", SessionID: "s", Generation: 2, Backend: "tmux", OwnershipID: "owner"})
	}()
	<-external.launched
	view, err := runner.BackendStatus(context.Background(), "task", 2)
	if err != nil || view.Backend != "tmux" || view.State != "running" || !view.Healthy || !view.Attach || !view.Show || !view.Hide {
		t.Fatalf("view=%#v err=%v", view, err)
	}
	if err := runner.BackendAttach(context.Background(), "task", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.BackendStatus(context.Background(), "task", 1); !errors.Is(err, ErrBackendRunNotActive) {
		t.Fatalf("stale generation err=%v", err)
	}
	cancel()
	<-done
}
