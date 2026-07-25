package todo

import "testing"

const testSessionID = "test-session"

func newTestTaskStore(t *testing.T, baseDir string) *TaskStore {
	t.Helper()
	store, err := NewSessionTaskStore(baseDir, testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
