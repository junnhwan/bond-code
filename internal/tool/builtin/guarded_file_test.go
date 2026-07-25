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

func executeArgs(t *testing.T, candidate tool.Tool, args any) error {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	_, err = candidate.Execute(context.Background(), raw)
	return err
}
func guardedTools(t *testing.T) (tool.Tool, tool.Tool, tool.Tool, *undo.Store) {
	t.Helper()
	observations := NewObservationStore()
	history := undo.NewStore(8)
	r, err := NewReadFileToolWithObservations(observations)
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewWriteFileToolWithObservations(observations, history)
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewEditFileToolWithObservations(observations, history)
	if err != nil {
		t.Fatal(err)
	}
	return r, w, e, history
}
func TestGuardedReadThenWriteSucceeds(t *testing.T) {
	r, w, _, h := guardedTools(t)
	p := filepath.Join(t.TempDir(), "a")
	os.WriteFile(p, []byte("old"), 0o600)
	if err := executeArgs(t, r, ReadFileInput{Path: p}); err != nil {
		t.Fatal(err)
	}
	if err := executeArgs(t, w, WriteFileInput{Path: p, Content: "new"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "new" || h.Len() != 1 {
		t.Fatalf("body=%q history=%d", b, h.Len())
	}
}
func TestGuardedWriteWithoutReadRejectsAndPreservesFile(t *testing.T) {
	_, w, _, h := guardedTools(t)
	p := filepath.Join(t.TempDir(), "a")
	os.WriteFile(p, []byte("old"), 0o600)
	if err := executeArgs(t, w, WriteFileInput{Path: p, Content: "new"}); err == nil {
		t.Fatal("expected rejection")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "old" || h.Len() != 0 {
		t.Fatalf("body=%q history=%d", b, h.Len())
	}
}
func TestGuardedWriteRejectsExternalChange(t *testing.T) {
	r, w, _, _ := guardedTools(t)
	p := filepath.Join(t.TempDir(), "a")
	os.WriteFile(p, []byte("one"), 0o600)
	executeArgs(t, r, ReadFileInput{Path: p})
	os.WriteFile(p, []byte("two"), 0o600)
	if err := executeArgs(t, w, WriteFileInput{Path: p, Content: "new"}); err == nil {
		t.Fatal("expected stale rejection")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "two" {
		t.Fatalf("body=%q", b)
	}
}
func TestGuardedRereadPermitsWriteAfterExternalChange(t *testing.T) {
	r, w, _, _ := guardedTools(t)
	p := filepath.Join(t.TempDir(), "a")
	os.WriteFile(p, []byte("one"), 0o600)
	executeArgs(t, r, ReadFileInput{Path: p})
	os.WriteFile(p, []byte("two"), 0o600)
	executeArgs(t, r, ReadFileInput{Path: p})
	if err := executeArgs(t, w, WriteFileInput{Path: p, Content: "new"}); err != nil {
		t.Fatal(err)
	}
}
func TestGuardedWriteCreatesNewFile(t *testing.T) {
	_, w, _, h := guardedTools(t)
	p := filepath.Join(t.TempDir(), "new", "a")
	if err := executeArgs(t, w, WriteFileInput{Path: p, Content: "new"}); err != nil {
		t.Fatal(err)
	}
	if h.Len() != 0 {
		t.Fatalf("history=%d", h.Len())
	}
}
func TestGuardedReadThenEditSucceeds(t *testing.T) {
	r, _, e, h := guardedTools(t)
	p := filepath.Join(t.TempDir(), "a")
	os.WriteFile(p, []byte("hello world"), 0o600)
	executeArgs(t, r, ReadFileInput{Path: p})
	if err := executeArgs(t, e, EditInput{Path: p, OldString: "world", NewString: "there"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "hello there" || h.Len() != 1 {
		t.Fatalf("body=%q history=%d", b, h.Len())
	}
}
func TestGuardedEditWithoutReadRejects(t *testing.T) {
	_, _, e, _ := guardedTools(t)
	p := filepath.Join(t.TempDir(), "a")
	os.WriteFile(p, []byte("hello"), 0o600)
	if err := executeArgs(t, e, EditInput{Path: p, OldString: "hello", NewString: "hi"}); err == nil {
		t.Fatal("expected rejection")
	}
}
func TestGuardedSuccessfulMutationRefreshesObservation(t *testing.T) {
	r, w, _, _ := guardedTools(t)
	p := filepath.Join(t.TempDir(), "a")
	os.WriteFile(p, []byte("a"), 0o600)
	executeArgs(t, r, ReadFileInput{Path: p})
	if err := executeArgs(t, w, WriteFileInput{Path: p, Content: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := executeArgs(t, w, WriteFileInput{Path: p, Content: "c"}); err != nil {
		t.Fatal(err)
	}
}
func TestGuardedConstructorsRejectNilDependencies(t *testing.T) {
	if _, err := NewReadFileToolWithObservations(nil); err == nil {
		t.Fatal("read accepted nil")
	}
	if _, err := NewWriteFileToolWithObservations(nil, undo.NewStore(1)); err == nil {
		t.Fatal("write accepted nil")
	}
	if _, err := NewEditFileToolWithObservations(NewObservationStore(), nil); err == nil {
		t.Fatal("edit accepted nil")
	}
}
func TestGuardedToolsBindSessionInvalidatesObservation(t *testing.T) {
	r, w, _, _ := guardedTools(t)
	p := filepath.Join(t.TempDir(), "a")
	os.WriteFile(p, []byte("a"), 0o600)
	r.(interface{ BindSession(string) }).BindSession("one")
	executeArgs(t, r, ReadFileInput{Path: p})
	w.(interface{ BindSession(string) }).BindSession("two")
	if err := executeArgs(t, w, WriteFileInput{Path: p, Content: "b"}); err == nil {
		t.Fatal("expected invalidation")
	}
}

func TestGuardedWriteDoesNotOverwriteConcurrentCreator(t *testing.T) {
	observations := NewObservationStore()
	history := undo.NewStore(4)
	candidate, err := NewWriteFileToolWithObservations(observations, history)
	if err != nil {
		t.Fatal(err)
	}
	write := candidate.(*writeFileTool)
	path := filepath.Join(t.TempDir(), "new.txt")
	write.openExclusive = func(name string, flag int, mode os.FileMode) (*os.File, error) {
		if err := os.WriteFile(name, []byte("external"), 0o600); err != nil {
			return nil, err
		}
		return os.OpenFile(name, flag, mode)
	}
	if err := executeArgs(t, write, WriteFileInput{Path: path, Content: "agent"}); err == nil {
		t.Fatal("expected concurrent creator rejection")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "external" {
		t.Fatalf("concurrent creator overwritten: %q", content)
	}
	if history.Len() != 0 {
		t.Fatalf("history=%d, want 0", history.Len())
	}
}
