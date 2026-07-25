package builtin

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/junnhwan/bond-code/internal/undo"
)

var (
	ErrNotObserved      = errors.New("file has not been observed")
	ErrStaleObservation = errors.New("file changed since it was observed")
)

type fileObservation struct {
	digest [sha256.Size]byte
	size   int
}

type ObservationStore struct {
	mu        sync.Mutex
	epoch     uint64
	sessionID string
	entries   map[string]fileObservation
}

type ReadObservation struct {
	epoch uint64
	path  string
}

func NewObservationStore() *ObservationStore {
	return &ObservationStore{entries: make(map[string]fileObservation)}
}

func (s *ObservationStore) BeginRead(path string) (ReadObservation, error) {
	if s == nil {
		return ReadObservation{}, errors.New("observation store is nil")
	}
	canonical, err := canonicalFilePath(path)
	if err != nil {
		return ReadObservation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return ReadObservation{epoch: s.epoch, path: canonical}, nil
}

func (s *ObservationStore) CommitRead(token ReadObservation, content []byte) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if token.path == "" || token.epoch != s.epoch {
		return false
	}
	s.entries[token.path] = observationOf(content)
	return true
}

func (s *ObservationStore) BindSession(sessionID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionID == sessionID {
		return
	}
	s.sessionID = sessionID
	s.epoch++
	s.entries = make(map[string]fileObservation)
}

func (s *ObservationStore) GuardMutation(path string, history *undo.Store, mutate func(string, []byte, bool) ([]byte, *undo.Snapshot, error)) error {
	if s == nil {
		return errors.New("observation store is nil")
	}
	if history == nil {
		return errors.New("undo store is nil")
	}
	if mutate == nil {
		return errors.New("mutation callback is nil")
	}
	canonical, err := canonicalFilePath(path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var updated []byte
	err = history.Apply(func() (*undo.Snapshot, error) {
		current, readErr := os.ReadFile(canonical)
		exists := readErr == nil
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return nil, readErr
		}
		if exists {
			observed, ok := s.entries[canonical]
			if !ok {
				return nil, fmt.Errorf("%w: %s", ErrNotObserved, canonical)
			}
			if got := observationOf(current); got != observed {
				return nil, fmt.Errorf("%w: %s", ErrStaleObservation, canonical)
			}
		}
		var snapshot *undo.Snapshot
		var mutationErr error
		updated, snapshot, mutationErr = mutate(canonical, current, exists)
		if mutationErr != nil {
			return nil, mutationErr
		}
		return snapshot, nil
	})
	if err != nil {
		return err
	}
	s.entries[canonical] = observationOf(updated)
	return nil
}

func observationOf(content []byte) fileObservation {
	return fileObservation{digest: sha256.Sum256(content), size: len(content)}
}

func canonicalFilePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("file path is empty")
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	ancestor := absolute
	var suffix []string
	for {
		_, statErr := os.Lstat(ancestor)
		if statErr == nil {
			break
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
	resolved, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, suffix[i])
	}
	return filepath.Clean(resolved), nil
}
