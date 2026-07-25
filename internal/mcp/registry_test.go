package mcp

import (
	"context"
	"testing"

	"github.com/junnhwan/bond-code/internal/tool"
)

func TestRefreshRegistryAddsNamespacedTools(t *testing.T) {
	server := buildFakeMCPServer(t)
	manager := NewProcessManager()
	if err := manager.Connect(context.Background(), "fake", server, nil); err != nil {
		t.Fatalf("connect fake server: %v", err)
	}
	defer manager.Disconnect(context.Background(), "fake")

	registry := tool.NewRegistry()
	count, err := RefreshRegistry(context.Background(), manager, registry, RefreshOptions{NamespaceTools: true})
	if err != nil {
		t.Fatalf("refresh registry: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one registered tool, got %d", count)
	}
	if _, ok := registry.Get("mcp__fake__fake_echo"); !ok {
		t.Fatalf("expected namespaced fake tool, got names %v", registry.Names())
	}
}
