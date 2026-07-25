package builtin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/undo"
)

func observeFile(t *testing.T, store *ObservationStore, path string) []byte {
	t.Helper()
	token, err := store.BeginRead(path)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !store.CommitRead(token, content) {
		t.Fatal("CommitRead rejected current token")
	}
	return content
}

func TestObservationStoreCommitReadAndValidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewObservationStore()
	observeFile(t, store, path)
	err := store.GuardMutation(path, undo.NewStore(4), func(_ string, current []byte, exists bool) ([]byte, *undo.Snapshot, error) {
		if !exists || string(current) != "old" {
			t.Fatalf("current=%q exists=%v", current, exists)
		}
		return []byte("new"), nil, nil
	})
	if err != nil {
		t.Fatalf("GuardMutation: %v", err)
	}
}

func TestObservationStoreRejectsMissingObservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := NewObservationStore().GuardMutation(path, undo.NewStore(4), func(string, []byte, bool) ([]byte, *undo.Snapshot, error) {
		t.Fatal("mutation called")
		return nil, nil, nil
	})
	if !errors.Is(err, ErrNotObserved) {
		t.Fatalf("error=%v, want ErrNotObserved", err)
	}
}

func TestObservationStoreRejectsSameSizeByteChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewObservationStore()
	observeFile(t, store, path)
	if err := os.WriteFile(path, []byte("xyz"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := store.GuardMutation(path, undo.NewStore(4), func(string, []byte, bool) ([]byte, *undo.Snapshot, error) {
		t.Fatal("mutation called")
		return nil, nil, nil
	})
	if !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("error=%v, want ErrStaleObservation", err)
	}
}

func TestObservationStoreBindSessionInvalidatesDelayedRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewObservationStore()
	store.BindSession("one")
	token, err := store.BeginRead(path)
	if err != nil {
		t.Fatal(err)
	}
	store.BindSession("two")
	if store.CommitRead(token, []byte("abc")) {
		t.Fatal("delayed read crossed session epoch")
	}
}

func TestObservationStoreRepeatedBindIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewObservationStore()
	store.BindSession("one")
	observeFile(t, store, path)
	store.BindSession("one")
	if err := store.GuardMutation(path, undo.NewStore(4), func(string, []byte, bool) ([]byte, *undo.Snapshot, error) { return []byte("abc"), nil, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestObservationStoreGuardMutationFailureDoesNotRefreshOrPublishUndo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewObservationStore()
	observeFile(t, store, path)
	history := undo.NewStore(4)
	want := errors.New("failed")
	err := store.GuardMutation(path, history, func(string, []byte, bool) ([]byte, *undo.Snapshot, error) {
		return []byte("new"), &undo.Snapshot{Path: path, Old: []byte("abc")}, want
	})
	if !errors.Is(err, want) || history.Len() != 0 {
		t.Fatalf("error=%v len=%d", err, history.Len())
	}
}

func TestObservationStoreAllowsMtimeOnlyChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewObservationStore()
	observeFile(t, store, path)
	if err := os.Chtimes(path, time.Now().Add(-time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.GuardMutation(path, undo.NewStore(4), func(string, []byte, bool) ([]byte, *undo.Snapshot, error) { return []byte("abc"), nil, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestObservationStoreCanonicalizesExistingAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, ".", "sub", "..", "a.txt")
	store := NewObservationStore()
	observeFile(t, store, alias)
	if err := store.GuardMutation(path, undo.NewStore(4), func(string, []byte, bool) ([]byte, *undo.Snapshot, error) { return []byte("abc"), nil, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestObservationStoreCanonicalizesSymlinkedExistingAncestor(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	linkDir := filepath.Join(dir, "link")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	path := filepath.Join(realDir, "a.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewObservationStore()
	observeFile(t, store, filepath.Join(linkDir, "a.txt"))
	if err := store.GuardMutation(path, undo.NewStore(4), func(string, []byte, bool) ([]byte, *undo.Snapshot, error) { return []byte("abc"), nil, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestObservationStoreGuardMutationRefreshesAfterSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewObservationStore()
	observeFile(t, store, path)
	history := undo.NewStore(4)
	if err := store.GuardMutation(path, history, func(canonical string, current []byte, exists bool) ([]byte, *undo.Snapshot, error) {
		next := []byte("new")
		if err := os.WriteFile(canonical, next, 0o600); err != nil {
			return nil, nil, err
		}
		return next, &undo.Snapshot{Path: canonical, Old: current}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.GuardMutation(path, history, func(string, []byte, bool) ([]byte, *undo.Snapshot, error) { return []byte("new"), nil, nil }); err != nil {
		t.Fatalf("refreshed observation rejected: %v", err)
	}
}

func TestObservationStoreGuardMutationSerializesDifferentPaths(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store := NewObservationStore()
	observeFile(t, store, a)
	observeFile(t, store, b)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- store.GuardMutation(a, undo.NewStore(4), func(string, []byte, bool) ([]byte, *undo.Snapshot, error) {
			close(entered)
			<-release
			return []byte("x"), nil, nil
		})
	}()
	<-entered
	second := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- store.GuardMutation(b, undo.NewStore(4), func(string, []byte, bool) ([]byte, *undo.Snapshot, error) {
			close(second)
			return []byte("x"), nil, nil
		})
	}()
	select {
	case <-second:
		t.Fatal("different paths crossed observation boundary concurrently")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-second
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

func TestObservationStoreGuardMutationWaitsAcrossSessionClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewObservationStore()
	store.BindSession("one")
	observeFile(t, store, path)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- store.GuardMutation(path, undo.NewStore(4), func(string, []byte, bool) ([]byte, *undo.Snapshot, error) {
			close(entered)
			<-release
			return []byte("x"), nil, nil
		})
	}()
	<-entered
	bound := make(chan struct{})
	go func() { store.BindSession("two"); close(bound) }()
	select {
	case <-bound:
		t.Fatal("session clear crossed mutation boundary")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-bound
	err := store.GuardMutation(path, undo.NewStore(4), func(string, []byte, bool) ([]byte, *undo.Snapshot, error) { return nil, nil, nil })
	if !errors.Is(err, ErrNotObserved) {
		t.Fatalf("error=%v", err)
	}
}

func TestObservationStoreUndoBeforeFinalValidationFailsStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a")
	if err := os.WriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewObservationStore()
	observeFile(t, store, path)
	history := undo.NewStore(4)
	history.Record(path, []byte("old"))
	if _, err := history.Restore(func(snapshot undo.Snapshot) error { return os.WriteFile(snapshot.Path, snapshot.Old, 0o600) }); err != nil {
		t.Fatal(err)
	}
	called := false
	err := store.GuardMutation(path, history, func(string, []byte, bool) ([]byte, *undo.Snapshot, error) { called = true; return nil, nil, nil })
	if !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("error=%v, want stale", err)
	}
	if called {
		t.Fatal("mutation ran after undo invalidated observation")
	}
}
