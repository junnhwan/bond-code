package todo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplaceAllPersistsSingleFile(t *testing.T) {
	store := newTestTaskStore(t, t.TempDir())
	require.NoError(t, store.ReplaceAll([]Task{
		{Subject: "First", Status: StatusPending},
		{ID: "impl", Subject: "Implement", Status: StatusInProgress, ActiveForm: "Implementing"},
	}))

	path := filepath.Join(store.BaseDir(), listFileName)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc diskList
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.Equal(t, listSchemaVersion, doc.SchemaVersion)
	require.Len(t, doc.Items, 2)
	assert.Equal(t, "1", doc.Items[0].ID)
	assert.Equal(t, "impl", doc.Items[1].ID)
	assert.Equal(t, "Implementing", doc.Items[1].ActiveForm)
}

func TestReplaceAllClearsWhenAllCompleted(t *testing.T) {
	store := newTestTaskStore(t, t.TempDir())
	require.NoError(t, store.ReplaceAll([]Task{
		{ID: "a", Subject: "A", Status: StatusCompleted},
		{ID: "b", Subject: "B", Status: StatusCompleted},
	}))
	items, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestReplaceAllRejectsMultipleInProgress(t *testing.T) {
	store := newTestTaskStore(t, t.TempDir())
	err := store.ReplaceAll([]Task{
		{Subject: "A", Status: StatusInProgress},
		{Subject: "B", Status: StatusInProgress},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one in_progress")
}

func TestReplaceAllRejectsMissingSubject(t *testing.T) {
	store := newTestTaskStore(t, t.TempDir())
	err := store.ReplaceAll([]Task{{ID: "1", Status: StatusPending}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subject")
}

func TestSwitchSessionIsolatesLists(t *testing.T) {
	base := t.TempDir()
	store, err := NewSessionTaskStore(base, "session-a")
	require.NoError(t, err)
	require.NoError(t, store.ReplaceAll([]Task{{ID: "a1", Subject: "A only", Status: StatusInProgress}}))

	require.NoError(t, store.SwitchSession("session-b"))
	items, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, items)
	require.NoError(t, store.ReplaceAll([]Task{{ID: "b1", Subject: "B only", Status: StatusPending}}))

	require.NoError(t, store.SwitchSession("session-a"))
	items, err = store.List()
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "A only", items[0].Subject)
}

func TestLoadMigratesLegacyPerTaskFiles(t *testing.T) {
	base := t.TempDir()
	store, err := NewSessionTaskStore(base, "legacy")
	require.NoError(t, err)

	legacy := `{"id":"todo_001","subject":"Old plan item","status":"in_progress","blocked_by":[]}`
	require.NoError(t, os.WriteFile(filepath.Join(store.BaseDir(), "todo_001.json"), []byte(legacy), 0o644))

	items, err := store.List()
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "todo_001", items[0].ID)
	assert.Equal(t, "Old plan item", items[0].Subject)

	_, err = os.Stat(filepath.Join(store.BaseDir(), listFileName))
	require.NoError(t, err)
}

func TestSummaryAndFormatForPrompt(t *testing.T) {
	store := newTestTaskStore(t, t.TempDir())
	require.NoError(t, store.ReplaceAll([]Task{
		{ID: "1", Subject: "Write tests", Status: StatusInProgress, ActiveForm: "Writing tests"},
		{ID: "2", Subject: "Ship", Status: StatusPending},
	}))

	summary, err := store.Summary()
	require.NoError(t, err)
	assert.Contains(t, summary, "2 tasks")
	assert.Contains(t, summary, "1 in_progress")

	text, err := store.FormatForPrompt()
	require.NoError(t, err)
	assert.Contains(t, text, "# Tasks")
	assert.Contains(t, text, "Write tests")
	assert.Contains(t, text, "Writing tests")
}
