package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/junnhwan/bond-code/internal/fsx"
)

type JSONLStore struct {
	dir string
}

func NewJSONLStore(dir string) *JSONLStore {
	return &JSONLStore{dir: dir}
}

func (s *JSONLStore) Append(event Event) error {
	if event.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if event.EventID == "" {
		event.EventID = newEventID()
	}
	if err := validateSessionID(event.SessionID); err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path(event.SessionID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *JSONLStore) Load(sessionID string) ([]Event, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}
	f, err := os.Open(s.path(sessionID))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []Event
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if line = strings.TrimRight(line, "\n"); line != "" {
			var event Event
			if jsonErr := json.Unmarshal([]byte(line), &event); jsonErr != nil {
				return nil, jsonErr
			}
			events = append(events, event)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}
	return events, nil
}

func (s *JSONLStore) List() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		id, ok := sessionIDFromJSONLName(entry.Name())
		if !ok {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// sessionIDFromJSONLName maps a directory entry name to a session id when it is
// a real audit log (sessionID.jsonl). Sidecars that also end in .jsonl — notably
// opt-in debug traces named sessionID.debug.jsonl — must not appear as sessions
// in /resume, CLI session list, or the TUI session manager.
func sessionIDFromJSONLName(name string) (string, bool) {
	if !strings.HasSuffix(name, ".jsonl") {
		return "", false
	}
	// Debug traces live beside audit logs as <id>.debug.jsonl. filepath.Ext only
	// sees the final .jsonl, so without this guard they become fake sessions
	// titled "(no messages)".
	if strings.HasSuffix(name, ".debug.jsonl") {
		return "", false
	}
	id := strings.TrimSuffix(name, ".jsonl")
	// Production ids embed a UTC timestamp with a fractional second
	// (session-20060102-150405.000000000), so dots inside the id are normal.
	// Only known non-audit suffixes (currently .debug) are excluded above.
	if err := validateSessionID(id); err != nil {
		return "", false
	}
	return id, true
}

func (s *JSONLStore) Delete(sessionID string) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	err := os.Remove(s.path(sessionID))
	if os.IsNotExist(err) {
		err = nil
	}
	// Best-effort: clear sidecars so delete fully removes the session surface
	// (meta title/pin, resumable history snapshot, opt-in debug trace).
	for _, p := range []string{s.MetaPath(sessionID), s.HistoryPath(sessionID), s.DebugPath(sessionID)} {
		if mErr := os.Remove(p); mErr != nil && !os.IsNotExist(mErr) && err == nil {
			err = mErr
		}
	}
	return err
}

func (s *JSONLStore) path(sessionID string) string {
	return filepath.Join(s.dir, sessionID+".jsonl")
}

// DebugPath returns the opt-in debug trace path for a session
// (<session-dir>/<id>.debug.jsonl). Owned here so List/Delete share one naming
// convention with observe.NewDebugFileLogger.
func (s *JSONLStore) DebugPath(sessionID string) string {
	return filepath.Join(s.dir, sessionID+".debug.jsonl")
}

// HistoryPath returns the path to the resumable history snapshot for a session.
// The snapshot is opaque bytes (the app layer marshals its message list); the
// session package only owns the path and file I/O convention.
func (s *JSONLStore) HistoryPath(sessionID string) string {
	return filepath.Join(s.dir, sessionID+".history.json")
}

// WriteHistory writes a resumable history snapshot for a session.
func (s *JSONLStore) WriteHistory(sessionID string, data []byte) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	return fsx.WriteFileAtomic(s.HistoryPath(sessionID), data, 0o600)
}

// ReadHistory reads a resumable history snapshot. It returns nil data (no
// error) when no snapshot exists yet.
func (s *JSONLStore) ReadHistory(sessionID string) ([]byte, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.HistoryPath(sessionID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

func validateSessionID(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if strings.ContainsAny(sessionID, `/\`) || sessionID == "." || sessionID == ".." {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	return nil
}

var lastEventIDUnixNano atomic.Int64

// newEventID returns a process-unique, time-shaped event ID. UnixNano alone is
// not unique on platforms whose wall clock advances at a coarser resolution
// (notably Windows), so equal or backward timestamps advance monotonically.
func newEventID() string {
	now := time.Now().UnixNano()
	for {
		last := lastEventIDUnixNano.Load()
		next := now
		if next <= last {
			next = last + 1
		}
		if lastEventIDUnixNano.CompareAndSwap(last, next) {
			return fmt.Sprintf("evt_%d", next)
		}
	}
}
