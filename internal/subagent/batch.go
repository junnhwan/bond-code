package subagent

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/junnhwan/bond-code/internal/observe"
)

func (m *SubagentManager) RunBatch(ctx context.Context, req BatchRequest) (*BatchResult, error) {
	mode := req.Mode
	if mode == "" {
		mode = TaskModeSingle
	}
	switch mode {
	case TaskModeSingle:
		return m.runSingle(ctx, req.Tasks)
	case TaskModeParallel:
		return m.runParallel(ctx, req.Tasks)
	case TaskModeChain:
		return m.runChain(ctx, req.Tasks)
	default:
		return nil, fmt.Errorf("unsupported task mode %q", mode)
	}
}

func (m *SubagentManager) runSingle(ctx context.Context, tasks []TaskRequest) (*BatchResult, error) {
	if len(tasks) != 1 {
		return nil, fmt.Errorf("single mode requires exactly one task")
	}
	result, err := m.RunTask(ctx, tasks[0])
	results := resultSlice(result, err, tasks[0])
	return &BatchResult{
		Mode:    TaskModeSingle,
		Results: results,
		Status:  aggregateStatus(results),
		Error:   aggregateError(results, err),
	}, nil
}

func (m *SubagentManager) runParallel(ctx context.Context, tasks []TaskRequest) (*BatchResult, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("parallel mode requires at least one task")
	}
	results := make([]SubagentResult, len(tasks))
	var wg sync.WaitGroup
	for idx, task := range tasks {
		idx := idx
		task := task
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					observe.LogPanic("subagent-parallel", r, debug.Stack())
					results[idx] = resultValue(nil, fmt.Errorf("subagent panic: %v", r), task)
				}
			}()
			result, err := m.RunTask(ctx, task)
			results[idx] = resultValue(result, err, task)
		}()
	}
	wg.Wait()
	return &BatchResult{
		Mode:    TaskModeParallel,
		Results: results,
		Status:  aggregateStatus(results),
		Error:   aggregateResultErrors(results),
	}, nil
}

func (m *SubagentManager) runChain(ctx context.Context, tasks []TaskRequest) (*BatchResult, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("chain mode requires at least one task")
	}
	results := make([]SubagentResult, 0, len(tasks))
	var previous *SubagentResult
	for _, original := range tasks {
		task := original
		if previous != nil {
			task.Prompt = appendPreviousResult(task.Prompt, *previous, m.resultCap())
		}
		result, err := m.RunTask(ctx, task)
		value := resultValue(result, err, task)
		results = append(results, value)
		previous = &results[len(results)-1]
		if value.Status != "completed" {
			break
		}
	}
	return &BatchResult{
		Mode:    TaskModeChain,
		Results: results,
		Status:  aggregateStatus(results),
		Error:   aggregateResultErrors(results),
	}, nil
}

func resultSlice(result *SubagentResult, err error, task TaskRequest) []SubagentResult {
	return []SubagentResult{resultValue(result, err, task)}
}

func resultValue(result *SubagentResult, err error, task TaskRequest) SubagentResult {
	if result != nil {
		return *result
	}
	value := SubagentResult{
		TaskID:      task.TaskID,
		Description: task.Description,
		Prompt:      task.Prompt,
		AgentType:   task.SubagentType,
		Status:      "failed",
	}
	if err != nil {
		value.Error = err.Error()
	}
	return value
}

func appendPreviousResult(prompt string, previous SubagentResult, maxChars int) string {
	body := strings.TrimSpace(previous.FinalAnswer)
	if body == "" {
		body = strings.TrimSpace(previous.Error)
	}
	if body == "" {
		body = previous.Status
	}
	body = capString(body, maxChars, "[subagent result truncated]")
	return strings.TrimSpace(prompt) + "\n\nPrevious subagent result:\n" + body
}

func aggregateStatus(results []SubagentResult) string {
	if len(results) == 0 {
		return "failed"
	}
	allCompleted := true
	anyCancelled := false
	for _, result := range results {
		switch result.Status {
		case "completed":
		case "cancelled":
			allCompleted = false
			anyCancelled = true
		default:
			allCompleted = false
		}
	}
	if allCompleted {
		return "completed"
	}
	if anyCancelled {
		return "cancelled"
	}
	return "failed"
}

func aggregateError(results []SubagentResult, err error) string {
	if err != nil {
		return err.Error()
	}
	return aggregateResultErrors(results)
}

func aggregateResultErrors(results []SubagentResult) string {
	errors := []string{}
	for _, result := range results {
		if strings.TrimSpace(result.Error) != "" {
			errors = append(errors, result.Error)
		}
	}
	return strings.Join(errors, "\n")
}
