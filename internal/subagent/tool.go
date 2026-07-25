package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"sync"

	"github.com/junnhwan/bond-code/internal/tool"
)

type TaskTool struct {
	manager *SubagentManager

	mu        sync.RWMutex
	sessionID string
}

func NewTaskTool(manager *SubagentManager) *TaskTool {
	return &TaskTool{manager: manager}
}

// BindSession scopes future child contexts and resume_task_id lookups to the
// active parent session. TaskTool is registered once and must follow hot
// session transitions instead of retaining bootstrap state.
func (t *TaskTool) BindSession(sessionID string) {
	t.mu.Lock()
	t.sessionID = sessionID
	t.mu.Unlock()
}

func (t *TaskTool) activeSessionID() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.sessionID
}

func (t *TaskTool) Name() string {
	return tool.Task
}

func (t *TaskTool) Description() string {
	return taskToolDescription()
}

// taskToolDescription is the delegation teaching for the task tool, ported from
// Claude Code's AgentTool prompt and localized to BondCode's toolset/profiles.
// It tells the main agent WHEN to delegate (and when not to), HOW to write a
// delegation prompt (the child starts with zero context — never delegate
// understanding), The profile list
// is sourced from AgentTypeListing() so it tracks the runtime profiles.
func taskToolDescription() string {
	guidance := `Launch a synchronous subagent with isolated context to handle a delegated task that is worth its own ReAct loop. The child runs with a restricted toolset and the SAME safety boundary as you (every tool call still goes through Policy + Confirmer), then returns ONE compact result. The user does not see the child's result — you must relay a summary back.

When to use:
- A bounded task that benefits from isolated context and several tool calls of its own — deep exploration, a well-specified implementation, or an independent review.
- You already know the file paths / line numbers / what to change well enough to hand the task off completely.

When NOT to use (do these yourself, faster):
- Reading one specific file → read_file.
- Finding files or code by a path/pattern you can name → search_text or list_dir.
- Trivial questions, direct answers, memory or todo updates, or anything doable in a single tool call.

Writing the prompt — the child has ZERO context:
- Brief it like a smart colleague who just walked into the room: what you're trying to accomplish, what you've already learned or ruled out, and enough background to make judgment calls.
- NEVER delegate understanding. Don't write "based on your findings, fix the bug" or "implement the research". Write prompts that prove you understood: include file paths, line numbers, and exactly what to change. Terse command-style prompts produce shallow, generic work.
- Lookups → hand the exact command. Investigations → hand the question, not prescribed steps (prescribed steps become dead weight when the premise is wrong).
- Say whether you want code changes or investigation only, and if you want a short answer, say so ("report in under 200 words").

Profiles (subagent_type — pick by what the work is):
` + AgentTypeListing() + `
Parallel vs sequential:
- 2-3 INDEPENDENT subtasks → send multiple task calls in ONE message (preferred) or use tasks[] with mode="parallel".
- Each task needs the previous one's output → mode="chain".

Resuming: set resume_task_id to a task_id a prior task call returned. The child continues that prior child's conversation (same profile, full history preserved) with your new prompt, instead of starting fresh. Use it to iterate on a sub-agent's work without re-explaining the context. Unknown id → error.`
	return guidance
}

func (t *TaskTool) Schema() any {
	taskItemSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"description": map[string]any{"type": "string"},
			"prompt": map[string]any{
				"type": "string",
			},
			"subagent_type": map[string]any{
				"type": "string",
				"enum": []string{string(AgentTypeResearch), string(AgentTypeCoder), string(AgentTypeReviewer)},
			},
			"task_id": map[string]any{"type": "string"},
		},
		"required": []string{"prompt"},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"description": map[string]any{
				"type":        "string",
				"description": "Short title for the delegated task.",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "Detailed task instructions for the isolated subagent.",
			},
			"subagent_type": map[string]any{
				"type":        "string",
				"enum":        []string{string(AgentTypeResearch), string(AgentTypeCoder), string(AgentTypeReviewer)},
				"description": "Subagent profile: research, coder, or reviewer.",
			},
			"task_id": map[string]any{
				"type":        "string",
				"description": "Optional stable task id for correlation. Echoed back so you can pass it as resume_task_id later.",
			},
			"resume_task_id": map[string]any{
				"type":        "string",
				"description": "Optional. Continue a prior child agent's conversation by passing the task_id it returned. The child resumes with its full history (same profile) and your new prompt. Only valid for single-task mode (not tasks[]). Unknown id returns an error.",
			},
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{string(TaskModeSingle), string(TaskModeParallel), string(TaskModeChain)},
				"description": "Optional orchestration mode for tasks: single, parallel, or chain.",
			},
			"tasks": map[string]any{
				"type":        "array",
				"description": "Optional batch tasks. If omitted, legacy description/prompt/subagent_type fields are used.",
				"minItems":    1,
				"items":       taskItemSchema,
			},
		},
		"oneOf": []map[string]any{
			{"required": []string{"prompt"}},
			{"required": []string{"tasks"}},
		},
	}
}

func (t *TaskTool) Risk(args json.RawMessage) tool.RiskLevel {
	return tool.RiskMedium
}

func (t *TaskTool) Execute(ctx context.Context, args json.RawMessage) (*tool.Result, error) {
	var parsed struct {
		Description  string    `json:"description"`
		Prompt       string    `json:"prompt"`
		SubagentType AgentType `json:"subagent_type"`
		TaskID       string    `json:"task_id"`
		ResumeTaskID string    `json:"resume_task_id"`
		Mode         TaskMode  `json:"mode"`
		Tasks        []struct {
			Description  string    `json:"description"`
			Prompt       string    `json:"prompt"`
			SubagentType AgentType `json:"subagent_type"`
			TaskID       string    `json:"task_id"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		return tool.ErrorResult(t.Name(), "invalid JSON arguments", err.Error()), nil
	}
	if t.manager == nil {
		return tool.ErrorResult(t.Name(), "subagent unavailable", "subagent manager is nil"), nil
	}
	sessionID := t.activeSessionID()

	if len(parsed.Tasks) > 0 {
		if strings.TrimSpace(parsed.ResumeTaskID) != "" {
			return tool.ErrorResult(t.Name(), "resume not supported in batch mode", "resume_task_id is not supported in batch mode (tasks[]); use a single task call to continue a prior child"), nil
		}
		request := BatchRequest{Mode: parsed.Mode, Tasks: make([]TaskRequest, 0, len(parsed.Tasks))}
		for _, item := range parsed.Tasks {
			if err := ValidateAgentType(item.SubagentType); err != nil {
				return tool.ErrorResult(t.Name(), "invalid subagent type", err.Error()), nil
			}
			if strings.TrimSpace(item.Prompt) == "" {
				return tool.ErrorResult(t.Name(), "missing task prompt", "prompt is required"), nil
			}
			request.Tasks = append(request.Tasks, TaskRequest{
				SessionID:    sessionID,
				Description:  item.Description,
				Prompt:       item.Prompt,
				SubagentType: item.SubagentType,
				TaskID:       item.TaskID,
			})
		}
		result, err := t.manager.RunBatch(ctx, request)
		if err != nil {
			return tool.ErrorResult(t.Name(), "subagent batch failed", err.Error()), nil
		}
		output := formatBatchResult(result, t.manager.resultCap())
		metadata := map[string]any{
			"mode":        string(result.Mode),
			"status":      result.Status,
			"child_count": len(result.Results),
			"batched":     true,
		}
		if result.Status != "completed" {
			envelope := tool.ErrorResult(t.Name(), "subagent batch failed", result.Error)
			envelope.Output = output
			envelope.Metadata = metadata
			return envelope, nil
		}
		envelope := tool.Success(t.Name(), "subagent batch completed", output)
		envelope.Metadata = metadata
		return envelope, nil
	}

	if err := ValidateAgentType(parsed.SubagentType); err != nil {
		return tool.ErrorResult(t.Name(), "invalid subagent type", err.Error()), nil
	}
	if strings.TrimSpace(parsed.Prompt) == "" {
		return tool.ErrorResult(t.Name(), "missing task prompt", "prompt is required"), nil
	}

	result, err := t.manager.RunTask(ctx, TaskRequest{
		SessionID:    sessionID,
		Description:  parsed.Description,
		Prompt:       parsed.Prompt,
		SubagentType: parsed.SubagentType,
		TaskID:       parsed.TaskID,
		ResumeTaskID: parsed.ResumeTaskID,
	})
	if err != nil {
		return tool.ErrorResult(t.Name(), "subagent failed", err.Error()), nil
	}

	output := formatTaskResult(result)
	metadata := map[string]any{
		"task_id":       result.TaskID,
		"subagent_type": string(result.AgentType),
		"status":        result.Status,
		"iterations":    result.Iterations,
	}
	if result.Status != "completed" {
		envelope := tool.ErrorResult(t.Name(), "subagent task failed", result.Error)
		envelope.Output = output
		envelope.Metadata = metadata
		return envelope, nil
	}
	envelope := tool.Success(t.Name(), "subagent task completed", output)
	envelope.Metadata = metadata
	return envelope, nil
}

func formatBatchResult(result *BatchResult, maxChars int) string {
	if result == nil {
		return `<task_result mode="" status="failed">` + "\n</task_result>"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<task_result mode="%s" status="%s">`,
		html.EscapeString(string(result.Mode)),
		html.EscapeString(result.Status),
	))
	for _, child := range result.Results {
		body := strings.TrimSpace(child.FinalAnswer)
		if body == "" {
			body = strings.TrimSpace(child.Error)
		}
		if body == "" {
			body = "No final answer returned."
		}
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf(`<child id="%s" type="%s" status="%s" iterations="%d">`,
			html.EscapeString(child.TaskID),
			html.EscapeString(string(child.AgentType)),
			html.EscapeString(child.Status),
			child.Iterations,
		))
		b.WriteString("\nSummary:\n")
		b.WriteString(body)
		b.WriteString("\n</child>")
	}
	b.WriteString("\n</task_result>")
	return capBatchOutput(b.String(), maxChars)
}

func capBatchOutput(output string, maxChars int) string {
	if maxChars <= 0 || len(output) <= maxChars {
		return output
	}
	prefix := output[:maxChars]
	if idx := strings.LastIndex(prefix, "\n"); idx > 0 {
		prefix = prefix[:idx]
	}
	prefix = strings.TrimRight(prefix, "\r\n")
	lines := []string{prefix, "[subagent result truncated]"}
	if lastOpen := strings.LastIndex(prefix, "<child "); lastOpen >= 0 {
		lastClose := strings.LastIndex(prefix, "</child>")
		if lastClose < lastOpen {
			lines = append(lines, "</child>")
		}
	}
	lines = append(lines, "</task_result>")
	return strings.Join(lines, "\n")
}

func formatTaskResult(result *SubagentResult) string {
	if result == nil {
		return `<task_result id="" type="" status="failed" iterations="0">` + "\nSummary:\nsubagent returned no result\n</task_result>"
	}
	body := strings.TrimSpace(result.FinalAnswer)
	if body == "" {
		body = strings.TrimSpace(result.Error)
	}
	if body == "" {
		body = "No final answer returned."
	}
	return fmt.Sprintf(`<task_result id="%s" type="%s" status="%s" iterations="%d">`+"\nSummary:\n%s\n</task_result>",
		html.EscapeString(result.TaskID),
		html.EscapeString(string(result.AgentType)),
		html.EscapeString(result.Status),
		result.Iterations,
		body,
	)
}

// SpawnTool implements the spawn tool for launching subagents
type SpawnTool struct {
	manager *SubagentManager

	mu        sync.RWMutex
	sessionID string
}

// NewSpawnTool creates a new SpawnTool
func NewSpawnTool(manager *SubagentManager, sessionID string) *SpawnTool {
	return &SpawnTool{
		manager:   manager,
		sessionID: sessionID,
	}
}

// BindSession moves future background tasks to the active session. SpawnTool is
// registered once for the process lifetime, so /resume, /new, and fork must
// explicitly rebind it instead of retaining the bootstrap session forever.
func (t *SpawnTool) BindSession(sessionID string) {
	t.mu.Lock()
	t.sessionID = sessionID
	t.mu.Unlock()
}

func (t *SpawnTool) activeSessionID() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.sessionID
}

// Name returns the tool name
func (t *SpawnTool) Name() string {
	return tool.Spawn
}

// Description returns the tool description
func (t *SpawnTool) Description() string {
	return "Spawn a fire-and-forget background subagent prototype. " +
		"It returns a task ID only; completed results are not returned to or injected into the main conversation yet."
}

// Schema returns the JSON schema for tool arguments
func (t *SpawnTool) Schema() any {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"prompt": map[string]interface{}{
				"type":        "string",
				"description": "The task prompt for the subagent",
			},
		},
		"required": []string{"prompt"},
	}
}

// Risk returns the risk level for this tool
func (t *SpawnTool) Risk(args json.RawMessage) tool.RiskLevel {
	return tool.RiskMedium // spawns background task
}

// Execute executes the spawn tool
func (t *SpawnTool) Execute(ctx context.Context, args json.RawMessage) (*tool.Result, error) {
	var parsed struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		return tool.ErrorResult(t.Name(), "invalid JSON arguments", err.Error()), nil
	}

	taskID, err := t.manager.Spawn(ctx, t.activeSessionID(), parsed.Prompt)
	if err != nil {
		return tool.ErrorResult(t.Name(), "failed to spawn subagent", err.Error()), nil
	}

	return tool.Success(t.Name(), "subagent spawned", fmt.Sprintf("Subagent spawned as fire-and-forget task ID: %s. Results are not injected into the main conversation yet.", taskID)), nil
}
