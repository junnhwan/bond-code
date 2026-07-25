package agenttask

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/junnhwan/bond-code/internal/tool"
)

func findLifecycleTool(t *testing.T, tools []interface{}, name string) { t.Helper() }
func TestLifecycleToolsStartOutputAndSessionBinding(t *testing.T) {
	runner := newControlledRunner()
	service := openService(t, runner)
	tools := LifecycleTools(service, "session-a")
	byName := map[string]interface{}{}
	for _, candidate := range tools {
		byName[candidate.Name()] = candidate
	}
	start := byName["agent_task"].(*lifecycleTool)
	raw := json.RawMessage(`{"prompt":"inspect","mode":"background","idempotency_key":"tool-start"}`)
	result, err := start.Execute(context.Background(), raw)
	if err != nil || !result.OK {
		t.Fatalf("start=%#v %v", result, err)
	}
	req := <-runner.started
	if req.SessionID != "session-a" {
		t.Fatalf("session=%q", req.SessionID)
	}
	start.BindSession("session-b")
	raw = json.RawMessage(`{"prompt":"inspect again","mode":"background","idempotency_key":"tool-start-2"}`)
	if _, err = start.Execute(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	req = <-runner.started
	if req.SessionID != "session-b" {
		t.Fatalf("rebound session=%q", req.SessionID)
	}
}
func TestLifecycleToolsExposeStableSurface(t *testing.T) {
	tools := LifecycleTools(openService(t, newControlledRunner()), "s")
	want := []string{"agent_task", "task_output", "task_list", "task_stop", "task_resume", "task_input"}
	if len(tools) != len(want) {
		t.Fatalf("tools=%d", len(tools))
	}
	for i, name := range want {
		if tools[i].Name() != name {
			t.Fatalf("tool[%d]=%q", i, tools[i].Name())
		}
	}
}

func TestLifecycleToolsSchemasDoNotEncodeNullRequired(t *testing.T) {
	tools := LifecycleTools(openService(t, newControlledRunner()), "s")
	for _, candidate := range tools {
		body, err := json.Marshal(candidate.Schema())
		if err != nil {
			t.Fatalf("marshal %s schema: %v", candidate.Name(), err)
		}
		if string(body) == "" || json.Valid(body) == false {
			t.Fatalf("invalid %s schema: %s", candidate.Name(), body)
		}
		if containsJSONNullRequired(body) {
			t.Fatalf("%s schema contains required:null: %s", candidate.Name(), body)
		}
	}
}

func containsJSONNullRequired(body []byte) bool {
	var schema map[string]any
	if err := json.Unmarshal(body, &schema); err != nil {
		return false
	}
	required, exists := schema["required"]
	return exists && required == nil
}

type fakeBackendController struct {
	action     string
	taskID     string
	generation uint64
}

func (c *fakeBackendController) BackendStatus(context.Context, string, uint64) (BackendView, error) {
	c.action = "status"
	return BackendView{Backend: "tmux", State: "running", Healthy: true, Attach: true}, nil
}
func (c *fakeBackendController) BackendAttach(_ context.Context, taskID string, generation uint64) error {
	c.action, c.taskID, c.generation = "attach", taskID, generation
	return nil
}
func (c *fakeBackendController) BackendShow(context.Context, string, uint64) error { return nil }
func (c *fakeBackendController) BackendHide(context.Context, string, uint64) error { return nil }

func TestLifecycleToolsWithBackendControlsActiveGeneration(t *testing.T) {
	controller := &fakeBackendController{}
	tools := LifecycleToolsWithBackend(openService(t, newControlledRunner()), "s", controller)
	var backendTool tool.Tool
	for _, candidate := range tools {
		if candidate.Name() == "task_backend" {
			backendTool = candidate
			break
		}
	}
	if backendTool == nil {
		t.Fatal("missing task_backend")
	}
	result, err := backendTool.Execute(context.Background(), json.RawMessage(`{"action":"status","task_id":"task-1","generation":2}`))
	if err != nil || !result.OK || controller.action != "status" {
		t.Fatalf("status result=%#v err=%v action=%q", result, err, controller.action)
	}
	result, err = backendTool.Execute(context.Background(), json.RawMessage(`{"action":"attach","task_id":"task-1","generation":2}`))
	if err != nil || !result.OK {
		t.Fatalf("attach result=%#v err=%v", result, err)
	}
	if controller.action != "attach" || controller.taskID != "task-1" || controller.generation != 2 {
		t.Fatalf("forwarded action=%q task=%q generation=%d", controller.action, controller.taskID, controller.generation)
	}
}
