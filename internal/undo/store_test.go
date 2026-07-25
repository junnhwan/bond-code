package undo

import (
	"errors"
	"testing"
)

func TestStoreRecordPeekPop(t *testing.T) {
	s := NewStore(8)
	if s.Peek() != nil {
		t.Fatal("expected nil peek on an empty store")
	}
	s.Record("a.txt", []byte("old-a"))
	s.Record("b.txt", []byte("old-b"))

	top := s.Peek()
	if top == nil || top.Path != "b.txt" || string(top.Old) != "old-b" {
		t.Fatalf("peek top = %#v, want b.txt/old-b", top)
	}
	popped := s.Pop()
	if popped == nil || popped.Path != "b.txt" {
		t.Fatalf("pop top = %#v, want b.txt", popped)
	}
	if got := s.Peek(); got == nil || got.Path != "a.txt" {
		t.Fatalf("after pop want a.txt on top, got %#v", got)
	}
}

func TestStoreRecordNilOrEmptyIsNoOp(t *testing.T) {
	s := NewStore(4)
	s.Record("", []byte("x"))
	s.Record("a.txt", nil)
	if s.Peek() != nil {
		t.Fatal("expected nil after no-op records")
	}
}

func TestStoreBoundsToMax(t *testing.T) {
	s := NewStore(2)
	s.Record("a", []byte("1"))
	s.Record("b", []byte("2"))
	s.Record("c", []byte("3"))
	if s.Len() != 2 {
		t.Fatalf("Len = %d, want 2", s.Len())
	}
	if top := s.Peek(); top == nil || top.Path != "c" {
		t.Fatalf("top = %#v, want c (most recent)", top)
	}
}

func TestStoreSnapshotIsACopy(t *testing.T) {
	s := NewStore(4)
	buf := []byte("original")
	s.Record("a", buf)
	peeked := s.Peek()
	buf[0] = 'X'
	if string(peeked.Old) != "original" {
		t.Fatalf("snapshot should be a copy, got mutated %q", peeked.Old)
	}
}

func TestStoreApplyPublishesSnapshotOnlyAfterSuccess(t *testing.T) {
	s := NewStore(4)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.Apply(func() (*Snapshot, error) {
			close(entered)
			<-release
			return &Snapshot{Path: "a", Old: []byte("old")}, nil
		})
	}()
	<-entered
	peekDone := make(chan *Snapshot, 1)
	go func() { peekDone <- s.Peek() }()
	select {
	case <-peekDone:
		t.Fatal("snapshot became observable before mutation completed")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := <-peekDone; got == nil || got.Path != "a" {
		t.Fatalf("Peek = %#v, want a", got)
	}
}

func TestStoreApplyFailurePublishesNoSnapshot(t *testing.T) {
	s := NewStore(4)
	wantErr := errors.New("mutation failed")
	err := s.Apply(func() (*Snapshot, error) { return &Snapshot{Path: "a", Old: []byte("old")}, wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("Apply error = %v, want %v", err, wantErr)
	}
	if got := s.Len(); got != 0 {
		t.Fatalf("Len = %d, want 0", got)
	}
}

func TestStoreRestoreHoldsLockAcrossWriteAndPop(t *testing.T) {
	s := NewStore(4)
	s.Record("a", []byte("old"))
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := s.Restore(func(Snapshot) error { close(entered); <-release; return nil })
		done <- err
	}()
	<-entered
	lenDone := make(chan int, 1)
	go func() { lenDone <- s.Len() }()
	select {
	case <-lenDone:
		t.Fatal("store lock was released during restore")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := <-lenDone; got != 0 {
		t.Fatalf("Len = %d, want 0", got)
	}
}

func TestStoreApplyWaitsForConcurrentRestore(t *testing.T) {
	s := NewStore(4)
	s.Record("a", []byte("old"))
	restoreEntered := make(chan struct{})
	releaseRestore := make(chan struct{})
	restoreDone := make(chan error, 1)
	go func() {
		_, err := s.Restore(func(Snapshot) error { close(restoreEntered); <-releaseRestore; return nil })
		restoreDone <- err
	}()
	<-restoreEntered
	applyEntered := make(chan struct{})
	applyDone := make(chan error, 1)
	go func() {
		applyDone <- s.Apply(func() (*Snapshot, error) {
			close(applyEntered)
			return &Snapshot{Path: "b", Old: []byte("before")}, nil
		})
	}()
	select {
	case <-applyEntered:
		t.Fatal("Apply entered while Restore held the lock")
	default:
	}
	close(releaseRestore)
	if err := <-restoreDone; err != nil {
		t.Fatalf("Restore: %v", err)
	}
	<-applyEntered
	if err := <-applyDone; err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func TestStoreSuccessfulApplyOrderDefinesLIFO(t *testing.T) {
	s := NewStore(4)
	for _, path := range []string{"a", "b"} {
		path := path
		if err := s.Apply(func() (*Snapshot, error) { return &Snapshot{Path: path, Old: []byte(path)}, nil }); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.Pop(); got == nil || got.Path != "b" {
		t.Fatalf("Pop = %#v, want b", got)
	}
	if got := s.Pop(); got == nil || got.Path != "a" {
		t.Fatalf("Pop = %#v, want a", got)
	}
}
