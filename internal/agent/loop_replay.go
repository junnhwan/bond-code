package agent

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/tool"
)

type toolReplayOutcome struct {
	messages           []llm.Message
	stepBudgetFallback string
	stepUsedTodo       bool
	noProgressGuard    string
}

// executeAndReplayTools owns one model response's tool phase: serial guard
// checks, safe parallel execution when the entire batch qualifies, and ordered
// tool-result replay into model context.
func (l *Loop) executeAndReplayTools(
	ctx context.Context,
	messages []llm.Message,
	requested []llm.ToolCall,
	guard *loopGuard,
	emit EventSink,
	step int,
	stepBudgetFallback string,
) (toolReplayOutcome, error) {
	outcome := toolReplayOutcome{
		messages:           messages,
		stepBudgetFallback: stepBudgetFallback,
	}

	// loopGuard is stateful and not concurrency-safe, so every decision is made
	// serially before any eligible tool execution starts.
	guardedDecisions := make([]loopGuardDecision, len(requested))
	anyGuarded := false
	for i, call := range requested {
		if strings.HasPrefix(call.Name, "todo_") {
			outcome.stepUsedTodo = true
		}
		guardedDecisions[i] = guard.Check(call, i)
		if guardedDecisions[i].Guarded {
			anyGuarded = true
		}
	}

	allSafe := !anyGuarded && len(requested) > 1
	for _, call := range requested {
		if !isParallelSafeTool(call.Name) {
			allSafe = false
			break
		}
	}
	if allSafe {
		results := make([]*tool.Result, len(requested))
		var wg sync.WaitGroup
		for i, call := range requested {
			call := call
			wg.Add(1)
			go func(idx int, c llm.ToolCall) {
				defer wg.Done()
				// Preserve tool-use/result pairing even if a tool implementation
				// panics in its worker goroutine.
				defer func() {
					if r := recover(); r != nil {
						observe.LogPanic("tool-parallel", r, debug.Stack())
						msg := fmt.Sprintf("tool panic: %v", r)
						emit(Event{Type: EventAgentError, Message: msg, ToolName: c.Name, ToolCallID: c.ID, Error: msg})
						results[idx] = tool.ErrorResult(c.Name, "tool panic", msg)
					}
				}()
				r, execErr := l.executeTool(ctx, &c, emit, step)
				if execErr != nil || r == nil {
					msg := ""
					if execErr != nil {
						msg = execErr.Error()
					}
					emit(Event{Type: EventAgentError, Message: msg, ToolName: c.Name, ToolCallID: c.ID, Error: msg})
					r = tool.ErrorResult(c.Name, "tool error", msg)
				}
				results[idx] = r
			}(i, call)
		}
		wg.Wait()
		for i, result := range results {
			outcome.stepBudgetFallback = selectStepBudgetFallback(outcome.stepBudgetFallback, result)
			content := modelToolResultContent(result)
			guard.RecordResult(requested[i], content)
			outcome.messages = append(outcome.messages, llm.Message{
				Role:       llm.RoleTool,
				Content:    content,
				ToolCallID: requested[i].ID,
				ToolName:   requested[i].Name,
			})
		}
		return outcome, nil
	}

	for i, call := range requested {
		call := call
		if outcome.noProgressGuard != "" {
			result := emitGuardedToolResult(emit, &call, skippedAfterNoProgressGuardMessage)
			outcome.messages = append(outcome.messages, llm.Message{Role: llm.RoleTool, Content: modelToolResultContent(result), ToolCallID: call.ID, ToolName: call.Name})
			continue
		}
		decision := guardedDecisions[i]
		if decision.Guarded {
			result := emitGuardedToolResult(emit, &call, decision.Output)
			outcome.messages = append(outcome.messages, llm.Message{Role: llm.RoleTool, Content: modelToolResultContent(result), ToolCallID: call.ID, ToolName: call.Name})
			if decision.Stop && l.cfg.StopOnNoProgress {
				outcome.noProgressGuard = decision.Output
			}
			continue
		}
		result, err := l.executeTool(ctx, &call, emit, step)
		if err != nil {
			emit(Event{Type: EventAgentError, Message: err.Error(), ToolName: call.Name, ToolCallID: call.ID, Error: err.Error()})
			return outcome, err
		}
		outcome.stepBudgetFallback = selectStepBudgetFallback(outcome.stepBudgetFallback, result)
		content := modelToolResultContent(result)
		guard.RecordResult(call, content)
		outcome.messages = append(outcome.messages, llm.Message{Role: llm.RoleTool, Content: content, ToolCallID: call.ID, ToolName: call.Name})
	}
	return outcome, nil
}

func selectStepBudgetFallback(current string, result *tool.Result) string {
	result = tool.NormalizeResult(result, "")
	if !result.OK || strings.TrimSpace(result.Output) == "" {
		return current
	}
	if strings.Contains(result.Output, "<task_result") {
		return result.Output
	}
	return result.Output
}
