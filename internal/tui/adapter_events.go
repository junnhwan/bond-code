package tui

import (
	"strings"

	"github.com/junnhwan/bond-code/internal/agent"
)

type agentEventFamily uint8

const (
	agentEventFamilyOther agentEventFamily = iota
	agentEventFamilyStream
	agentEventFamilyContext
	agentEventFamilyTool
	agentEventFamilySubagent
	agentEventFamilyTerminal
)

func classifyAgentEvent(eventType agent.EventType) agentEventFamily {
	switch eventType {
	case agent.EventAgentStarted, agent.EventModelChunk, agent.EventReasoningChunk:
		return agentEventFamilyStream
	case agent.EventContextUpdated, agent.EventContextMeasured, agent.EventCompactionStarted, agent.EventCompactionFinished:
		return agentEventFamilyContext
	case agent.EventToolRequested, agent.EventToolConfirmationRequested, agent.EventToolApproved,
		agent.EventToolRejected, agent.EventToolResult:
		return agentEventFamilyTool
	case agent.EventSubagentStarted, agent.EventSubagentProgress, agent.EventSubagentFinished,
		agent.EventSubagentFailed, agent.EventSubagentCancelled, agent.EventSubagentToolCall,
		agent.EventSubagentModelChunk, agent.EventSubagentReasoningChunk:
		return agentEventFamilySubagent
	case agent.EventAgentFinished, agent.EventAgentError, agent.EventTextDegeneration:
		return agentEventFamilyTerminal
	default:
		return agentEventFamilyOther
	}
}

func (m Model) applyStreamEvent(event agent.Event) Model {
	switch event.Type {
	case agent.EventAgentStarted:
		m.agent.LiveStream = nil
		m.agent.LiveDetail = ""
		m.agent.TerminalHandled = false
		m.timeline = m.timeline.MarkAgentStarted(eventTime(event))
		return m
	case agent.EventModelChunk:
		return m.applyAssistantChunk(event.Message, eventTime(event))
	case agent.EventReasoningChunk:
		return m.applyReasoningChunk(event.Message, eventTime(event))
	default:
		return m
	}
}

func (m Model) applyContextEvent(event agent.Event) Model {
	switch event.Type {
	case agent.EventContextUpdated:
		// The loop reports the estimated live context-window usage on each call.
		return m.cacheContextTokens(event)
	case agent.EventContextMeasured:
		// Prefer real API counts so the header matches /status.
		return m.cacheMeasuredTokens(event)
	case agent.EventCompactionStarted:
		m.timeline = m.timeline.UpdateAgentStatus("working", "compacting context", eventTime(event))
		return m
	case agent.EventCompactionFinished:
		before := m.agent.ContextTokens
		m = m.cacheContextTokens(event)
		m.timeline = m.timeline.UpdateAgentStatus("working", "thinking", eventTime(event))
		if div := compactionDividerBody(before, event.ContextTokens); div != "" {
			m.timeline = m.timeline.AppendBlock(BlockCompaction, "compact", div)
		}
		m = m.refreshStatus()
		return m.markNewOutputBelow()
	default:
		return m
	}
}

func (m Model) applyToolEvent(event agent.Event) Model {
	switch event.Type {
	case agent.EventToolRequested:
		m.timeline = m.timeline.UpdateAgentStatus("working", "tool: "+event.ToolName, eventTime(event))
		tool := toolBlockFromEvent(event, ToolRunning)
		m.timeline = m.timeline.UpsertToolBlock(tool)
		return m.markNewOutputBelow()
	case agent.EventToolConfirmationRequested:
		m.timeline = m.timeline.UpdateAgentStatus("waiting", "confirm "+event.ToolName, eventTime(event))
		m = m.deferQuestionDock()
		m = m.closeHistoryOverlay()
		if m.composer.Suggestions != nil {
			m.composer.Suggestions.Hide()
		}
		m.agent.Pending = &event
		// High risk defaults to reject; other risks default to allow once.
		if event.Risk == "high" {
			m.agent.ConfirmChoice = choiceReject
		} else {
			m.agent.ConfirmChoice = choiceOnce
		}
		m.agent.ConfirmEnteringReject = false
		m.agent.ConfirmRejectReason = ""
		tool := toolBlockFromEvent(event, ToolPending)
		tool.Summary = firstNonEmpty(event.Message, summarizeToolInput(event.ToolName, event.Input), confirmationPrompt(event))
		m.timeline = m.timeline.UpsertToolBlock(tool)
		return m.markNewOutputBelow()
	case agent.EventToolApproved:
		m.timeline = m.timeline.UpdateAgentStatus("working", "tool: "+event.ToolName, eventTime(event))
		m.agent.Pending = nil
		tool := toolBlockFromEvent(event, ToolRunning)
		tool.Summary = firstNonEmpty(event.Message, "approved")
		m.timeline = m.timeline.UpsertToolBlock(tool)
		return m.markNewOutputBelow()
	case agent.EventToolRejected:
		m.timeline = m.timeline.UpdateAgentStatus("working", "rejected "+event.ToolName, eventTime(event))
		m.agent.Pending = nil
		tool := toolBlockFromEvent(event, ToolRejected)
		tool.Summary = firstNonEmpty(event.Message, "rejected")
		m.timeline = m.timeline.UpsertToolBlock(tool)
		return m.markNewOutputBelow()
	case agent.EventToolResult:
		m.timeline = m.timeline.UpdateAgentStatus("working", "thinking", eventTime(event))
		status := ToolDone
		if strings.TrimSpace(event.Error) != "" {
			status = ToolFailed
		}
		tool := toolBlockFromEvent(event, status)
		m.timeline = m.timeline.UpsertToolBlock(tool)
		m = m.markNewOutputBelow()
		if status == ToolDone && isTodoMutation(event.ToolName) {
			m = m.refreshStatus()
		}
		return m
	default:
		return m
	}
}

func (m Model) applySubagentEvent(event agent.Event) Model {
	switch event.Type {
	case agent.EventSubagentStarted, agent.EventSubagentProgress, agent.EventSubagentFinished,
		agent.EventSubagentFailed, agent.EventSubagentCancelled:
		taskID, title, status, body := subagentBlockFromEvent(event)
		m.timeline = m.timeline.UpdateAgentStatus("working", firstNonEmpty("subagent: "+status, "subagent"), eventTime(event))
		m.timeline = m.timeline.UpsertSubagentBlock(taskID, title, status, body)
		m.live = LiveStatusWithSubagentTimeline(m.live, m.timeline)
		m = m.updateSubagentTrace(event)
		return m.markNewOutputBelow()
	case agent.EventSubagentToolCall, agent.EventSubagentModelChunk, agent.EventSubagentReasoningChunk:
		// Child transcript/tool streams live only in the agent window, not the main timeline.
		return m.updateSubagentTrace(event)
	default:
		return m
	}
}

func (m Model) applyTerminalEvent(event agent.Event, hadAssistantLive bool) Model {
	switch event.Type {
	case agent.EventAgentFinished:
		// The finish event repeats streamed text; use it only as a non-streamed fallback.
		if !hadAssistantLive {
			if body := strings.TrimSpace(event.Message); body != "" {
				m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", event.Message)
			}
		}
		m.agent.Err = nil
		m.agent.TerminalHandled = true
		m.timeline = m.timeline.MarkAgentEnded("done", "", eventTime(event))
		return m.markNewOutputNotice("agent done")
	case agent.EventAgentError:
		body := humanizeAgentError(firstNonEmpty(event.Error, event.Message))
		if event.ToolCallID != "" {
			// Parallel tool failures fail only the matching tool; the run continues.
			tool := toolBlockFromEvent(event, ToolFailed)
			tool.Summary = firstNonEmpty(body, event.Message, "tool failed")
			m.timeline = m.timeline.UpsertToolBlock(tool)
			return m.markNewOutputBelow()
		}
		m.agent.TerminalHandled = true
		m.timeline = m.timeline.MarkAgentEnded("failed", body, eventTime(event))
		m.timeline = m.timeline.AppendBlock(BlockError, "agent error", body)
		return m.markNewOutputBelow()
	case agent.EventTextDegeneration:
		// Soft recovery cue: status + toast. Do not dump a long "text guard"
		// command block into the transcript — it looks like a hard error and
		// crowds the tool/answer audit trail (especially on reasoning false trips).
		m.timeline = m.timeline.UpdateAgentStatus("working", "recovering from repeated output", eventTime(event))
		body := firstNonEmpty(event.Message, "repeated output detected; recovering")
		// Keep a structural boundary so live assistant/reasoning commits cleanly
		// before recovery continues, but use a short dim notice.
		notice := body
		if strings.Contains(strings.ToLower(body), "reasoning") {
			notice = "thinking loop detected; recovering"
		} else if strings.Contains(strings.ToLower(body), "degeneration") {
			notice = "repeated output; recovering"
		}
		m.timeline = m.timeline.AppendBlock(BlockCommand, "recovering", notice)
		m = m.pushToast(notice, toastInfo)
		return m.markNewOutputBelow()
	default:
		return m
	}
}
