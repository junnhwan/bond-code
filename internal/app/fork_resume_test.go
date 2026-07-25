package app

import (
	"testing"

	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/session"
	"github.com/junnhwan/bond-code/internal/tool"
)

// ForkAndResume forks the session into a new branch and rebuilds the messages
// along the path to eventID; the rebuilt messages equal NavigateToEvent's, the
// app switches onto the new branch, and the original branch is left intact
// (design test matrix §1, invariants 1 & 2).
func TestForkAndResumeProducesNewBranchAndRebuiltMessages(t *testing.T) {
	dir := t.TempDir()
	store := session.NewJSONLStore(dir)
	registry := tool.NewRegistry()
	probe := &sessionBindingProbeTool{name: "session_probe"}
	if err := registry.Register(probe); err != nil {
		t.Fatal(err)
	}
	rules := session.NewRuleSource(store, "src")
	previousManager := contextx.NewManager(contextx.NewGovernor(contextx.GovernorConfig{}))
	app := &App{
		Sessions:       store,
		SessionID:      "src",
		Tools:          registry,
		RuleSource:     rules,
		ContextManager: previousManager,
		Config:         &config.Config{Session: config.SessionConfig{Dir: dir}, Context: config.ContextConfig{Enabled: true}},
	}

	// Forked event tree:
	//   a (user q1) -> b (assistant a1) -> c (user q2)   [path we resume to]
	//                                \-> e (user q2')   [abandoned sibling]
	events := []session.Event{
		{EventID: "a", SessionID: "src", Type: "message", Message: &session.Message{Role: session.RoleUser, Content: "q1"}},
		{EventID: "b", SessionID: "src", Type: "message", ParentID: "a", Message: &session.Message{Role: session.RoleAssistant, Content: "a1"}},
		{EventID: "c", SessionID: "src", Type: "message", ParentID: "b", Message: &session.Message{Role: session.RoleUser, Content: "q2"}},
		{EventID: "e", SessionID: "src", Type: "message", ParentID: "b", Message: &session.Message{Role: session.RoleUser, Content: "q2-prime"}},
	}
	for _, e := range events {
		if err := store.Append(e); err != nil {
			t.Fatalf("append %s: %v", e.EventID, err)
		}
	}

	want, err := app.NavigateToEvent("src", "c")
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}

	newID, got, err := app.ForkAndResume("src", "c")
	if err != nil {
		t.Fatalf("fork and resume: %v", err)
	}
	if newID == "" || newID == "src" {
		t.Fatalf("expected a new session id distinct from src, got %q", newID)
	}
	if !messagesEqual(got, want) {
		t.Fatalf("rebuilt messages should equal NavigateToEvent result:\n got=%#v\n want=%#v", got, want)
	}
	// App switched onto the new branch with the rebuilt context.
	if app.SessionID != newID {
		t.Fatalf("expected app SessionID switched to %q, got %q", newID, app.SessionID)
	}
	if !messagesEqual(app.History(), got) {
		t.Fatalf("expected app history to hold rebuilt messages, got %#v", app.History())
	}
	if app.ContextManager == nil || app.ContextManager == previousManager {
		t.Fatal("expected fork to rebuild the context manager for the new session")
	}
	if got := rules.SessionID(); got != newID {
		t.Fatalf("rule source remained on %q after fork, want %q", got, newID)
	}
	if got := probe.sessionID(); got != newID {
		t.Fatalf("session-bound tool remained on %q after fork, want %q", got, newID)
	}

	// Invariant 2: original branch file untouched and still resolvable.
	origPath, err := app.NavigateToEvent("src", "c")
	if err != nil {
		t.Fatalf("original session must remain intact after fork: %v", err)
	}
	if !messagesEqual(origPath, want) {
		t.Fatalf("original branch path changed after fork")
	}
	// Fork copies the whole session, so the forked file carries the same tree,
	// including the abandoned sibling event e.
	forked, err := store.Load(newID)
	if err != nil {
		t.Fatalf("load forked session: %v", err)
	}
	if len(forked) != len(events) {
		t.Fatalf("forked session should carry all %d events, got %d", len(events), len(forked))
	}
}

func messagesEqual(a, b []llm.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Role != b[i].Role || a[i].Content != b[i].Content {
			return false
		}
	}
	return true
}
