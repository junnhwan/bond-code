package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/junnhwan/bond-code/internal/fsx"
)

// KeepInResumeList reports whether a session should appear in /resume, the TUI
// session manager, and CLI session lists. Empty sessions (no user messages and
// no custom title) are hidden unless they are active or pinned — otherwise
// /clear and short-lived smoke runs accumulate noise.
func KeepInResumeList(active, pinned bool, customTitle string, userMessageCount int) bool {
	if active || pinned {
		return true
	}
	if userMessageCount > 0 {
		return true
	}
	return strings.TrimSpace(customTitle) != ""
}

// SessionPreview derives a quick identifier for a session from its audit log:
// the first user message (truncated), the user-message count, and the last
// event's timestamp. Shared by the /sessions command and the TUI session
// manager so they agree on what a session "is" at a glance.
func SessionPreview(store *JSONLStore, id string) (preview string, count int, lastActivity time.Time) {
	if store == nil {
		return "", 0, time.Time{}
	}
	events, err := store.Load(id)
	if err != nil || len(events) == 0 {
		return "", 0, time.Time{}
	}
	var firstUser string
	for _, ev := range events {
		if !ev.CreatedAt.IsZero() {
			lastActivity = ev.CreatedAt
		}
		if ev.Message != nil && ev.Message.Role == RoleUser {
			count++
			if firstUser == "" {
				firstUser = strings.TrimSpace(ev.Message.Content)
			}
		}
	}
	return truncatePreview(firstUser, 48), count, lastActivity
}

// truncatePreview caps a preview string to n visible runes with an ellipsis,
// first collapsing newlines so a multi-line first message reads as one line.
func truncatePreview(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 3 {
		return string(r[:n])
	}
	return string(r[:n-3]) + "..."
}

// Meta is the per-session sidecar metadata that does not belong in the audit
// event stream: a user-chosen title (overriding the first-message preview in
// session lists) and a pinned flag (pinned sessions sort to the top). It lives
// in <session-dir>/<id>.meta.json so rename/pin are O(1) and never touch the
// append-only audit log.
type Meta struct {
	Title  string `json:"title,omitempty"`
	Pinned bool   `json:"pinned,omitempty"`
}

// MetaPath returns the sidecar path for a session's metadata.
func (s *JSONLStore) MetaPath(sessionID string) string {
	return filepath.Join(s.dir, sessionID+".meta.json")
}

// LoadMeta reads a session's sidecar metadata. A missing file is not an error:
// it yields the zero Meta (no custom title, not pinned).
func (s *JSONLStore) LoadMeta(sessionID string) (Meta, error) {
	if err := validateSessionID(sessionID); err != nil {
		return Meta{}, err
	}
	data, err := os.ReadFile(s.MetaPath(sessionID))
	if os.IsNotExist(err) {
		return Meta{}, nil
	}
	if err != nil {
		return Meta{}, err
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return Meta{}, err
	}
	return meta, nil
}

// SaveMeta writes a session's sidecar metadata, creating the session dir if
// needed. An empty Meta clears the sidecar so the session reverts to the
// derived preview / unpinned defaults.
func (s *JSONLStore) SaveMeta(sessionID string, meta Meta) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	if meta.Title == "" && !meta.Pinned {
		err := os.Remove(s.MetaPath(sessionID))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(s.MetaPath(sessionID), append(data, '\n'), 0o600)
}
