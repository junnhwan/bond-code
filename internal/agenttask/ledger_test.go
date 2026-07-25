package agenttask

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLedgerCreateIsIdempotentAndDurable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	ledger, err := Open(path, "lease-a")
	if err != nil {
		t.Fatal(err)
	}
	input := CreateInput{IdempotencyKey: "request-1", SessionID: "s", Description: "inspect", Mode: ModeBackground, Kind: KindAgent}
	first, created, err := ledger.Create(input)
	if err != nil || !created {
		t.Fatalf("Create=%#v %v %v", first, created, err)
	}
	second, created, err := ledger.Create(input)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("duplicate=%#v %v %v", second, created, err)
	}
	reopened, err := Open(path, "lease-a")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.Get(first.ID)
	if !ok || got.ID != first.ID {
		t.Fatalf("replayed=%#v", got)
	}
}
func TestLedgerRejectsInvalidTransition(t *testing.T) {
	l, _ := Open(filepath.Join(t.TempDir(), "x"), "l")
	task, _, _ := l.Create(CreateInput{IdempotencyKey: "x", Kind: KindAgent, Mode: ModeBackground})
	_, err := l.Transition(task.ID, task.Generation, "l", StateCompleted, ResultRef{})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("error=%v", err)
	}
}
func TestLedgerRejectsStaleGenerationAndLease(t *testing.T) {
	l, _ := Open(filepath.Join(t.TempDir(), "x"), "l1")
	task, _, _ := l.Create(CreateInput{IdempotencyKey: "x", Kind: KindAgent, Mode: ModeBackground})
	running, err := l.Transition(task.ID, task.Generation, "l1", StateRunning, ResultRef{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.Transition(task.ID, running.Generation+1, "l1", StateCompleted, ResultRef{}); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("generation error=%v", err)
	}
	if _, err = l.Transition(task.ID, running.Generation, "l2", StateCompleted, ResultRef{}); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("lease error=%v", err)
	}
}
func TestLedgerNewLeaseInterruptsNonTerminalTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x")
	l, _ := Open(path, "old")
	task, _, _ := l.Create(CreateInput{IdempotencyKey: "x", Kind: KindAgent, Mode: ModeBackground})
	if _, err := l.Transition(task.ID, task.Generation, "old", StateRunning, ResultRef{}); err != nil {
		t.Fatal(err)
	}
	next, err := Open(path, "new")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := next.Get(task.ID)
	if got.State != StateInterrupted || got.RuntimeLease != "new" {
		t.Fatalf("task=%#v", got)
	}
}
func TestLedgerResumeIncrementsGeneration(t *testing.T) {
	l, _ := Open(filepath.Join(t.TempDir(), "x"), "l")
	task, _, _ := l.Create(CreateInput{IdempotencyKey: "x", Kind: KindAgent, Mode: ModeBackground})
	running, _ := l.Transition(task.ID, task.Generation, "l", StateRunning, ResultRef{})
	stopped, _ := l.Transition(task.ID, running.Generation, "l", StateCanceled, ResultRef{})
	resumed, err := l.Resume(stopped.ID, "resume-1")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Generation != stopped.Generation+1 || resumed.State != StateQueued {
		t.Fatalf("resumed=%#v", resumed)
	}
}

func TestLedgerQuarantinesCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(path, []byte(`{"payload":{},"checksum":"bad"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, "lease"); !errors.Is(err, ErrCorruptLedger) {
		t.Fatalf("error=%v", err)
	}
	matches, err := filepath.Glob(path + ".corrupt-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("quarantine files=%v", matches)
	}
}
func TestLedgerEventsAreMonotonic(t *testing.T) {
	l, _ := Open(filepath.Join(t.TempDir(), "x"), "l")
	task, _, _ := l.Create(CreateInput{IdempotencyKey: "x", Kind: KindAgent, Mode: ModeBackground})
	if _, err := l.Transition(task.ID, task.Generation, "l", StateRunning, ResultRef{}); err != nil {
		t.Fatal(err)
	}
	events := l.Events(0)
	if len(events) != 2 || events[0].Sequence >= events[1].Sequence || events[0].ID == events[1].ID {
		t.Fatalf("events=%#v", events)
	}
}
