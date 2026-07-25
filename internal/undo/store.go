// Package undo holds the in-memory pre-write snapshots the /undo command
// restores from. The builtin file tools (write_file, edit_file) record a
// snapshot of a file's prior content immediately before overwriting it; /undo
// pops the most recent snapshot and writes the old content back. The store is
// process-wide (a singleton) so the write tools stay dependency-free `struct{}`
// while /undo, in a different package, can still reach the history.
package undo

import (
	"errors"
	"sync"
	"time"
)

// Snapshot is one restorable pre-write state.
type Snapshot struct {
	Path string
	Old  []byte
	When time.Time
}

// Mutation performs a filesystem mutation and returns the snapshot to publish.
type Mutation func() (*Snapshot, error)

// RestoreFunc restores one snapshot while the store transaction is locked.
type RestoreFunc func(Snapshot) error

// Store is a bounded LIFO of recent snapshots.
type Store struct {
	mu    sync.Mutex
	stack []Snapshot
	max   int
}

// NewStore returns a store that keeps at most max recent snapshots (min 1).
func NewStore(max int) *Store {
	if max <= 0 {
		max = 50
	}
	return &Store{max: max}
}

// Default is the singleton the file tools record into and /undo restores from.
var Default = NewStore(50)

// Record stashes the pre-write content of a path. No-op when old is nil (the
// file did not exist, so there is nothing to revert to) or when path is empty.
// The slice is copied so later mutation of the caller's buffer cannot corrupt
// the snapshot. Only the most recent `max` snapshots are kept.
func (s *Store) Record(path string, old []byte) {
	if s == nil || path == "" || old == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordLocked(Snapshot{Path: path, Old: old, When: time.Now()})
}

func (s *Store) recordLocked(snapshot Snapshot) {
	snapshot.Old = append([]byte(nil), snapshot.Old...)
	if snapshot.When.IsZero() {
		snapshot.When = time.Now()
	}
	s.stack = append(s.stack, snapshot)
	if len(s.stack) > s.max {
		s.stack = s.stack[len(s.stack)-s.max:]
	}
}

// Apply runs mutate and atomically publishes its snapshot only on success.
func (s *Store) Apply(mutate Mutation) error {
	if s == nil {
		return errors.New("undo store is nil")
	}
	if mutate == nil {
		return errors.New("undo mutation is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := mutate()
	if err != nil {
		return err
	}
	if snapshot != nil && snapshot.Path != "" && snapshot.Old != nil {
		s.recordLocked(*snapshot)
	}
	return nil
}

// Restore runs restore against the latest snapshot and consumes it only after success.
func (s *Store) Restore(restore RestoreFunc) (*Snapshot, error) {
	if s == nil {
		return nil, errors.New("undo store is nil")
	}
	if restore == nil {
		return nil, errors.New("undo restore callback is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.stack) == 0 {
		return nil, nil
	}
	snapshot := s.stack[len(s.stack)-1]
	callbackSnapshot := Snapshot{Path: snapshot.Path, Old: append([]byte(nil), snapshot.Old...), When: snapshot.When}
	if err := restore(callbackSnapshot); err != nil {
		return nil, err
	}
	s.stack = s.stack[:len(s.stack)-1]
	return &callbackSnapshot, nil
}

// Peek returns a copy of the most recent snapshot, or nil if empty. Does not
// remove it — call Pop to consume after a successful restore.
func (s *Store) Peek() *Snapshot {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.stack) == 0 {
		return nil
	}
	snap := s.stack[len(s.stack)-1]
	return &Snapshot{Path: snap.Path, Old: append([]byte(nil), snap.Old...), When: snap.When}
}

// Pop removes and returns the most recent snapshot, or nil if empty.
func (s *Store) Pop() *Snapshot {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.stack) == 0 {
		return nil
	}
	snap := s.stack[len(s.stack)-1]
	s.stack = s.stack[:len(s.stack)-1]
	return &snap
}

// Len returns the number of stored snapshots (mainly for tests/preview).
func (s *Store) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.stack)
}

// Reset clears the store. Intended for tests.
func (s *Store) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stack = nil
}
