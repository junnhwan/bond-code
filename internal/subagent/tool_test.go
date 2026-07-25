package subagent

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskToolRunsSubagentSynchronouslyAndReturnsResult(t *testing.T) {
	manager := newTestManager(&MockLLMClient{
		Chunks: []llm.Chunk{
			{Content: "Summary:\nREADME describes a local coding agent runtime.\nEvidence:\n- README.md:1", Done: true},
		},
	}, tool.NewRegistry())
	taskTool := NewTaskTool(manager)

	result, err := taskTool.Execute(context.Background(), []byte(`{
		"description":"summarize README",
		"prompt":"Read README and summarize the runtime.",
		"subagent_type":"research"
	}`))

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.OK)
	assert.Equal(t, "task", result.ToolName)
	assert.Equal(t, tool.StatusSuccess, result.Status)
	assert.Contains(t, result.Output, `<task_result id="sub-`)
	assert.Contains(t, result.Output, `type="research"`)
	assert.Contains(t, result.Output, `status="completed"`)
	assert.Contains(t, result.Output, "Summary:")
	assert.Contains(t, result.Output, "README describes a local coding agent runtime.")
}

func TestTaskToolRunsParallelBatch(t *testing.T) {
	manager := newTestManagerWithOptions(
		llmfake.New([][]llm.Chunk{
			{{Content: "alpha summary", Done: true}},
			{{Content: "beta summary", Done: true}},
		}),
		tool.NewRegistry(),
		ManagerOptions{MaxChildrenPerTurn: 2, DefaultTimeoutSeconds: 5},
	)
	taskTool := NewTaskTool(manager)

	result, err := taskTool.Execute(context.Background(), []byte(`{
		"mode":"parallel",
		"tasks":[
			{"description":"alpha","prompt":"inspect alpha","subagent_type":"research","task_id":"alpha"},
			{"description":"beta","prompt":"inspect beta","subagent_type":"research","task_id":"beta"}
		]
	}`))

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.OK)
	assert.Contains(t, result.Output, `<task_result mode="parallel" status="completed">`)
	assert.Contains(t, result.Output, `<child id="alpha" type="research" status="completed"`)
	assert.Contains(t, result.Output, `<child id="beta" type="research" status="completed"`)
	assert.Contains(t, result.Output, "alpha summary")
	assert.Contains(t, result.Output, "beta summary")
}

func TestTaskToolSchemaRequiresLegacyPromptOrBatchTasks(t *testing.T) {
	taskTool := NewTaskTool(newUnconfiguredTestManager(nil, tool.NewRegistry()))

	schema, ok := taskTool.Schema().(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "object", schema["type"])
	assert.Equal(t, false, schema["additionalProperties"])

	oneOf, ok := schema["oneOf"].([]map[string]any)
	require.True(t, ok, "expected oneOf variants in schema: %#v", schema["oneOf"])
	require.Len(t, oneOf, 2)
	assert.Equal(t, []string{"prompt"}, oneOf[0]["required"])
	assert.Equal(t, []string{"tasks"}, oneOf[1]["required"])

	properties := schema["properties"].(map[string]any)
	tasks := properties["tasks"].(map[string]any)
	assert.Equal(t, 1, tasks["minItems"])
	itemSchema := tasks["items"].(map[string]any)
	assert.Equal(t, false, itemSchema["additionalProperties"])
	assert.Equal(t, []string{"prompt"}, itemSchema["required"])
}

func TestTaskToolRejectsUnsupportedMode(t *testing.T) {
	taskTool := NewTaskTool(newUnconfiguredTestManager(nil, tool.NewRegistry()))

	result, err := taskTool.Execute(context.Background(), []byte(`{
		"mode":"fanout",
		"tasks":[{"description":"alpha","prompt":"inspect alpha","subagent_type":"research"}]
	}`))

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.OK)
	assert.Equal(t, tool.StatusError, result.Status)
	assert.Contains(t, result.Error, "unsupported task mode")
}

func TestFormatBatchResultPreservesStructureWhenTruncated(t *testing.T) {
	result := &BatchResult{
		Mode:   TaskModeParallel,
		Status: "completed",
		Results: []SubagentResult{
			{
				TaskID:      "alpha",
				AgentType:   AgentTypeResearch,
				Status:      "completed",
				Iterations:  1,
				FinalAnswer: strings.Repeat("alpha ", 80),
			},
		},
	}

	output := formatBatchResult(result, 120)

	assert.Contains(t, output, "[subagent result truncated]")
	assert.Contains(t, output, `<child id="alpha" type="research" status="completed" iterations="1">`)
	assert.Contains(t, output, "</child>")
	assert.True(t, strings.HasSuffix(output, "</task_result>"), output)
	assert.Less(t, strings.Index(output, "</child>"), strings.LastIndex(output, "</task_result>"))
}

func TestTaskToolDescriptionTeachesDelegation(t *testing.T) {
	taskTool := NewTaskTool(newUnconfiguredTestManager(nil, tool.NewRegistry()))

	description := taskTool.Description()
	// Phase 2: the description must teach the model how to delegate well, not
	// just list capabilities. These anchors each cover one teaching point ported
	// from Claude Code's AgentTool prompt.
	for _, want := range []string{
		"isolated context",             // what a subagent is
		"When NOT to use",              // when NOT to delegate
		"read_file",                    // single-file → do it yourself
		"search_text",                  // named-pattern → do it yourself
		"NEVER delegate understanding", // the core prompt-writing rule
		"smart colleague who just walked into the room", // brief from zero context
		"resume_task_id", // Phase 4 resume hook
		// all three profiles surface via AgentTypeListing()
		"research:",
		"coder:",
		"reviewer:",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("expected task description to contain %q, got:\n%s", want, description)
		}
	}
}

func TestTaskToolRejectsInvalidSubagentType(t *testing.T) {
	taskTool := NewTaskTool(newUnconfiguredTestManager(nil, tool.NewRegistry()))

	result, err := taskTool.Execute(context.Background(), []byte(`{
		"description":"bad type",
		"prompt":"do work",
		"subagent_type":"planner"
	}`))

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.OK)
	assert.Equal(t, tool.StatusError, result.Status)
	assert.Contains(t, result.Error, "unsupported subagent_type")
}

// TestTaskToolRejectsResumeInBatchMode (Phase 4): resume_task_id is a
// single-task continuation hook; passing it together with tasks[] is a misuse
// and must come back as a protocol-safe error (no child runs).
func TestTaskToolRejectsResumeInBatchMode(t *testing.T) {
	taskTool := NewTaskTool(newUnconfiguredTestManager(nil, tool.NewRegistry()))

	result, err := taskTool.Execute(context.Background(), []byte(`{
		"resume_task_id":"task-A",
		"mode":"parallel",
		"tasks":[{"description":"a","prompt":"inspect a","subagent_type":"research"}]
	}`))

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.OK)
	assert.Equal(t, tool.StatusError, result.Status)
	assert.Contains(t, result.Error, "batch")
}
