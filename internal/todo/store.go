package todo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/junnhwan/bond-code/internal/fsx"
)

// Status is the Claude Code TodoWrite three-state checklist progress.
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
)

// Backward-compatible aliases used by older call sites and tests.
type TaskStatus = Status

const (
	TaskStatusPending    = StatusPending
	TaskStatusInProgress = StatusInProgress
	TaskStatusCompleted  = StatusCompleted
)

const (
	listFileName      = "todos.json"
	listSchemaVersion = 1
	maxActiveItems    = 30
)

// Task is one checklist item in the session todo list (Claude Code TodoWrite shape).
// Subject maps to CC "content"; ActiveForm is the present-continuous spinner label.
type Task struct {
	ID         string `json:"id"`
	Subject    string `json:"subject"`
	Status     Status `json:"status"`
	ActiveForm string `json:"active_form,omitempty"`
	// Owner is retained for TUI display compatibility; TodoWrite does not set it.
	Owner string `json:"owner,omitempty"`
}

// diskList is the on-disk document for one session's todo list.
type diskList struct {
	SchemaVersion int    `json:"schema_version"`
	Items         []Task `json:"items"`
}

// TaskStore is a session-scoped, file-backed todo list.
// Path: <baseDir>/tasks/<sessionID>/todos.json (single atomic file, whole-list replace).
type TaskStore struct {
	baseDir     string
	sessionsDir string
	mu          sync.Mutex
}

// NewSessionTaskStore binds a store to one session directory.
func NewSessionTaskStore(baseDir, sessionID string) (*TaskStore, error) {
	sessionsDir := filepath.Join(baseDir, "tasks")
	tasksDir, err := sessionTaskDir(sessionsDir, sessionID)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return nil, err
	}
	return &TaskStore{baseDir: tasksDir, sessionsDir: sessionsDir}, nil
}

// SwitchSession rebinds the store to another session directory without replacing
// the TaskStore pointer held by registered tools.
func (s *TaskStore) SwitchSession(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionsDir == "" {
		return fmt.Errorf("task store is not session-scoped")
	}
	tasksDir, err := sessionTaskDir(s.sessionsDir, sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return err
	}
	s.baseDir = tasksDir
	return nil
}

func sessionTaskDir(sessionsDir, sessionID string) (string, error) {
	if sessionID == "" || strings.ContainsAny(sessionID, `/\`) || sessionID == "." || sessionID == ".." {
		return "", fmt.Errorf("invalid session id %q", sessionID)
	}
	return filepath.Join(sessionsDir, sessionID), nil
}

// BaseDir returns the current session's task directory.
func (s *TaskStore) BaseDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.baseDir
}

func (s *TaskStore) listPath() string {
	return filepath.Join(s.baseDir, listFileName)
}

// List returns the current checklist (copy). Migrates legacy per-task JSON files once.
func (s *TaskStore) List() ([]*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	out := make([]*Task, len(items))
	for i := range items {
		cp := items[i]
		out[i] = &cp
	}
	return out, nil
}

// ReplaceAll validates and atomically replaces the whole list.
// Claude Code behavior: if every item is completed, the list is cleared.
func (s *TaskStore) ReplaceAll(tasks []Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized, err := normalizeItems(tasks)
	if err != nil {
		return err
	}
	return s.saveLocked(normalized)
}

// FormatForPrompt renders the checklist for DynamicReminder / todo_read summary.
func (s *TaskStore) FormatForPrompt() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.loadLocked()
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("# Tasks\n\n")
	for _, task := range items {
		line := fmt.Sprintf("%s %s %s: %s", StatusIcon(task.Status), task.Status, task.ID, task.Subject)
		if task.ActiveForm != "" && task.Status == StatusInProgress {
			line += fmt.Sprintf(" (%s)", task.ActiveForm)
		}
		sb.WriteString(line + "\n")
	}
	return sb.String(), nil
}

// Summary is a one-line planning status for /status and TUI live chrome.
func (s *TaskStore) Summary() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.loadLocked()
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", nil
	}
	var pending, inProgress, completed int
	for _, item := range items {
		switch item.Status {
		case StatusPending:
			pending++
		case StatusInProgress:
			inProgress++
		case StatusCompleted:
			completed++
		}
	}
	return fmt.Sprintf("%d tasks (%d pending, %d in_progress, %d completed)",
		len(items), pending, inProgress, completed), nil
}

// GraphSummary is a deprecated alias for Summary (call sites during migration).
func (s *TaskStore) GraphSummary() (string, error) {
	return s.Summary()
}

// StatusIcon is the single-character marker shared by prompt and TUI.
func StatusIcon(status Status) string {
	switch status {
	case StatusPending:
		return "○"
	case StatusInProgress:
		return "●"
	case StatusCompleted:
		return "✓"
	default:
		return "○"
	}
}

// TaskStatusIcon keeps the old name used by external packages.
func TaskStatusIcon(status Status) string {
	return StatusIcon(status)
}

func (s *TaskStore) loadLocked() ([]Task, error) {
	data, err := os.ReadFile(s.listPath())
	if err == nil {
		var doc diskList
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parse todo list: %w", err)
		}
		if doc.Items == nil {
			doc.Items = []Task{}
		}
		return doc.Items, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	// Legacy migration: older BondCode wrote one JSON file per task id.
	legacy, err := s.loadLegacyPerTaskFiles()
	if err != nil {
		return nil, err
	}
	if len(legacy) == 0 {
		return []Task{}, nil
	}
	if err := s.saveLocked(legacy); err != nil {
		return nil, err
	}
	return legacy, nil
}

func (s *TaskStore) loadLegacyPerTaskFiles() ([]Task, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var items []Task
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.Name() == listFileName {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.baseDir, entry.Name()))
		if err != nil {
			continue
		}
		var legacy struct {
			ID         string `json:"id"`
			Subject    string `json:"subject"`
			Status     Status `json:"status"`
			Owner      string `json:"owner"`
			ActiveForm string `json:"active_form"`
		}
		if err := json.Unmarshal(raw, &legacy); err != nil {
			continue
		}
		if legacy.ID == "" {
			legacy.ID = strings.TrimSuffix(entry.Name(), ".json")
		}
		// Drop cancelled / unknown statuses from the old DAG model.
		switch legacy.Status {
		case StatusPending, StatusInProgress, StatusCompleted:
		case "cancelled":
			continue
		default:
			if legacy.Status == "" {
				legacy.Status = StatusPending
			} else {
				continue
			}
		}
		if strings.TrimSpace(legacy.Subject) == "" {
			continue
		}
		items = append(items, Task{
			ID:         legacy.ID,
			Subject:    legacy.Subject,
			Status:     legacy.Status,
			ActiveForm: legacy.ActiveForm,
			Owner:      legacy.Owner,
		})
	}
	// Relax multi in_progress from legacy data so migration does not fail resume.
	inProgress := 0
	for i := range items {
		if items[i].Status == StatusInProgress {
			inProgress++
			if inProgress > 1 {
				items[i].Status = StatusPending
			}
		}
	}
	return items, nil
}

func (s *TaskStore) saveLocked(items []Task) error {
	if items == nil {
		items = []Task{}
	}
	doc := diskList{SchemaVersion: listSchemaVersion, Items: items}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return err
	}
	if err := fsx.WriteFileAtomic(s.listPath(), data, 0o644); err != nil {
		return err
	}
	// Best-effort cleanup of legacy per-task files after successful write.
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == listFileName || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		_ = os.Remove(filepath.Join(s.baseDir, entry.Name()))
	}
	return nil
}

func normalizeItems(tasks []Task) ([]Task, error) {
	if tasks == nil {
		tasks = []Task{}
	}
	normalized := make([]Task, len(tasks))
	copy(normalized, tasks)

	ids := make(map[string]struct{}, len(normalized))
	inProgress := 0
	active := 0
	allCompleted := len(normalized) > 0

	for i := range normalized {
		task := &normalized[i]
		if task.ID == "" {
			task.ID = fmt.Sprintf("%d", i+1)
		}
		if strings.ContainsAny(task.ID, `/\`) || task.ID == "." || task.ID == ".." {
			return nil, fmt.Errorf("invalid task id %q", task.ID)
		}
		if _, ok := ids[task.ID]; ok {
			return nil, fmt.Errorf("duplicate task id %q", task.ID)
		}
		ids[task.ID] = struct{}{}

		if strings.TrimSpace(task.Subject) == "" {
			return nil, fmt.Errorf("task %s subject is required", task.ID)
		}
		if task.Status == "" {
			task.Status = StatusPending
		}
		switch task.Status {
		case StatusPending, StatusInProgress:
			active++
			allCompleted = false
		case StatusCompleted:
			// keep
		case "cancelled":
			return nil, fmt.Errorf("task %s: cancelled is not supported; omit the item instead", task.ID)
		default:
			return nil, fmt.Errorf("task %s has invalid status %q", task.ID, task.Status)
		}
		if task.Status == StatusInProgress {
			inProgress++
		}
	}
	if active > maxActiveItems {
		return nil, fmt.Errorf("expected at most %d active tasks, got %d", maxActiveItems, active)
	}
	if inProgress > 1 {
		return nil, fmt.Errorf("only one in_progress task is allowed")
	}
	// Claude Code TodoWrite: closing out every item clears the list.
	if allCompleted {
		return []Task{}, nil
	}
	return normalized, nil
}
