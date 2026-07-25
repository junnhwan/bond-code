package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/llm"
)

func TestFakeRuntimeSmokeCoversCoreAgentFeatures(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfig(t, dataDir)
	readPath := filepath.Join(t.TempDir(), "smoke.txt")
	if err := os.WriteFile(readPath, []byte("smoke file contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &runtimeSmokeClient{readPath: readPath}
	application, err := Bootstrap(Options{
		ConfigPath: configPath,
		Client:     client,
		AutoYes:    true,
	})
	if err != nil {
		t.Fatal(err)
	}

	var events []agent.Event
	direct, err := application.RunWithEvents(context.Background(), "answer directly for smoke", func(event agent.Event) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	if direct.FinalAnswer != "direct smoke answer" {
		t.Fatalf("expected direct answer, got %q", direct.FinalAnswer)
	}
	if !smokeHasEvent(events, agent.EventContextUpdated) {
		t.Fatalf("expected context governor event, got %#v", events)
	}

	for _, prompt := range []string{
		"read smoke file",
		"write todo smoke",
		"remember smoke memory",
		"is smoke memory visible?",
		"delegate smoke subagent",
	} {
		result, err := application.Chat(context.Background(), prompt)
		if err != nil {
			t.Fatalf("prompt %q failed: %v", prompt, err)
		}
		if strings.TrimSpace(result.FinalAnswer) == "" {
			t.Fatalf("prompt %q returned empty final answer", prompt)
		}
	}
	if !client.memoryVisible {
		t.Fatal("expected saved memory to be visible in a later prompt")
	}
	if !client.taskResultSeen {
		t.Fatal("expected parent agent to receive task subagent result")
	}
}

type runtimeSmokeClient struct {
	readPath       string
	memoryVisible  bool
	taskResultSeen bool
}

func (c *runtimeSmokeClient) Stream(ctx context.Context, messages []llm.Message, tools []llm.ToolSpec) (<-chan llm.Chunk, <-chan error) {
	chunks := make(chan llm.Chunk, 3)
	errs := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(errs)
		user := lastSmokeUser(messages)
		hasToolResult := smokeHasToolResultAfterLastUser(messages)
		switch {
		case strings.Contains(user, "delegate smoke subagent"):
			if hasToolResult {
				if smokeHasToolResultContentAfterLastUser(messages, "child smoke result") {
					c.taskResultSeen = true
				}
				chunks <- llm.Chunk{Content: "delegated smoke done", Done: true}
				break
			}
			chunks <- llm.Chunk{ToolCall: &llm.ToolCall{ID: "smoke-task", Name: "task", Arguments: `{"description":"smoke subagent","prompt":"smoke subagent","subagent_type":"research"}`}, Done: true}
		case strings.Contains(user, "smoke subagent"):
			chunks <- llm.Chunk{Content: "child smoke result", Done: true}
		case strings.Contains(user, "answer directly"):
			chunks <- llm.Chunk{Content: "direct smoke answer", Done: true}
		case strings.Contains(user, "read smoke file"):
			if hasToolResult {
				chunks <- llm.Chunk{Content: "read smoke file done", Done: true}
				break
			}
			args, _ := json.Marshal(map[string]string{"path": c.readPath})
			chunks <- llm.Chunk{ToolCall: &llm.ToolCall{ID: "smoke-read", Name: "read_file", Arguments: string(args)}, Done: true}
		case strings.Contains(user, "write todo smoke"):
			if hasToolResult {
				chunks <- llm.Chunk{Content: "todo smoke done", Done: true}
				break
			}
			chunks <- llm.Chunk{ToolCall: &llm.ToolCall{ID: "smoke-todo", Name: "todo_write", Arguments: `{"items":[{"id":"1","subject":"Smoke todo","status":"in_progress"}]}`}, Done: true}
		case strings.Contains(user, "remember smoke memory"):
			if hasToolResult {
				chunks <- llm.Chunk{Content: "memory smoke saved", Done: true}
				break
			}
			chunks <- llm.Chunk{ToolCall: &llm.ToolCall{ID: "smoke-memory", Name: "memory_save", Arguments: `{"type":"user","name":"Smoke pref","description":"Smoke memory preference","content":"Smoke memory preference is visible."}`}, Done: true}
		case strings.Contains(user, "is smoke memory visible"):
			// Memory rides on the current user turn as a <system-reminder>, not the
			// system prompt (keeps the system prefix cache-stable). `user` is the
			// last user message content, which carries the reminder.
			if strings.Contains(user, "Smoke memory preference is visible.") {
				c.memoryVisible = true
			}
			chunks <- llm.Chunk{Content: "memory visible", Done: true}
		default:
			chunks <- llm.Chunk{Content: "smoke fallback", Done: true}
		}
		errs <- nil
	}()
	return chunks, errs
}

func lastSmokeUser(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleUser {
			return messages[i].Content
		}
	}
	return ""
}

func smokeHasToolResultAfterLastUser(messages []llm.Message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleUser {
			return false
		}
		if messages[i].Role == llm.RoleTool {
			return true
		}
	}
	return false
}

func smokeHasToolResultContentAfterLastUser(messages []llm.Message, want string) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleUser {
			return false
		}
		if messages[i].Role == llm.RoleTool && strings.Contains(messages[i].Content, want) {
			return true
		}
	}
	return false
}

func smokeHasEvent(events []agent.Event, eventType agent.EventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
