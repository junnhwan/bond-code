package agenttask

import (
	"os"
	"path/filepath"
	"testing"
)

func failingLedger(t *testing.T) *Ledger {
	t.Helper()
	root := t.TempDir()
	block := filepath.Join(root, "block")
	if err := os.WriteFile(block, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Ledger{path: filepath.Join(block, "ledger.json"), state: diskState{SchemaVersion: ledgerSchemaVersion, Lease: "lease", Tasks: map[string]Task{}, Idempotency: map[string]string{}}}
}

func TestCreatePersistenceFailureDoesNotMutateMemory(t *testing.T) {
	l := failingLedger(t)
	task, created, err := l.Create(CreateInput{IdempotencyKey: "one"})
	if err == nil || created {
		t.Fatalf("task=%+v created=%v err=%v", task, created, err)
	}
	if got := l.List(""); len(got) != 0 {
		t.Fatalf("memory advanced after failed persist: %+v", got)
	}
	if l.state.Sequence != 0 || len(l.state.Events) != 0 || len(l.state.Idempotency) != 0 {
		t.Fatalf("state advanced: %+v", l.state)
	}
}

func TestTransitionPersistenceFailureDoesNotMutateMemory(t *testing.T) {
	l := failingLedger(t)
	task := Task{ID: "task-1", State: StateQueued, Generation: 1, RuntimeLease: "lease"}
	l.state.Tasks[task.ID] = task
	if _, err := l.Transition(task.ID, 1, "lease", StateRunning, ResultRef{}); err == nil {
		t.Fatal("expected persist error")
	}
	got, _ := l.Get(task.ID)
	if got.State != StateQueued || got.EventSequence != 0 || l.state.Sequence != 0 {
		t.Fatalf("state advanced: %+v sequence=%d", got, l.state.Sequence)
	}
}

func TestAliasPersistenceFailureDoesNotMutateMemory(t *testing.T) {
	l := failingLedger(t)
	l.state.Tasks["task-1"] = Task{ID: "task-1", Generation: 1, RuntimeLease: "lease"}
	if err := l.AddLegacyAlias("task-1", 1, "lease", "legacy"); err == nil {
		t.Fatal("expected persist error")
	}
	got, _ := l.Get("task-1")
	if len(got.LegacyAliases) != 0 {
		t.Fatalf("aliases advanced: %+v", got.LegacyAliases)
	}
}

func TestResumePersistenceFailureDoesNotMutateMemory(t *testing.T) {
	l := failingLedger(t)
	l.state.Tasks["task-1"] = Task{ID: "task-1", State: StateCompleted, Generation: 1, RuntimeLease: "lease"}
	if _, err := l.Resume("task-1", "resume-1", "new prompt"); err == nil {
		t.Fatal("expected persist error")
	}
	got, _ := l.Get("task-1")
	if got.State != StateCompleted || got.Generation != 1 || got.Prompt != "" || l.state.Sequence != 0 {
		t.Fatalf("state advanced: %+v sequence=%d", got, l.state.Sequence)
	}
}
