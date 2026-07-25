package agenttask

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/junnhwan/bond-code/internal/tool"
)

type toolScope struct {
	mu        sync.RWMutex
	sessionID string
}

func (s *toolScope) bind(id string)  { s.mu.Lock(); s.sessionID = id; s.mu.Unlock() }
func (s *toolScope) session() string { s.mu.RLock(); defer s.mu.RUnlock(); return s.sessionID }

type lifecycleTool struct {
	name, description string
	risk              tool.RiskLevel
	schema            any
	scope             *toolScope
	execute           func(context.Context, json.RawMessage) (any, error)
}

func (t *lifecycleTool) Name() string                        { return t.name }
func (t *lifecycleTool) Description() string                 { return t.description }
func (t *lifecycleTool) Schema() any                         { return t.schema }
func (t *lifecycleTool) Risk(json.RawMessage) tool.RiskLevel { return t.risk }
func (t *lifecycleTool) BindSession(id string)               { t.scope.bind(id) }
func (t *lifecycleTool) Execute(ctx context.Context, raw json.RawMessage) (*tool.Result, error) {
	value, err := t.execute(ctx, raw)
	if err != nil {
		return tool.ErrorResult(t.name, "agent task operation failed", err.Error()), nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return tool.Success(t.name, "agent task operation completed", string(data)), nil
}

type BackendView struct {
	Backend string `json:"backend"`
	State   string `json:"state"`
	Healthy bool   `json:"healthy"`
	Detail  string `json:"detail,omitempty"`
	Attach  bool   `json:"attach"`
	Show    bool   `json:"show"`
	Hide    bool   `json:"hide"`
}

type TaskBackendController interface {
	BackendStatus(context.Context, string, uint64) (BackendView, error)
	BackendAttach(context.Context, string, uint64) error
	BackendShow(context.Context, string, uint64) error
	BackendHide(context.Context, string, uint64) error
}

func LifecycleTools(service *Service, sessionID string) []tool.Tool {
	return LifecycleToolsWithBackend(service, sessionID, nil)
}

func LifecycleToolsWithBackend(service *Service, sessionID string, controller TaskBackendController) []tool.Tool {
	scope := &toolScope{sessionID: sessionID}
	object := func(properties map[string]any, required ...string) any {
		schema := map[string]any{"type": "object", "properties": properties}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	}
	start := &lifecycleTool{name: "agent_task", description: "Start a foreground or background child-agent task and return its stable task ID and state.", risk: tool.RiskMedium, scope: scope, schema: object(map[string]any{"description": map[string]any{"type": "string"}, "prompt": map[string]any{"type": "string"}, "profile": map[string]any{"type": "string", "enum": []string{"research", "coder", "reviewer"}}, "mode": map[string]any{"type": "string", "enum": []string{"foreground", "background"}}, "idempotency_key": map[string]any{"type": "string"}, "team_id": map[string]any{"type": "string"}, "member_id": map[string]any{"type": "string"}, "backend": map[string]any{"type": "string", "enum": []string{"in_process", "tmux", "iterm"}}}, "prompt"), execute: func(ctx context.Context, raw json.RawMessage) (any, error) {
		var in struct {
			Description    string `json:"description"`
			Prompt         string `json:"prompt"`
			Profile        string `json:"profile"`
			Mode           Mode   `json:"mode"`
			IdempotencyKey string `json:"idempotency_key"`
			TeamID         string `json:"team_id"`
			MemberID       string `json:"member_id"`
			Backend        string `json:"backend"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.Prompt) == "" {
			return nil, fmt.Errorf("prompt is required")
		}
		if in.IdempotencyKey == "" {
			in.IdempotencyKey = newID("request")
		}
		return service.Start(ctx, StartInput{IdempotencyKey: in.IdempotencyKey, SessionID: scope.session(), OwnerID: "session:" + scope.session(), TeamID: in.TeamID, MemberID: in.MemberID, Description: in.Description, Prompt: in.Prompt, Profile: in.Profile, Backend: in.Backend, Mode: in.Mode})
	}}
	output := &lifecycleTool{name: "task_output", description: "Get or wait for a task's current state and durable result.", risk: tool.RiskLow, scope: scope, schema: object(map[string]any{"task_id": map[string]any{"type": "string"}, "wait": map[string]any{"type": "boolean"}}, "task_id"), execute: func(ctx context.Context, raw json.RawMessage) (any, error) {
		var in struct {
			TaskID string `json:"task_id"`
			Wait   bool   `json:"wait"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		if in.Wait {
			return service.Wait(ctx, in.TaskID)
		}
		task, ok := service.Get(in.TaskID)
		if !ok {
			return nil, ErrTaskNotFound
		}
		return task, nil
	}}
	list := &lifecycleTool{name: "task_list", description: "List child-agent tasks for the active session.", risk: tool.RiskLow, scope: scope, schema: object(map[string]any{}), execute: func(context.Context, json.RawMessage) (any, error) { return service.List(scope.session()), nil }}
	stop := &lifecycleTool{name: "task_stop", description: "Stop a queued or running child-agent task.", risk: tool.RiskMedium, scope: scope, schema: object(map[string]any{"task_id": map[string]any{"type": "string"}}, "task_id"), execute: func(ctx context.Context, raw json.RawMessage) (any, error) {
		var in struct {
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		return service.Stop(ctx, in.TaskID)
	}}
	resume := &lifecycleTool{name: "task_resume", description: "Resume a completed, failed, canceled, or interrupted child-agent task with the same stable ID and a new generation.", risk: tool.RiskMedium, scope: scope, schema: object(map[string]any{"task_id": map[string]any{"type": "string"}, "prompt": map[string]any{"type": "string"}, "idempotency_key": map[string]any{"type": "string"}}, "task_id", "prompt"), execute: func(ctx context.Context, raw json.RawMessage) (any, error) {
		var in struct {
			TaskID string `json:"task_id"`
			Prompt string `json:"prompt"`
			Key    string `json:"idempotency_key"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		if in.Key == "" {
			in.Key = newID("resume")
		}
		return service.Resume(ctx, in.TaskID, in.Key, in.Prompt)
	}}
	input := &lifecycleTool{name: "task_input", description: "Send additional input to a running task when its execution backend supports interactive input.", risk: tool.RiskMedium, scope: scope, schema: object(map[string]any{"task_id": map[string]any{"type": "string"}, "input": map[string]any{"type": "string"}}, "task_id", "input"), execute: func(ctx context.Context, raw json.RawMessage) (any, error) {
		var in struct {
			TaskID string `json:"task_id"`
			Input  string `json:"input"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		if err := service.SendInput(ctx, in.TaskID, in.Input); err != nil {
			return nil, err
		}
		task, _ := service.Get(in.TaskID)
		return task, nil
	}}
	tools := []tool.Tool{start, output, list, stop, resume, input}
	if controller == nil {
		return tools
	}
	backend := &lifecycleTool{name: "task_backend", description: "Inspect or control the observable terminal backend for an active task generation.", risk: tool.RiskMedium, scope: scope, schema: object(map[string]any{"action": map[string]any{"type": "string", "enum": []string{"status", "attach", "show", "hide"}}, "task_id": map[string]any{"type": "string"}, "generation": map[string]any{"type": "integer", "minimum": 1}}, "action", "task_id", "generation"), execute: func(ctx context.Context, raw json.RawMessage) (any, error) {
		var in struct {
			Action     string `json:"action"`
			TaskID     string `json:"task_id"`
			Generation uint64 `json:"generation"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.TaskID) == "" || in.Generation == 0 {
			return nil, fmt.Errorf("task_id and generation are required")
		}
		switch in.Action {
		case "status":
			return controller.BackendStatus(ctx, in.TaskID, in.Generation)
		case "attach":
			return BackendView{}, controller.BackendAttach(ctx, in.TaskID, in.Generation)
		case "show":
			return BackendView{}, controller.BackendShow(ctx, in.TaskID, in.Generation)
		case "hide":
			return BackendView{}, controller.BackendHide(ctx, in.TaskID, in.Generation)
		default:
			return nil, fmt.Errorf("unsupported backend action %q", in.Action)
		}
	}}
	return append(tools, backend)
}
