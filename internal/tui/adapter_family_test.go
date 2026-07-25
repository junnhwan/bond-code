package tui

import (
	"testing"

	"github.com/junnhwan/bond-code/internal/agent"
)

func TestAgentEventFamilyClassification(t *testing.T) {
	tests := []struct {
		name  string
		type_ agent.EventType
		want  agentEventFamily
	}{
		{name: "assistant stream", type_: agent.EventModelChunk, want: agentEventFamilyStream},
		{name: "context", type_: agent.EventContextUpdated, want: agentEventFamilyContext},
		{name: "tool", type_: agent.EventToolRequested, want: agentEventFamilyTool},
		{name: "subagent", type_: agent.EventSubagentProgress, want: agentEventFamilySubagent},
		{name: "terminal", type_: agent.EventAgentFinished, want: agentEventFamilyTerminal},
		{name: "other", type_: agent.EventLoopGuard, want: agentEventFamilyOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyAgentEvent(tt.type_); got != tt.want {
				t.Fatalf("event family = %v, want %v", got, tt.want)
			}
		})
	}
}
