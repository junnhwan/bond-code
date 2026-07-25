package subagent

import (
	"context"
	"errors"
	"testing"

	"github.com/junnhwan/bond-code/internal/agenttask"
)

type fakeAgentTaskManager struct {
	request  TaskRequest
	result   *SubagentResult
	err      error
	canceled string
}

func (f *fakeAgentTaskManager) RunTask(_ context.Context, req TaskRequest) (*SubagentResult, error) {
	f.request = req
	return f.result, f.err
}
func (f *fakeAgentTaskManager) CancelTask(id string) bool { f.canceled = id; return true }
func TestAgentTaskRunnerUsesCanonicalIDAndResumeHistory(t *testing.T) {
	manager := &fakeAgentTaskManager{result: &SubagentResult{TaskID: "canonical", Status: "completed", FinalAnswer: "done"}}
	runner := &AgentTaskRunner{manager: manager}
	result := runner.Run(context.Background(), agenttask.RunRequest{TaskID: "canonical", Generation: 2, SessionID: "s", Prompt: "continue", Profile: "coder"})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if manager.request.TaskID != "canonical" || manager.request.ResumeTaskID != "canonical" || manager.request.SubagentType != AgentTypeCoder {
		t.Fatalf("request=%#v", manager.request)
	}
}
func TestAgentTaskRunnerRejectsUnusableCompletedAnswer(t *testing.T) {
	tests := []string{
		"",
		"<tool_call>search_text<arg_key>path</arg_key>",
		"Step budget reached after tool use; returning the latest available tool result instead of continuing.\n\npackage rawsource",
	}
	for _, answer := range tests {
		manager := &fakeAgentTaskManager{result: &SubagentResult{TaskID: "x", Status: "completed", FinalAnswer: answer}}
		result := (&AgentTaskRunner{manager: manager}).Run(context.Background(), agenttask.RunRequest{TaskID: "x", Generation: 1})
		if result.Err == nil {
			t.Fatalf("unusable completed answer must be mapped to failure: %q", answer)
		}
	}
}

func TestAgentTaskRunnerAcceptsSummaryThatMentionsToolProtocol(t *testing.T) {
	answer := "The review found that literal <tool_call> artifacts must be rejected."
	manager := &fakeAgentTaskManager{result: &SubagentResult{TaskID: "x", Status: "completed", FinalAnswer: answer}}
	result := (&AgentTaskRunner{manager: manager}).Run(context.Background(), agenttask.RunRequest{TaskID: "x", Generation: 1})
	if result.Err != nil {
		t.Fatalf("prose summary should remain usable: %v", result.Err)
	}
}

func TestAgentTaskRunnerMapsFailure(t *testing.T) {
	manager := &fakeAgentTaskManager{result: &SubagentResult{TaskID: "x", Status: "failed", Error: "boom"}}
	result := (&AgentTaskRunner{manager: manager}).Run(context.Background(), agenttask.RunRequest{TaskID: "x", Generation: 1})
	if result.Err == nil || result.ErrorText != "boom" {
		t.Fatalf("result=%#v", result)
	}
}
func TestAgentTaskRunnerStopAndUnsupportedInput(t *testing.T) {
	manager := &fakeAgentTaskManager{}
	runner := &AgentTaskRunner{manager: manager}
	if err := runner.Stop(context.Background(), "x", 1); err != nil {
		t.Fatal(err)
	}
	if manager.canceled != "x" {
		t.Fatalf("canceled=%q", manager.canceled)
	}
	if err := runner.SendInput(context.Background(), "x", 1, "hello"); !errors.Is(err, ErrTaskInputUnsupported) {
		t.Fatalf("error=%v", err)
	}
}

func TestSendTaskInputQueuesForRunningTask(t *testing.T) {
	manager := NewSubagentManagerWithOptions(nil, nil, ManagerOptions{})
	input := make(chan string, 1)
	manager.taskInputs.Store("task-1", input)
	defer manager.taskInputs.Delete("task-1")
	if err := manager.SendTaskInput(context.Background(), "task-1", "focus on tests"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-input:
		if got != "focus on tests" {
			t.Fatalf("input = %q", got)
		}
	default:
		t.Fatal("input was not queued")
	}
	if err := manager.SendTaskInput(context.Background(), "missing", "x"); err != ErrTaskNotRunning {
		t.Fatalf("missing error = %v", err)
	}
}
