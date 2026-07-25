package subagent

import (
	"context"
	"testing"
)

func TestCancelTaskInvokesRegisteredCancel(t *testing.T) {
	m := newUnconfiguredTestManager(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	m.runningTasks.Store("sub-1", context.CancelFunc(cancel))

	if !m.CancelTask("sub-1") {
		t.Fatal("expected CancelTask to find and cancel sub-1")
	}
	if ctx.Err() == nil {
		t.Fatal("expected the registered ctx to be cancelled")
	}
	// The entry is consumed, so a second call finds nothing.
	if m.CancelTask("sub-1") {
		t.Fatal("expected CancelTask false after the entry was consumed")
	}
}

func TestCancelTaskEmptyOrUnknownIsNoOp(t *testing.T) {
	m := newUnconfiguredTestManager(nil, nil)
	if m.CancelTask("") {
		t.Fatal("expected CancelTask('') to be a no-op (false)")
	}
	if m.CancelTask("unknown") {
		t.Fatal("expected CancelTask on unknown id to be false")
	}
}
