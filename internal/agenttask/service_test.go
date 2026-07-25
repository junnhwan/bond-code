package agenttask

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

type controlledRunner struct {
	mu      sync.Mutex
	runs    map[uint64]chan RunResult
	started chan RunRequest
	inputs  []string
}

func newControlledRunner() *controlledRunner {
	return &controlledRunner{runs: map[uint64]chan RunResult{}, started: make(chan RunRequest, 8)}
}
func (r *controlledRunner) Run(ctx context.Context, req RunRequest) RunResult {
	r.mu.Lock()
	ch := r.runs[req.Generation]
	if ch == nil {
		ch = make(chan RunResult, 1)
		r.runs[req.Generation] = ch
	}
	r.mu.Unlock()
	r.started <- req
	select {
	case result := <-ch:
		return result
	case <-ctx.Done():
		return RunResult{Err: ctx.Err()}
	}
}
func (r *controlledRunner) Stop(context.Context, string, uint64) error { return nil }
func (r *controlledRunner) SendInput(_ context.Context, _ string, _ uint64, input string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inputs = append(r.inputs, input)
	return nil
}
func (r *controlledRunner) finish(g uint64, result RunResult) {
	r.mu.Lock()
	ch := r.runs[g]
	r.mu.Unlock()
	ch <- result
}

func openService(t *testing.T, r Runner) *Service {
	t.Helper()
	l, err := Open(filepath.Join(t.TempDir(), "tasks.json"), "lease")
	if err != nil {
		t.Fatal(err)
	}
	return NewService(l, r)
}
func TestServiceBackgroundStartGetListAndWait(t *testing.T) {
	r := newControlledRunner()
	s := openService(t, r)
	task, err := s.Start(context.Background(), StartInput{IdempotencyKey: "one", SessionID: "s", Description: "work", Prompt: "do it", Mode: ModeBackground})
	if err != nil {
		t.Fatal(err)
	}
	req := <-r.started
	if req.TaskID != task.ID || req.Generation != 1 {
		t.Fatalf("request=%#v", req)
	}
	r.finish(1, RunResult{Summary: "done", LegacyAlias: "legacy-1"})
	finished, err := s.Wait(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != StateCompleted || finished.Result.Summary != "done" || len(finished.LegacyAliases) != 1 {
		t.Fatalf("task=%#v", finished)
	}
	if got, ok := s.Get(task.ID); !ok || got.State != StateCompleted {
		t.Fatalf("Get=%#v %v", got, ok)
	}
	if got := s.List("s"); len(got) != 1 || got[0].ID != task.ID {
		t.Fatalf("List=%#v", got)
	}
}
func TestServicePersistsFailedRunnerResultWithPartialSummary(t *testing.T) {
	r := newControlledRunner()
	s := openService(t, r)
	task, err := s.Start(context.Background(), StartInput{IdempotencyKey: "failed-summary", Prompt: "review", Mode: ModeBackground})
	if err != nil {
		t.Fatal(err)
	}
	<-r.started
	r.finish(1, RunResult{Summary: "partial diagnostic", ErrorText: "final answer unusable", Err: errors.New("final answer unusable")})

	finished, err := s.Wait(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != StateFailed {
		t.Fatalf("runner error must persist as failed, got %#v", finished)
	}
	if finished.Result.Summary != "partial diagnostic" || finished.Result.Error != "final answer unusable" {
		t.Fatalf("failed result should retain diagnostics, got %#v", finished.Result)
	}
}

func TestServiceForegroundUsesSameTaskAndWaits(t *testing.T) {
	r := newControlledRunner()
	s := openService(t, r)
	done := make(chan Task, 1)
	errs := make(chan error, 1)
	go func() {
		task, err := s.Start(context.Background(), StartInput{IdempotencyKey: "fg", Prompt: "do", Mode: ModeForeground})
		done <- task
		errs <- err
	}()
	req := <-r.started
	r.finish(req.Generation, RunResult{Summary: "ok"})
	task := <-done
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if task.State != StateCompleted {
		t.Fatalf("task=%#v", task)
	}
}
func TestServiceStopCancelsRunningTask(t *testing.T) {
	r := newControlledRunner()
	s := openService(t, r)
	task, err := s.Start(context.Background(), StartInput{IdempotencyKey: "stop", Prompt: "do", Mode: ModeBackground})
	if err != nil {
		t.Fatal(err)
	}
	<-r.started
	stopped, err := s.Stop(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != StateCanceled {
		t.Fatalf("task=%#v", stopped)
	}
	waited, err := s.Wait(context.Background(), task.ID)
	if err != nil || waited.State != StateCanceled {
		t.Fatalf("wait=%#v %v", waited, err)
	}
}
func TestServiceResumeFencesLateCompletion(t *testing.T) {
	r := newControlledRunner()
	s := openService(t, r)
	first, _ := s.Start(context.Background(), StartInput{IdempotencyKey: "resume", Prompt: "first", Mode: ModeBackground})
	<-r.started
	if _, err := s.Stop(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := s.Resume(context.Background(), first.ID, "resume-key", "continue")
	if err != nil {
		t.Fatal(err)
	}
	req := <-r.started
	if req.Generation != 2 || second.Generation != 2 {
		t.Fatalf("request=%#v task=%#v", req, second)
	}
	r.finish(1, RunResult{Summary: "late"})
	r.finish(2, RunResult{Summary: "current"})
	finished, err := s.Wait(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Generation != 2 || finished.Result.Summary != "current" {
		t.Fatalf("task=%#v", finished)
	}
}
func TestServiceSendInputTargetsCurrentGeneration(t *testing.T) {
	r := newControlledRunner()
	s := openService(t, r)
	task, _ := s.Start(context.Background(), StartInput{IdempotencyKey: "input", Prompt: "do", Mode: ModeBackground})
	<-r.started
	if err := s.SendInput(context.Background(), task.ID, "more"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	if len(r.inputs) != 1 || r.inputs[0] != "more" {
		t.Fatalf("inputs=%v", r.inputs)
	}
}
func TestServiceWaitHonorsCancellation(t *testing.T) {
	r := newControlledRunner()
	s := openService(t, r)
	task, _ := s.Start(context.Background(), StartInput{IdempotencyKey: "cancel-wait", Prompt: "do", Mode: ModeBackground})
	<-r.started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Wait(ctx, task.ID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestSendInputRecordsTeamMailboxBeforeDelivery(t *testing.T) {
	ledger, _ := Open(filepath.Join(t.TempDir(), "tasks.json"), "lease")
	runner := newControlledRunner()
	journal := &fakeInputJournal{}
	service := NewService(ledger, runner)
	service.SetInputJournal(journal)
	task, err := service.Start(context.Background(), StartInput{IdempotencyKey: "start", SessionID: "s", TeamID: "team", MemberID: "member", Prompt: "work", Mode: ModeBackground})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	if err = service.SendInput(context.Background(), task.ID, "focus"); err != nil {
		t.Fatal(err)
	}
	if journal.task.ID != task.ID || journal.input != "focus" {
		t.Fatalf("journal = %#v, %q", journal.task, journal.input)
	}
	runner.finish(1, RunResult{Summary: "done"})
	if _, err = service.Wait(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
}

type fakeInputJournal struct {
	task  Task
	input string
	err   error
}

func (f *fakeInputJournal) RecordInput(_ context.Context, task Task, input string) error {
	f.task = task
	f.input = input
	return f.err
}

type launchResolverFunc func(context.Context, StartInput) (StartInput, error)

func (f launchResolverFunc) ResolveLaunch(ctx context.Context, in StartInput) (StartInput, error) {
	return f(ctx, in)
}

func TestServiceResolvesBackendBeforeDurableCreate(t *testing.T) {
	runner := newControlledRunner()
	service := openService(t, runner)
	service.SetLaunchResolver(launchResolverFunc(func(_ context.Context, in StartInput) (StartInput, error) {
		if in.TeamID != "team" || in.MemberID != "member" {
			t.Fatalf("resolver input = %#v", in)
		}
		in.Backend = "tmux"
		in.OwnershipID = "owner-token"
		return in, nil
	}))
	task, err := service.Start(context.Background(), StartInput{IdempotencyKey: "backend", SessionID: "s", OwnerID: "owner", TeamID: "team", MemberID: "member", Prompt: "work", Mode: ModeBackground})
	if err != nil {
		t.Fatal(err)
	}
	if task.Backend != "tmux" || task.BackendOwnershipID != "owner-token" {
		t.Fatalf("task = %#v", task)
	}
	req := <-runner.started
	if req.Backend != "tmux" || req.OwnershipID != "owner-token" || req.TeamID != "team" || req.MemberID != "member" {
		t.Fatalf("run request = %#v", req)
	}
}

func TestServiceDoesNotPersistTaskWhenLaunchResolutionFails(t *testing.T) {
	runner := newControlledRunner()
	service := openService(t, runner)
	service.SetLaunchResolver(launchResolverFunc(func(context.Context, StartInput) (StartInput, error) {
		return StartInput{}, errors.New("backend unavailable")
	}))
	if _, err := service.Start(context.Background(), StartInput{IdempotencyKey: "reject", SessionID: "s", Prompt: "work", Mode: ModeBackground}); err == nil {
		t.Fatal("expected launch resolution error")
	}
	if got := service.List("s"); len(got) != 0 {
		t.Fatalf("persisted tasks = %#v", got)
	}
}

func TestContinueInputResumesCompletedTaskWithSameIdentity(t *testing.T) {
	r := newControlledRunner()
	s := openService(t, r)
	task, err := s.Start(context.Background(), StartInput{IdempotencyKey: "start", Prompt: "first", Mode: ModeBackground})
	if err != nil {
		t.Fatal(err)
	}
	first := <-r.started
	r.finish(first.Generation, RunResult{Summary: "done"})
	if _, err := s.Wait(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}

	continued, err := s.ContinueInput(context.Background(), task.ID, "follow up", "tui-generation-2")
	if err != nil {
		t.Fatal(err)
	}
	if continued.ID != task.ID || continued.Generation != 2 {
		t.Fatalf("continued task = %#v", continued)
	}
	second := <-r.started
	if second.TaskID != task.ID || second.Generation != 2 || second.Prompt != "follow up" {
		t.Fatalf("resumed request = %#v", second)
	}
	r.finish(2, RunResult{Summary: "continued"})
	if _, err := s.Wait(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
}

func TestContinueInputSteersRunningTaskWithoutRestart(t *testing.T) {
	r := newControlledRunner()
	s := openService(t, r)
	task, err := s.Start(context.Background(), StartInput{IdempotencyKey: "start", Prompt: "first", Mode: ModeBackground})
	if err != nil {
		t.Fatal(err)
	}
	<-r.started
	continued, err := s.ContinueInput(context.Background(), task.ID, "steer", "unused")
	if err != nil {
		t.Fatal(err)
	}
	if continued.Generation != 1 {
		t.Fatalf("running task should not restart: %#v", continued)
	}
	r.mu.Lock()
	inputs := append([]string(nil), r.inputs...)
	r.mu.Unlock()
	if len(inputs) != 1 || inputs[0] != "steer" {
		t.Fatalf("inputs = %#v", inputs)
	}
	r.finish(1, RunResult{Summary: "done"})
	if _, err := s.Wait(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
}
