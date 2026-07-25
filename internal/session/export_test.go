package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExportImportSession(t *testing.T) {
	dir := t.TempDir()
	store := NewJSONLStore(filepath.Join(dir, "sessions"))
	event := Event{
		SessionID: "s1",
		Type:      "message",
		Message:   &Message{Role: RoleUser, Content: "hello", CreatedAt: time.Now()},
		CreatedAt: time.Now(),
	}
	if err := store.Append(event); err != nil {
		t.Fatalf("append session: %v", err)
	}

	exportPath := filepath.Join(dir, "s1.export.jsonl")
	if err := store.Export("s1", exportPath); err != nil {
		t.Fatalf("export: %v", err)
	}

	importStore := NewJSONLStore(filepath.Join(dir, "imported"))
	if err := importStore.Import("s2", exportPath); err != nil {
		t.Fatalf("import: %v", err)
	}
	events, err := importStore.Load("s2")
	if err != nil {
		t.Fatalf("load imported: %v", err)
	}
	if len(events) != 1 || events[0].SessionID != "s2" || events[0].Message.Content != "hello" {
		t.Fatalf("unexpected imported events %#v", events)
	}
}

func TestForkSessionCopiesEventsWithNewID(t *testing.T) {
	dir := t.TempDir()
	store := NewJSONLStore(dir)
	if err := store.Append(Event{SessionID: "base", Type: "message", Message: &Message{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := store.Fork("base", "forked"); err != nil {
		t.Fatalf("fork: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "forked.jsonl")); err != nil {
		t.Fatalf("expected forked file: %v", err)
	}
}

func TestImportInvalidSourcePreservesExistingSession(t *testing.T) {
	dir := t.TempDir()
	store := NewJSONLStore(filepath.Join(dir, "sessions"))
	original := Event{
		SessionID: "target",
		Type:      "message",
		Message:   &Message{Role: RoleUser, Content: "keep me"},
	}
	if err := store.Append(original); err != nil {
		t.Fatalf("append original session: %v", err)
	}

	source := filepath.Join(dir, "invalid.jsonl")
	content := "{\"session_id\":\"source\",\"type\":\"message\"}\nnot-json\n"
	if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.Import("target", source); err == nil {
		t.Fatal("expected invalid import to fail")
	}
	events, err := store.Load("target")
	if err != nil {
		t.Fatalf("load preserved session: %v", err)
	}
	if len(events) != 1 || events[0].Message == nil || events[0].Message.Content != "keep me" {
		t.Fatalf("existing session was modified after failed import: %#v", events)
	}
}
