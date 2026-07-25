package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/tool"
)

// TodoReadTool implements todo_read.
type TodoReadTool struct {
	store *TaskStore
}

func NewTodoReadTool(store *TaskStore) *TodoReadTool {
	return &TodoReadTool{store: store}
}

func (t *TodoReadTool) Name() string { return tool.TodoRead }

func (t *TodoReadTool) Description() string {
	return "Read the current session todo list. Use summary for a compact checklist or json for full item data."
}

func (t *TodoReadTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"format": map[string]any{
				"type":        "string",
				"enum":        []string{"summary", "json"},
				"description": "Output format. Defaults to summary.",
			},
		},
	}
}

func (t *TodoReadTool) Risk(args json.RawMessage) tool.RiskLevel {
	return tool.RiskLow
}

func (t *TodoReadTool) Execute(ctx context.Context, args json.RawMessage) (*tool.Result, error) {
	var parsed struct {
		Format string `json:"format"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &parsed); err != nil {
			return tool.ErrorResult(t.Name(), "invalid JSON arguments", err.Error()), nil
		}
	}
	switch parsed.Format {
	case "", "summary":
		output, err := t.store.FormatForPrompt()
		if err != nil {
			return tool.ErrorResult(t.Name(), "failed to read todos", err.Error()), nil
		}
		if output == "" {
			output = "No tasks."
		}
		return tool.Success(t.Name(), "todo summary read", output), nil
	case "json":
		tasks, err := t.store.List()
		if err != nil {
			return tool.ErrorResult(t.Name(), "failed to read todos", err.Error()), nil
		}
		payload := struct {
			Items []*Task `json:"items"`
		}{Items: tasks}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return tool.ErrorResult(t.Name(), "failed to encode todos", err.Error()), nil
		}
		return tool.Success(t.Name(), "todo JSON read", string(data)), nil
	default:
		return tool.ErrorResult(t.Name(), "invalid format", "format must be summary or json"), nil
	}
}

// TodoWriteTool implements todo_write (Claude Code TodoWrite: whole-list replace).
type TodoWriteTool struct {
	store *TaskStore
}

func NewTodoWriteTool(store *TaskStore) *TodoWriteTool {
	return &TodoWriteTool{store: store}
}

func (t *TodoWriteTool) Name() string { return tool.TodoWrite }

func (t *TodoWriteTool) Description() string {
	return "Create and manage the session todo checklist — use it PROACTIVELY to track multi-step work. " +
		"It replaces the WHOLE list in one call, so send the full updated list every time.\n\n" +
		"WHEN TO USE: (1) any task with 3+ steps; (2) the user gives several sub-tasks; (3) right after new instructions — capture them as todos immediately; (4) before non-trivial implementation; (5) when you discover follow-up work mid-task.\n\n" +
		"WHEN NOT TO USE: a single trivial task, a pure question, or anything doable in under 3 trivial steps — just do it.\n\n" +
		"DISCIPLINE: keep exactly ONE item in_progress at a time, and mark it in_progress BEFORE you begin. Mark completed the MOMENT it is truly done — do not batch status updates. Remove items that are no longer relevant. Write specific, actionable subjects (e.g. \"Fix auth nil-pointer in login.go\").\n\n" +
		"COMPLETION: only mark completed when fully done. If tests/build fail or work is partial, leave it in_progress and add a follow-up item. When every item is completed the list is cleared automatically."
}

func (t *TodoWriteTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{
				"type":        "array",
				"description": "Full replacement todo list.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type":        "string",
							"description": "Stable item id. Auto-assigned when omitted.",
						},
						"subject": map[string]any{
							"type":        "string",
							"description": "Actionable checklist title (Claude Code content).",
						},
						"status": map[string]any{
							"type": "string",
							"enum": []string{"pending", "in_progress", "completed"},
						},
						"active_form": map[string]any{
							"type":        "string",
							"description": "Present continuous form for UI while in_progress (e.g. \"Running tests\").",
						},
					},
					"required": []string{"subject"},
				},
			},
		},
		"required": []string{"items"},
	}
}

func (t *TodoWriteTool) Risk(args json.RawMessage) tool.RiskLevel {
	return tool.RiskLow
}

func (t *TodoWriteTool) Execute(ctx context.Context, args json.RawMessage) (*tool.Result, error) {
	var parsed struct {
		Items []Task `json:"items"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		return tool.ErrorResult(t.Name(), "invalid JSON arguments", err.Error()), nil
	}
	if err := t.store.ReplaceAll(parsed.Items); err != nil {
		return tool.ErrorResult(t.Name(), "failed to replace todos", err.Error()), nil
	}
	tasks, err := t.store.List()
	if err != nil {
		return tool.ErrorResult(t.Name(), "failed to read todos", err.Error()), nil
	}
	if len(tasks) == 0 {
		return tool.Success(t.Name(), "todo list cleared",
			"All items were completed (or the list was empty); the session todo list is now empty. "+
				"Start a new list with todo_write when the next multi-step task begins."), nil
	}
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	summary := fmt.Sprintf("todo_write saved %d items: %s", len(tasks), strings.Join(ids, ", "))
	echo := "Continue using this list: keep exactly one item in_progress, mark completed as soon as work is truly done, and never mark completed while tests/build fail or work is partial."
	if !anyInProgress(tasks) {
		echo = "List saved, but no item is in_progress — mark the next item in_progress before you start. " + echo
	}
	return tool.Success(t.Name(), "todo list replaced", summary+"\n\n"+echo), nil
}

func anyInProgress(tasks []*Task) bool {
	for _, task := range tasks {
		if task.Status == StatusInProgress {
			return true
		}
	}
	return false
}
