package contextx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSummaryStoreSaveAndLoad(t *testing.T) {
	store := NewSummaryStore(t.TempDir(), "session-1")
	want := SummaryArtifact{Version: 1, Summary: "important decisions"}

	if err := store.Save(want); err != nil {
		t.Fatalf("save summary: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("load summary: %v", err)
	}
	if got == nil || got.Summary != want.Summary {
		t.Fatalf("expected saved summary, got %#v", got)
	}
}

func TestSummaryStoreReturnsNilWhenMissing(t *testing.T) {
	got, err := NewSummaryStore(t.TempDir(), "missing").Load()
	if err != nil {
		t.Fatalf("load missing summary: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil summary, got %#v", got)
	}
}

func TestSummaryStoreRejectsUnsafeSessionID(t *testing.T) {
	err := NewSummaryStore(t.TempDir(), "../bad").Save(SummaryArtifact{Version: 1})
	if err == nil {
		t.Fatal("expected invalid session id error")
	}
}

func TestSummaryStoreWritesInsideContextDirectory(t *testing.T) {
	dir := t.TempDir()
	store := NewSummaryStore(dir, "session-1")
	if err := store.Save(SummaryArtifact{Version: 1, Summary: "safe"}); err != nil {
		t.Fatalf("save summary: %v", err)
	}
	path := filepath.Join(dir, "context-summaries", "session-1.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected summary at %s: %v", path, err)
	}
}
