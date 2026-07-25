package agent

import (
	"context"
	"reflect"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
)

func TestCollectModelResponseClassifiesStream(t *testing.T) {
	tests := []struct {
		name             string
		chunks           []llm.Chunk
		wantText         string
		wantCalls        []llm.ToolCall
		wantStopReason   string
		wantUsage        *llm.Usage
		wantSawReasoning bool
		wantEvents       []EventType
	}{
		{
			name: "text and usage",
			chunks: []llm.Chunk{
				{Content: "hel"},
				{Content: "lo", Usage: &llm.Usage{InputTokens: 11, OutputTokens: 2}},
				{Done: true, StopReason: "end_turn"},
			},
			wantText:       "hello",
			wantStopReason: "end_turn",
			wantUsage:      &llm.Usage{InputTokens: 11, OutputTokens: 2},
			wantEvents: []EventType{
				EventModelChunk,
				EventContextMeasured,
				EventModelChunk,
				EventContextMeasured,
			},
		},
		{
			name: "reasoning and tool call",
			chunks: []llm.Chunk{
				{Reasoning: "checking"},
				{ToolCall: &llm.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"path":"a.go"}`}},
				{Done: true, StopReason: "tool_use"},
			},
			wantCalls:        []llm.ToolCall{{ID: "call-1", Name: "read_file", Arguments: `{"path":"a.go"}`}},
			wantStopReason:   "tool_use",
			wantSawReasoning: true,
			wantEvents:       []EventType{EventReasoningChunk, EventToolRequested},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loop := NewLoop(LoopConfig{}, llm.NewFakeClient(tt.chunks), tool.NewRegistry(), safety.Policy{}, safety.StaticConfirmer(true))
			var eventTypes []EventType

			response, err := loop.collectModelResponse(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "test"}}, nil, func(event Event) {
				eventTypes = append(eventTypes, event.Type)
			})
			if err != nil {
				t.Fatal(err)
			}
			if response.text != tt.wantText {
				t.Fatalf("text = %q, want %q", response.text, tt.wantText)
			}
			if !reflect.DeepEqual(response.toolCalls, tt.wantCalls) {
				t.Fatalf("tool calls = %#v, want %#v", response.toolCalls, tt.wantCalls)
			}
			if response.stopReason != tt.wantStopReason {
				t.Fatalf("stop reason = %q, want %q", response.stopReason, tt.wantStopReason)
			}
			if !reflect.DeepEqual(response.usage, tt.wantUsage) {
				t.Fatalf("usage = %#v, want %#v", response.usage, tt.wantUsage)
			}
			if response.sawReasoning != tt.wantSawReasoning {
				t.Fatalf("saw reasoning = %v, want %v", response.sawReasoning, tt.wantSawReasoning)
			}
			if !reflect.DeepEqual(eventTypes, tt.wantEvents) {
				t.Fatalf("event types = %v, want %v", eventTypes, tt.wantEvents)
			}
		})
	}
}
