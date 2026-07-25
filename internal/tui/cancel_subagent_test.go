package tui

import "testing"

func TestCancelSelectedSubagentInvokesCallback(t *testing.T) {
	var cancelled string
	m := NewModel(Config{
		CancelSubagent: func(id string) bool {
			cancelled = id
			return true
		},
	})
	m.cancelSelectedSubagent("sub-123")
	if cancelled != "sub-123" {
		t.Fatalf("expected CancelSubagent called with sub-123, got %q", cancelled)
	}
}

func TestCancelSelectedSubagentNilCallbackIsNoOp(t *testing.T) {
	m := NewModel(Config{})
	// Must not panic when no callback is wired (headless / test model).
	_ = m.cancelSelectedSubagent("sub-123")
}
