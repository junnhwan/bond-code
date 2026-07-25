package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/junnhwan/bond-code/internal/undo"
)

func runToolForTest(t *testing.T, tt tool.Tool, args map[string]any) *tool.Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	res, err := tt.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("execute %s: %v", tt.Name(), err)
	}
	return res
}

func writeExistingFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWriteFileRecordsExistingContentForUndo(t *testing.T) {
	undo.Default.Reset()
	defer undo.Default.Reset()
	path := filepath.Join(t.TempDir(), "a.txt")
	writeExistingFile(t, path, "before")

	runToolForTest(t, NewWriteFileTool(), map[string]any{"path": path, "content": "after"})

	snap := undo.Default.Pop()
	if snap == nil {
		t.Fatal("expected a pre-write snapshot to be recorded")
	}
	if snap.Path != path || string(snap.Old) != "before" {
		t.Fatalf("snapshot = %+v, want before at %s", snap, path)
	}
}

func TestWriteFileDoesNotRecordBrandNewFile(t *testing.T) {
	undo.Default.Reset()
	defer undo.Default.Reset()
	path := filepath.Join(t.TempDir(), "new.txt")

	runToolForTest(t, NewWriteFileTool(), map[string]any{"path": path, "content": "fresh"})

	if undo.Default.Peek() != nil {
		t.Fatal("a brand-new file has nothing to revert to; it must not record a snapshot")
	}
}

func TestEditFileRecordsPriorContentForUndo(t *testing.T) {
	undo.Default.Reset()
	defer undo.Default.Reset()
	path := filepath.Join(t.TempDir(), "b.txt")
	writeExistingFile(t, path, "hello world")

	runToolForTest(t, NewEditFileTool(), map[string]any{
		"path": path, "old_string": "world", "new_string": "there",
	})

	snap := undo.Default.Pop()
	if snap == nil {
		t.Fatal("expected a pre-edit snapshot to be recorded")
	}
	if string(snap.Old) != "hello world" {
		t.Fatalf("snapshot old = %q, want hello world", snap.Old)
	}
}
