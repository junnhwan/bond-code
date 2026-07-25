package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/command"
	"github.com/junnhwan/bond-code/internal/undo"
)

func TestUndoCommandRestoresPreWriteContent(t *testing.T) {
	undo.Default.Reset()
	defer undo.Default.Reset()

	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	undo.Default.Record(path, []byte("original"))
	// Simulate the agent overwriting it after the snapshot was recorded.
	if err := os.WriteFile(path, []byte("clobbered"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := UndoCommand().Run(context.Background(), command.Env{}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("expected original restored, got %q", got)
	}
	if !strings.Contains(res.Output, "reverted") {
		t.Fatalf("expected a reverted message, got %q", res.Output)
	}
	if undo.Default.Len() != 0 {
		t.Fatalf("expected the snapshot to be consumed, Len = %d", undo.Default.Len())
	}
}

func TestUndoCommandEmptyIsNoOp(t *testing.T) {
	undo.Default.Reset()
	defer undo.Default.Reset()

	res, err := UndoCommand().Run(context.Background(), command.Env{}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Output, "nothing to undo") {
		t.Fatalf("expected a nothing-to-undo message, got %q", res.Output)
	}
}

func TestUndoCommandHoldsStoreLockAcrossFilesystemRestore(t *testing.T) {
	store := undo.NewStore(4)
	store.Record("a", []byte("old"))
	writerEntered := make(chan struct{})
	releaseWriter := make(chan struct{})
	commandDone := make(chan error, 1)
	cmd := newUndoCommand(store, func(string, []byte, os.FileMode) error { close(writerEntered); <-releaseWriter; return nil })
	go func() { _, err := cmd.Run(context.Background(), command.Env{}, nil); commandDone <- err }()
	<-writerEntered
	applyEntered := make(chan struct{})
	applyDone := make(chan error, 1)
	go func() {
		applyDone <- store.Apply(func() (*undo.Snapshot, error) {
			close(applyEntered)
			return &undo.Snapshot{Path: "b", Old: []byte("before")}, nil
		})
	}()
	select {
	case <-applyEntered:
		t.Fatal("Apply entered while command restore was writing")
	default:
	}
	close(releaseWriter)
	if err := <-commandDone; err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-applyEntered
	if err := <-applyDone; err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func TestUndoCommandRestoreFailureDoesNotPop(t *testing.T) {
	store := undo.NewStore(4)
	store.Record("a", []byte("old"))
	wantErr := errors.New("write failed")
	cmd := newUndoCommand(store, func(string, []byte, os.FileMode) error { return wantErr })
	_, err := cmd.Run(context.Background(), command.Env{}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run error = %v, want %v", err, wantErr)
	}
	if got := store.Len(); got != 1 {
		t.Fatalf("Len = %d, want 1", got)
	}
}
