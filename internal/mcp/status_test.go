package mcp

import (
	"context"
	"testing"
)

func TestProcessManagerStatusReportsConnectedServer(t *testing.T) {
	server := buildFakeMCPServer(t)
	manager := NewProcessManager()

	if err := manager.Connect(context.Background(), "fake", server, nil); err != nil {
		t.Fatalf("connect fake server: %v", err)
	}
	defer manager.Disconnect(context.Background(), "fake")

	statuses := manager.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected one server status, got %#v", statuses)
	}
	got := statuses[0]
	if got.Name != "fake" || got.State != ServerStateConnected {
		t.Fatalf("unexpected status %#v", got)
	}
	if got.ToolCount != 0 {
		t.Fatalf("tool count should be zero before ListTools, got %d", got.ToolCount)
	}
}

func TestProcessManagerStatusRecordsListToolsError(t *testing.T) {
	server := buildFakeMCPServer(t, "list-error")
	manager := NewProcessManager()
	if err := manager.Connect(context.Background(), "fake", server, nil); err != nil {
		t.Fatalf("connect fake server: %v", err)
	}
	defer manager.Disconnect(context.Background(), "fake")

	_, err := manager.ListToolsForServer(context.Background(), "fake")
	if err == nil {
		t.Fatal("expected list tools error")
	}

	statuses := manager.Status()
	if statuses[0].LastError == "" || statuses[0].State != ServerStateError {
		t.Fatalf("expected error status, got %#v", statuses[0])
	}
}
