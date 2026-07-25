package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustAppendEvent(t *testing.T, store *JSONLStore, e Event) {
	t.Helper()
	if err := store.Append(e); err != nil {
		t.Fatalf("append: %v", err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestExportTextWritesReadableTranscript(t *testing.T) {
	dir := t.TempDir()
	store := NewJSONLStore(filepath.Join(dir, "sessions"))
	base := time.Date(2026, 6, 20, 14, 30, 0, 0, time.UTC)
	mustAppendEvent(t, store, Event{
		SessionID: "s1", Type: "message",
		Message:   &Message{Role: RoleUser, Content: "hello there", CreatedAt: base},
		CreatedAt: base,
	})
	mustAppendEvent(t, store, Event{
		SessionID: "s1", Type: "message",
		Message:   &Message{Role: RoleAssistant, Content: "hi back", CreatedAt: base.Add(time.Second)},
		CreatedAt: base.Add(time.Second),
	})
	mustAppendEvent(t, store, Event{
		SessionID: "s1", Type: "tool_result",
		ToolCall:  &ToolCall{Name: "read_file", Input: `{"path":"a.go"}`, Output: "package main", CreatedAt: base.Add(2 * time.Second)},
		CreatedAt: base.Add(2 * time.Second),
	})

	target := filepath.Join(dir, "out", "s1.txt")
	summary, err := store.ExportText("s1", target)
	if err != nil {
		t.Fatalf("export text: %v", err)
	}
	if summary.UserMessages != 1 || summary.AssistantMessages != 1 || summary.ToolCalls != 1 || summary.Path != target {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	body := mustReadFile(t, target)
	for _, want := range []string{
		"# BondCode Session Export", "session: s1", "## user", "hello there",
		"## assistant", "hi back", "tool · read_file  [done]", "package main", "input:",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("export missing %q\n---\n%s", want, body)
		}
	}
}

func TestExportTextMarksFailedToolCalls(t *testing.T) {
	dir := t.TempDir()
	store := NewJSONLStore(dir)
	mustAppendEvent(t, store, Event{
		SessionID: "s1", Type: "tool_result",
		ToolCall: &ToolCall{Name: "run_command", Error: "boom"},
	})
	target := filepath.Join(dir, "s1.txt")
	if _, err := store.ExportText("s1", target); err != nil {
		t.Fatalf("export: %v", err)
	}
	body := mustReadFile(t, target)
	if !strings.Contains(body, "[failed]") || !strings.Contains(body, "boom") {
		t.Fatalf("expected [failed] marker and error in:\n%s", body)
	}
}

func TestExportTextSkipsStreamingAgentEvents(t *testing.T) {
	dir := t.TempDir()
	store := NewJSONLStore(dir)
	// Simulate a real turn's session log: a user message, a burst of streaming
	// agent_event fragments (model/reasoning chunks), then the full assistant
	// message recorded once at turn end.
	mustAppendEvent(t, store, Event{SessionID: "s1", Type: "message", Message: &Message{Role: RoleUser, Content: "what is 1+1"}})
	for _, chunk := range []string{"The", " answer", " is", " 2."} {
		mustAppendEvent(t, store, Event{
			SessionID: "s1", Type: "agent_event",
			AgentEvent: &AgentEvent{Type: "model_chunk", Message: chunk},
		})
	}
	mustAppendEvent(t, store, Event{SessionID: "s1", Type: "message", Message: &Message{Role: RoleAssistant, Content: "The answer is 2."}})

	target := filepath.Join(dir, "s1.txt")
	summary, err := store.ExportText("s1", target)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	body := mustReadFile(t, target)

	// The chunks must not appear as their own sections.
	for _, chunk := range []string{"## model_chunk", "## agent_event", " The"} {
		if strings.Contains(body, chunk) {
			t.Errorf("streaming fragment leaked into export (%q):\n%s", chunk, body)
		}
	}
	// The single assistant reply should render as one continuous section.
	if !strings.Contains(body, "## assistant\n\nThe answer is 2.") {
		t.Fatalf("expected one continuous assistant section, got:\n%s", body)
	}
	if summary.UserMessages != 1 || summary.AssistantMessages != 1 || summary.ToolCalls != 0 {
		t.Fatalf("streaming events should not inflate counts: %+v", summary)
	}
}

func TestExportTextCreatesTargetDirectory(t *testing.T) {
	dir := t.TempDir()
	store := NewJSONLStore(dir)
	mustAppendEvent(t, store, Event{SessionID: "s1", Type: "message", Message: &Message{Role: RoleUser, Content: "x"}})
	target := filepath.Join(dir, "nested", "deep", "s1.txt")
	if _, err := store.ExportText("s1", target); err != nil {
		t.Fatalf("export into nested dir: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected file at %s: %v", target, err)
	}
}
