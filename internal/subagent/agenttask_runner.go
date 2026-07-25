package subagent

import (
	"context"
	"errors"
	"fmt"

	"github.com/junnhwan/bond-code/internal/agenttask"
)

var ErrTaskInputUnsupported = errors.New("running subagent does not support interactive input")

type agentTaskManager interface {
	RunTask(context.Context, TaskRequest) (*SubagentResult, error)
	CancelTask(string) bool
}
type taskInputSender interface {
	SendTaskInput(context.Context, string, string) error
}
type AgentTaskRunner struct{ manager agentTaskManager }

func NewAgentTaskRunner(manager *SubagentManager) *AgentTaskRunner {
	return &AgentTaskRunner{manager: manager}
}
func (r *AgentTaskRunner) Run(ctx context.Context, req agenttask.RunRequest) agenttask.RunResult {
	if r == nil || r.manager == nil {
		return agenttask.RunResult{Err: errors.New("subagent manager is required")}
	}
	agentType := AgentType(req.Profile)
	if agentType == "" {
		agentType = AgentTypeResearch
	}
	taskReq := TaskRequest{SessionID: req.SessionID, Description: req.Description, Prompt: req.Prompt, SubagentType: agentType, TaskID: req.TaskID, Generation: req.Generation}
	if req.Generation > 1 {
		taskReq.ResumeTaskID = req.TaskID
	}
	result, err := r.manager.RunTask(ctx, taskReq)
	if err != nil {
		return agenttask.RunResult{Err: err}
	}
	if result == nil {
		return agenttask.RunResult{Err: errors.New("subagent returned no result")}
	}
	run := agenttask.RunResult{Summary: result.FinalAnswer, ErrorText: result.Error, LegacyAlias: result.TaskID}
	if result.Status != "completed" {
		if result.Error != "" {
			run.Err = errors.New(result.Error)
		} else {
			run.Err = fmt.Errorf("subagent ended with status %s", result.Status)
		}
		return run
	}
	if validationErr := validateUsableFinalAnswer(result.FinalAnswer); validationErr != nil {
		run.Err = validationErr
		if run.ErrorText == "" {
			run.ErrorText = validationErr.Error()
		}
	}
	return run
}
func (r *AgentTaskRunner) Stop(_ context.Context, taskID string, _ uint64) error {
	if r == nil || r.manager == nil {
		return errors.New("subagent manager is required")
	}
	if !r.manager.CancelTask(taskID) {
		return nil
	}
	return nil
}
func (r *AgentTaskRunner) SendInput(ctx context.Context, taskID string, _ uint64, input string) error {
	if sender, ok := r.manager.(taskInputSender); ok {
		return sender.SendTaskInput(ctx, taskID, input)
	}
	return ErrTaskInputUnsupported
}
