package todo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	toolpkg "github.com/junnhwan/bond-code/internal/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTodoWriteToolReplacesPlanWithDeterministicIDs(t *testing.T) {
	store := newTestTaskStore(t, t.TempDir())
	tool := NewTodoWriteTool(store)

	result, err := tool.Execute(context.Background(), []byte(`{
		"items": [
			{"subject":"Inspect current todo code","status":"completed"},
			{"id":"implement","subject":"Implement todo_write","status":"in_progress","active_form":"Implementing todo_write"}
		]
	}`))

	require.NoError(t, err)
	require.True(t, result.OK, result.Output)
	assert.Contains(t, result.Output, "1")
	assert.Contains(t, result.Output, "implement")

	tasks, err := store.List()
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	assert.Equal(t, "1", tasks[0].ID)
	assert.Equal(t, "implement", tasks[1].ID)
	assert.Equal(t, "Implementing todo_write", tasks[1].ActiveForm)
}

func TestTodoWriteToolClearsWhenAllCompleted(t *testing.T) {
	store := newTestTaskStore(t, t.TempDir())
	tool := NewTodoWriteTool(store)

	result, err := tool.Execute(context.Background(), []byte(`{
		"items": [
			{"id":"a","subject":"Done A","status":"completed"},
			{"id":"b","subject":"Done B","status":"completed"}
		]
	}`))
	require.NoError(t, err)
	require.True(t, result.OK, result.Output)
	assert.Contains(t, strings.ToLower(result.Output), "empty")

	tasks, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestTodoReadToolReturnsSummary(t *testing.T) {
	store := newTestTaskStore(t, t.TempDir())
	require.NoError(t, store.ReplaceAll([]Task{
		{ID: "1", Subject: "Write tests", Status: StatusInProgress},
	}))
	tool := NewTodoReadTool(store)

	result, err := tool.Execute(context.Background(), []byte(`{"format":"summary"}`))

	require.NoError(t, err)
	require.True(t, result.OK, result.Output)
	assert.Contains(t, result.Output, "# Tasks")
	assert.Contains(t, result.Output, "Write tests")
}

func TestTodoReadToolReturnsJSON(t *testing.T) {
	store := newTestTaskStore(t, t.TempDir())
	require.NoError(t, store.ReplaceAll([]Task{
		{ID: "1", Subject: "Write tests", Status: StatusPending},
	}))
	tool := NewTodoReadTool(store)

	result, err := tool.Execute(context.Background(), []byte(`{"format":"json"}`))

	require.NoError(t, err)
	require.True(t, result.OK, result.Output)
	var payload struct {
		Items []Task `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.Output), &payload))
	require.Len(t, payload.Items, 1)
	assert.Equal(t, "Write tests", payload.Items[0].Subject)
}

func TestTodoWriteToolRejectsInvalidPlan(t *testing.T) {
	store := newTestTaskStore(t, t.TempDir())
	tool := NewTodoWriteTool(store)

	result, err := tool.Execute(context.Background(), []byte(`{"items":[{"id":"1","status":"pending"}]}`))

	require.NoError(t, err)
	require.False(t, result.OK)
	assert.Contains(t, strings.ToLower(result.Output), "subject")
}

func TestTodoWriteToolRiskIsLow(t *testing.T) {
	write := NewTodoWriteTool(newTestTaskStore(t, t.TempDir()))
	assert.Equal(t, toolpkg.RiskLow, write.Risk(nil))
}
