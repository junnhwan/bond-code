package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/junnhwan/bond-code/internal/undo"
)

const maxReadFileBytes = 1 << 20

type ReadFileInput struct {
	Path string `json:"path"`
}

type WriteFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type ListDirInput struct {
	Path  string `json:"path"`
	Depth int    `json:"depth"`
}

type readFileTool struct{ observations *ObservationStore }

func NewReadFileTool() tool.Tool { return &readFileTool{} }
func NewReadFileToolWithObservations(store *ObservationStore) (tool.Tool, error) {
	if store == nil {
		return nil, fmt.Errorf("observation store is required")
	}
	return &readFileTool{observations: store}, nil
}
func (t *readFileTool) BindSession(sessionID string) {
	if t.observations != nil {
		t.observations.BindSession(sessionID)
	}
}

func (*readFileTool) Name() string { return tool.ReadFile }
func (*readFileTool) Description() string {
	return "Read a local text file when exact file contents are needed. Does not mutate files. Output is the file content, capped by the runtime read limit."
}
func (*readFileTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
		},
		"required": []string{"path"},
	}
}
func (*readFileTool) Risk(json.RawMessage) tool.RiskLevel { return tool.RiskLow }
func (t *readFileTool) Execute(ctx context.Context, raw json.RawMessage) (*tool.Result, error) {
	var input ReadFileInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if input.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	var observation ReadObservation
	if t.observations != nil {
		var err error
		observation, err = t.observations.BeginRead(input.Path)
		if err != nil {
			return nil, err
		}
	}
	info, err := os.Stat(input.Path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxReadFileBytes {
		return nil, fmt.Errorf("file exceeds max read size %d bytes", maxReadFileBytes)
	}
	b, err := os.ReadFile(input.Path)
	if err != nil {
		return nil, err
	}
	if t.observations != nil {
		t.observations.CommitRead(observation, b)
	}
	return tool.Success(t.Name(), fmt.Sprintf("read %d bytes from %s", len(b), input.Path), string(b)), nil
}

type writeFileTool struct {
	observations  *ObservationStore
	history       *undo.Store
	openExclusive func(string, int, os.FileMode) (*os.File, error)
}

func NewWriteFileTool() tool.Tool { return &writeFileTool{} }
func NewWriteFileToolWithObservations(store *ObservationStore, history *undo.Store) (tool.Tool, error) {
	if store == nil || history == nil {
		return nil, fmt.Errorf("observation and undo stores are required")
	}
	return &writeFileTool{observations: store, history: history, openExclusive: os.OpenFile}, nil
}
func (t *writeFileTool) BindSession(sessionID string) {
	if t.observations != nil {
		t.observations.BindSession(sessionID)
	}
}

func (*writeFileTool) Name() string { return tool.WriteFile }
func (*writeFileTool) Description() string {
	return "Create or overwrite a local file only when the requested file content is ready. Mutates the workspace. Output is a short write confirmation."
}
func (*writeFileTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		},
		"required": []string{"path", "content"},
	}
}
func (*writeFileTool) Risk(json.RawMessage) tool.RiskLevel { return tool.RiskMedium }
func (t *writeFileTool) Execute(ctx context.Context, raw json.RawMessage) (*tool.Result, error) {
	var input WriteFileInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if input.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if t.observations == nil {
		if err := os.MkdirAll(filepath.Dir(input.Path), 0o700); err != nil {
			return nil, err
		}
		if existing, rerr := os.ReadFile(input.Path); rerr == nil {
			undo.Default.Record(input.Path, existing)
		}
		if err := os.WriteFile(input.Path, []byte(input.Content), 0o600); err != nil {
			return nil, err
		}
	} else {
		next := []byte(input.Content)
		err := t.observations.GuardMutation(input.Path, t.history, func(canonical string, current []byte, exists bool) ([]byte, *undo.Snapshot, error) {
			if err := os.MkdirAll(filepath.Dir(canonical), 0o700); err != nil {
				return nil, nil, err
			}
			if !exists {
				f, err := t.openExclusive(canonical, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
				if err != nil {
					if errors.Is(err, os.ErrExist) {
						return nil, nil, fmt.Errorf("%w: %s; call read_file before retrying", ErrNotObserved, canonical)
					}
					return nil, nil, err
				}
				if _, err = f.Write(next); err == nil {
					err = f.Close()
				} else {
					_ = f.Close()
				}
				if err != nil {
					return nil, nil, err
				}
				return next, nil, nil
			}
			if err := os.WriteFile(canonical, next, 0o600); err != nil {
				return nil, nil, err
			}
			return next, &undo.Snapshot{Path: canonical, Old: current}, nil
		})
		if err != nil {
			return nil, guardedMutationError(input.Path, err)
		}
	}
	output := fmt.Sprintf("wrote %d bytes to %s", len(input.Content), input.Path)
	return tool.Success(t.Name(), "file written", output), nil
}

type EditInput struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

type editFileTool struct {
	observations *ObservationStore
	history      *undo.Store
}

func NewEditFileTool() tool.Tool { return &editFileTool{} }
func NewEditFileToolWithObservations(store *ObservationStore, history *undo.Store) (tool.Tool, error) {
	if store == nil || history == nil {
		return nil, fmt.Errorf("observation and undo stores are required")
	}
	return &editFileTool{observations: store, history: history}, nil
}
func (t *editFileTool) BindSession(sessionID string) {
	if t.observations != nil {
		t.observations.BindSession(sessionID)
	}
}

func (*editFileTool) Name() string { return tool.EditFile }
func (*editFileTool) Description() string {
	return "Edit a local file by replacing a unique old_string with new_string. Prefer this over write_file for targeted changes: it touches only the matched region, costs fewer tokens, and avoids re-sending the whole file. old_string must appear exactly once unless replace_all is set. Mutates the workspace."
}
func (*editFileTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string"},
			"old_string":  map[string]any{"type": "string"},
			"new_string":  map[string]any{"type": "string"},
			"replace_all": map[string]any{"type": "boolean"},
		},
		"required": []string{"path", "old_string", "new_string"},
	}
}
func (*editFileTool) Risk(json.RawMessage) tool.RiskLevel { return tool.RiskMedium }
func (t *editFileTool) Execute(ctx context.Context, raw json.RawMessage) (*tool.Result, error) {
	var input EditInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if input.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if input.OldString == "" {
		return nil, fmt.Errorf("old_string is required")
	}
	if input.OldString == input.NewString {
		return nil, fmt.Errorf("old_string and new_string are identical; nothing to change")
	}
	count := 0
	applyEdit := func(path string, content []byte) ([]byte, error) {
		body := string(content)
		count = strings.Count(body, input.OldString)
		if count == 0 {
			return nil, fmt.Errorf("old_string not found in %s; verify it matches the file exactly including whitespace and indentation", input.Path)
		}
		if !input.ReplaceAll && count > 1 {
			return nil, fmt.Errorf("old_string appears %d times in %s; include more surrounding context so it is unique, or set replace_all", count, input.Path)
		}
		if input.ReplaceAll {
			return []byte(strings.ReplaceAll(body, input.OldString, input.NewString)), nil
		}
		return []byte(strings.Replace(body, input.OldString, input.NewString, 1)), nil
	}
	if t.observations == nil {
		content, err := os.ReadFile(input.Path)
		if err != nil {
			return nil, err
		}
		updated, err := applyEdit(input.Path, content)
		if err != nil {
			return nil, err
		}
		undo.Default.Record(input.Path, content)
		if err := os.WriteFile(input.Path, updated, 0o600); err != nil {
			return nil, err
		}
	} else {
		err := t.observations.GuardMutation(input.Path, t.history, func(canonical string, current []byte, exists bool) ([]byte, *undo.Snapshot, error) {
			if !exists {
				return nil, nil, os.ErrNotExist
			}
			updated, err := applyEdit(canonical, current)
			if err != nil {
				return nil, nil, err
			}
			if err := os.WriteFile(canonical, updated, 0o600); err != nil {
				return nil, nil, err
			}
			return updated, &undo.Snapshot{Path: canonical, Old: current}, nil
		})
		if err != nil {
			return nil, guardedMutationError(input.Path, err)
		}
	}
	output := fmt.Sprintf("replaced %d occurrence(s) in %s\n%s", count, input.Path, diffPreview(input.OldString, input.NewString))
	return tool.Success(t.Name(), fmt.Sprintf("edited %s", input.Path), output), nil
}

// diffPreview renders a minimal +/- preview of an edit so the TUI's diff
// highlighter (renderDiffLine) can color the removed and added lines.
func diffPreview(old, new string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(old, "\n"), "\n") {
		b.WriteString("- " + line + "\n")
	}
	for _, line := range strings.Split(strings.TrimRight(new, "\n"), "\n") {
		b.WriteString("+ " + line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

type listDirTool struct{}

func NewListDirTool() tool.Tool { return listDirTool{} }

func (listDirTool) Name() string { return tool.ListDir }
func (listDirTool) Description() string {
	return "List local directory entries before choosing files to inspect. Does not mutate files. Output is a newline-separated directory tree."
}
func (listDirTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":  map[string]any{"type": "string"},
			"depth": map[string]any{"type": "integer"},
		},
		"required": []string{"path"},
	}
}
func (listDirTool) Risk(json.RawMessage) tool.RiskLevel { return tool.RiskLow }
func (t listDirTool) Execute(ctx context.Context, raw json.RawMessage) (*tool.Result, error) {
	var input ListDirInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if input.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	depth := input.Depth
	if depth <= 0 {
		depth = 1
	}
	var lines []string
	if err := walkDir(input.Path, depth, 0, &lines); err != nil {
		return nil, err
	}
	return tool.Success(t.Name(), "directory listed", strings.Join(lines, "\n")), nil
}

func walkDir(root string, maxDepth int, current int, lines *[]string) error {
	if current >= maxDepth {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		indent := strings.Repeat("  ", current)
		if entry.IsDir() {
			*lines = append(*lines, indent+name+"/")
			if err := walkDir(filepath.Join(root, name), maxDepth, current+1, lines); err != nil {
				return err
			}
			continue
		}
		*lines = append(*lines, indent+name)
	}
	return nil
}

func guardedMutationError(path string, err error) error {
	if errors.Is(err, ErrNotObserved) || errors.Is(err, ErrStaleObservation) {
		return fmt.Errorf("cannot modify %s because it was not read or changed since the last read; call read_file before retrying: %w", path, err)
	}
	return err
}
