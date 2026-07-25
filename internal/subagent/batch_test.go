package subagent

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
)

func TestBatchRunsSingleTask(t *testing.T) {
	manager := newTestManager(
		llmfake.New([][]llm.Chunk{
			{{Content: "single answer", Done: true}},
		}),
		tool.NewRegistry(),
	)

	result, err := manager.RunBatch(context.Background(), BatchRequest{
		Mode: TaskModeSingle,
		Tasks: []TaskRequest{{
			Description:  "single",
			Prompt:       "inspect one thing",
			SubagentType: AgentTypeResearch,
			TaskID:       "single-1",
		}},
	})
	if err != nil {
		t.Fatalf("batch single: %v", err)
	}
	if result.Status != "completed" || len(result.Results) != 1 {
		t.Fatalf("expected one completed result, got %#v", result)
	}
	if result.Results[0].TaskID != "single-1" || result.Results[0].FinalAnswer != "single answer" {
		t.Fatalf("unexpected single result %#v", result.Results[0])
	}
}

func TestBatchRunsParallelTasksWithinLimit(t *testing.T) {
	manager := newTestManagerWithOptions(
		llmfake.New([][]llm.Chunk{
			{{Content: "alpha answer", Done: true}},
			{{Content: "beta answer", Done: true}},
		}),
		tool.NewRegistry(),
		ManagerOptions{MaxChildrenPerTurn: 2, DefaultTimeoutSeconds: 5},
	)

	result, err := manager.RunBatch(context.Background(), BatchRequest{
		Mode: TaskModeParallel,
		Tasks: []TaskRequest{
			{Description: "alpha", Prompt: "inspect alpha", SubagentType: AgentTypeResearch, TaskID: "alpha"},
			{Description: "beta", Prompt: "inspect beta", SubagentType: AgentTypeResearch, TaskID: "beta"},
		},
	})
	if err != nil {
		t.Fatalf("batch parallel: %v", err)
	}
	if result.Status != "completed" || len(result.Results) != 2 {
		t.Fatalf("expected two completed results, got %#v", result)
	}
	if result.Results[0].TaskID != "alpha" || result.Results[1].TaskID != "beta" {
		t.Fatalf("expected stable input order, got %#v", result.Results)
	}
}

func TestBatchRunsChainWithPreviousResult(t *testing.T) {
	client := llmfake.New([][]llm.Chunk{
		{{Content: "first answer", Done: true}},
		{{Content: "second answer", Done: true}},
	})
	manager := newTestManager(client, tool.NewRegistry())

	result, err := manager.RunBatch(context.Background(), BatchRequest{
		Mode: TaskModeChain,
		Tasks: []TaskRequest{
			{Description: "first", Prompt: "inspect first", SubagentType: AgentTypeResearch, TaskID: "first"},
			{Description: "second", Prompt: "inspect second", SubagentType: AgentTypeResearch, TaskID: "second"},
		},
	})
	if err != nil {
		t.Fatalf("batch chain: %v", err)
	}
	if result.Status != "completed" || len(result.Results) != 2 {
		t.Fatalf("expected completed chain, got %#v", result)
	}
	lastMessages := client.LastMessages()
	if len(lastMessages) == 0 {
		t.Fatal("expected fake client to record last child prompt")
	}
	lastPrompt := lastMessages[len(lastMessages)-1].Content
	if !strings.Contains(lastPrompt, "Previous subagent result") || !strings.Contains(lastPrompt, "first answer") {
		t.Fatalf("expected previous result in chained prompt, got %#v", lastMessages)
	}
}

func TestBatchCancelsParallelTasks(t *testing.T) {
	manager := newTestManagerWithOptions(
		llmfake.New([][]llm.Chunk{
			{{Content: "should not matter", Done: true}},
			{{Content: "should not matter", Done: true}},
		}),
		tool.NewRegistry(),
		ManagerOptions{MaxChildrenPerTurn: 2, DefaultTimeoutSeconds: 5},
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := manager.RunBatch(ctx, BatchRequest{
		Mode: TaskModeParallel,
		Tasks: []TaskRequest{
			{Description: "alpha", Prompt: "inspect alpha", SubagentType: AgentTypeResearch, TaskID: "alpha"},
			{Description: "beta", Prompt: "inspect beta", SubagentType: AgentTypeResearch, TaskID: "beta"},
		},
	})
	if err != nil {
		t.Fatalf("batch cancelled parallel: %v", err)
	}
	if result.Status != "cancelled" {
		t.Fatalf("expected aggregate cancelled status, got %#v", result)
	}
	for _, child := range result.Results {
		if child.Status != "cancelled" {
			t.Fatalf("expected child cancellation, got %#v", result.Results)
		}
	}
}

func TestBatchCapsOutput(t *testing.T) {
	manager := newTestManagerWithOptions(
		llmfake.New([][]llm.Chunk{
			{{Content: strings.Repeat("x", 100), Done: true}},
		}),
		tool.NewRegistry(),
		ManagerOptions{MaxResultChars: 24, DefaultTimeoutSeconds: 5},
	)

	result, err := manager.RunBatch(context.Background(), BatchRequest{
		Mode: TaskModeSingle,
		Tasks: []TaskRequest{{
			Description:  "long",
			Prompt:       "return long output",
			SubagentType: AgentTypeResearch,
		}},
	})
	if err != nil {
		t.Fatalf("batch capped: %v", err)
	}
	if !strings.Contains(result.Results[0].FinalAnswer, "[subagent result truncated]") {
		t.Fatalf("expected truncated marker, got %#v", result.Results[0])
	}
}
