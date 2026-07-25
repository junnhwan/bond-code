package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/session"
	"github.com/junnhwan/bond-code/internal/todo"
	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/junnhwan/bond-code/internal/tool/builtin"
	"github.com/junnhwan/bond-code/internal/undo"
)

// SwitchSession reloads the target's history snapshot and rebuilds its
// per-session context stores bound to the target id; SessionID switches onto it
// (design test matrix §1, invariants 2/3/5).
func TestSwitchSessionReloadsTargetHistoryAndRebuildsStores(t *testing.T) {
	dir := t.TempDir()
	store := session.NewJSONLStore(dir)
	current := "session-current"
	target := "session-target"

	// Seed the target with an event (so it is non-empty) and a history snapshot
	// (what SwitchSession reloads — same convention as bootstrap --resume).
	wantHistory := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "hello from target"},
		{Role: llm.RoleAssistant, Content: "hi there"},
	}
	if err := store.Append(session.Event{
		SessionID: target,
		Type:      "message",
		Message:   &session.Message{Role: session.RoleUser, Content: "seed", CreatedAt: time.Now()},
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("append seed event: %v", err)
	}
	data, err := json.Marshal(wantHistory)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if err := store.WriteHistory(target, data); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	app := &App{
		Sessions:  store,
		SessionID: current,
		Config:    &config.Config{Session: config.SessionConfig{Dir: dir}, Context: config.ContextConfig{Enabled: true}},
	}
	prevSummary := app.ContextSummary
	prevManager := app.ContextManager

	if err := app.SwitchSession(target); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if app.SessionID != target {
		t.Fatalf("expected SessionID=%q, got %q", target, app.SessionID)
	}
	got := app.History()
	if len(got) != len(wantHistory) || got[1].Content != "hello from target" {
		t.Fatalf("expected target history reloaded, got %#v", got)
	}
	// Invariant 3: per-session stores rebuilt (new objects, not the old ones).
	if app.ContextSummary == nil || app.ContextSummary == prevSummary {
		t.Fatal("expected a NEW summary store bound to the target session")
	}
	if app.ContextManager == nil || app.ContextManager == prevManager {
		t.Fatal("expected a NEW context manager bound to the target session")
	}
	// The rebuilt summary store writes to the target's file, proving it is bound
	// to the target id rather than the pre-switch current id.
	if err := app.ContextSummary.Save(contextx.SummaryArtifact{Version: 1, Summary: "post-switch"}); err != nil {
		t.Fatalf("save summary: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "context-summaries", target+".json")); err != nil {
		t.Fatalf("expected target summary file, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "context-summaries", current+".json")); !os.IsNotExist(err) {
		t.Fatalf("expected NO current summary file, got: %v", err)
	}
}

func TestSwitchSessionCorruptHistoryFallsBackToEmptyHistory(t *testing.T) {
	dir := t.TempDir()
	store := session.NewJSONLStore(dir)
	current := "session-current"
	target := "session-target"

	if err := store.Append(session.Event{
		SessionID: target,
		Type:      "message",
		Message:   &session.Message{Role: session.RoleUser, Content: "seed", CreatedAt: time.Now()},
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("append seed event: %v", err)
	}
	if err := store.WriteHistory(target, []byte(`{"not":"a message list"`)); err != nil {
		t.Fatalf("write corrupt snapshot: %v", err)
	}

	app := &App{
		Sessions:  store,
		SessionID: current,
		history:   []llm.Message{{Role: llm.RoleUser, Content: "current history"}},
		Config:    &config.Config{Session: config.SessionConfig{Dir: dir}, Context: config.ContextConfig{Enabled: true}},
	}

	if err := app.SwitchSession(target); err != nil {
		t.Fatalf("switch should tolerate corrupt target history: %v", err)
	}
	if app.SessionID != target {
		t.Fatalf("expected SessionID=%q, got %q", target, app.SessionID)
	}
	if got := app.History(); len(got) != 0 {
		t.Fatalf("expected empty target history after corrupt snapshot fallback, got %#v", got)
	}
	if warning := app.StatusSnapshot().Context.Stats; !strings.Contains(warning, "history snapshot warning") || !strings.Contains(warning, target) {
		t.Fatalf("expected corrupt snapshot warning in status, got %q", warning)
	}
}

// A failed switch (unknown target) must leave every piece of per-session state
// untouched (invariant 6: atomic rollback).
func TestSwitchSessionUnknownTargetRollsBack(t *testing.T) {
	dir := t.TempDir()
	store := session.NewJSONLStore(dir)
	current := "session-current"

	app := &App{
		Sessions:  store,
		SessionID: current,
		history:   []llm.Message{{Role: llm.RoleUser, Content: "current history"}},
		Config:    &config.Config{Session: config.SessionConfig{Dir: dir}, Context: config.ContextConfig{Enabled: true}},
	}
	prevSummary := contextx.NewSummaryStore(dir, current)
	prevManager := contextx.NewManager(contextx.NewGovernor(contextx.GovernorConfig{}))
	app.ContextSummary = prevSummary
	app.ContextManager = prevManager

	if err := app.SwitchSession("does-not-exist"); err == nil {
		t.Fatal("expected error switching to an unknown session")
	}
	if app.SessionID != current {
		t.Fatalf("expected SessionID unchanged after failed switch, got %q", app.SessionID)
	}
	if got := app.History(); len(got) != 1 || got[0].Content != "current history" {
		t.Fatalf("expected history unchanged after failed switch, got %#v", got)
	}
	if app.ContextSummary != prevSummary {
		t.Fatal("expected summary store unchanged after failed switch")
	}
	if app.ContextManager != prevManager {
		t.Fatal("expected context manager unchanged after failed switch")
	}
}

// While the agent is mid-turn (RunWithEvents holds a.mu), SwitchSession must
// refuse with the busy sentinel and touch nothing (invariant 1).
func TestSwitchSessionBusyWhileAgentRunning(t *testing.T) {
	dir := t.TempDir()
	store := session.NewJSONLStore(dir)
	app := &App{
		Sessions:  store,
		SessionID: "session-current",
		Config:    &config.Config{Session: config.SessionConfig{Dir: dir}, Context: config.ContextConfig{Enabled: true}},
	}
	// Simulate RunWithEvents holding a.mu for the whole turn.
	app.mu.Lock()
	defer app.mu.Unlock()

	err := app.SwitchSession("session-target")
	if !IsSessionBusy(err) {
		t.Fatalf("expected busy error, got %v", err)
	}
	if app.SessionID != "session-current" {
		t.Fatalf("expected SessionID unchanged while busy, got %q", app.SessionID)
	}
}

func TestNewSessionClearsHistoryAndRebuildsStores(t *testing.T) {
	dir := t.TempDir()
	current := "session-current"
	app := &App{
		Sessions:  session.NewJSONLStore(dir),
		SessionID: current,
		history:   []llm.Message{{Role: llm.RoleUser, Content: "current history"}},
		Config:    &config.Config{Session: config.SessionConfig{Dir: dir}, Context: config.ContextConfig{Enabled: true}},
	}
	prevSummary := contextx.NewSummaryStore(dir, current)
	prevManager := contextx.NewManager(contextx.NewGovernor(contextx.GovernorConfig{}))
	app.ContextSummary = prevSummary
	app.ContextManager = prevManager

	newID, err := app.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if newID == "" || newID == current {
		t.Fatalf("expected a fresh session id, got %q", newID)
	}
	if app.SessionID != newID {
		t.Fatalf("expected app SessionID=%q, got %q", newID, app.SessionID)
	}
	if len(app.History()) != 0 {
		t.Fatalf("expected fresh session history, got %#v", app.History())
	}
	if app.ContextSummary == nil || app.ContextSummary == prevSummary {
		t.Fatal("expected a NEW summary store bound to the fresh session")
	}
	if app.ContextManager == nil || app.ContextManager == prevManager {
		t.Fatal("expected a NEW context manager bound to the fresh session")
	}
	if err := app.ContextSummary.Save(contextx.SummaryArtifact{Version: 1, Summary: "fresh"}); err != nil {
		t.Fatalf("save summary: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "context-summaries", newID+".json")); err != nil {
		t.Fatalf("expected fresh summary file, got: %v", err)
	}
}

func TestNewSessionPrunesEmptyAbandonedSession(t *testing.T) {
	dir := t.TempDir()
	store := session.NewJSONLStore(dir)
	empty := "session-empty-old"
	kept := "session-with-msgs"
	if err := store.Append(session.Event{SessionID: empty, Type: "message"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(session.Event{
		SessionID: kept,
		Type:      "message",
		Message:   &session.Message{Role: session.RoleUser, Content: "keep"},
	}); err != nil {
		t.Fatal(err)
	}

	app := &App{Sessions: store, SessionID: empty, Config: &config.Config{Session: config.SessionConfig{Dir: dir}}}
	if _, err := app.NewSession(); err != nil {
		t.Fatalf("new session from empty: %v", err)
	}
	ids, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if id == empty {
			t.Fatalf("empty abandoned session should be pruned, list=%v", ids)
		}
	}
	if _, err := store.Load(kept); err != nil {
		t.Fatalf("session with messages must remain: %v", err)
	}

	// Content-bearing session is not pruned on /clear.
	app.SessionID = kept
	if _, err := app.NewSession(); err != nil {
		t.Fatalf("new session from kept: %v", err)
	}
	if _, err := store.Load(kept); err != nil {
		t.Fatalf("expected kept session to survive /clear: %v", err)
	}
}

func TestNewAndSwitchSessionKeepTodosIsolated(t *testing.T) {
	dataDir := t.TempDir()
	configPath := writeBootstrapTestConfig(t, dataDir)
	application, err := Bootstrap(Options{ConfigPath: configPath, UseFakeLLM: true})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := application.Chat(context.Background(), "seed first session"); err != nil {
		t.Fatalf("seed first session: %v", err)
	}
	firstID := application.SessionID
	if err := application.TaskStore.ReplaceAll([]todo.Task{{
		ID: "todo_001", Subject: "first todo", Status: todo.TaskStatusInProgress,
	}}); err != nil {
		t.Fatalf("write first todo: %v", err)
	}

	secondID, err := application.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	secondTasks, err := application.TaskStore.List()
	if err != nil {
		t.Fatalf("list second todo: %v", err)
	}
	if len(secondTasks) != 0 {
		t.Fatalf("new session inherited first todo: %#v", secondTasks)
	}
	if err := application.TaskStore.ReplaceAll([]todo.Task{{
		ID: "todo_001", Subject: "second todo", Status: todo.TaskStatusPending,
	}}); err != nil {
		t.Fatalf("write second todo: %v", err)
	}
	if err := application.Sessions.Append(session.Event{
		SessionID: secondID,
		Type:      "message",
		Message:   &session.Message{Role: session.RoleUser, Content: "seed second session"},
	}); err != nil {
		t.Fatalf("seed second session: %v", err)
	}

	if err := application.SwitchSession(firstID); err != nil {
		t.Fatalf("switch to first session: %v", err)
	}
	firstTasks, err := application.TaskStore.List()
	if err != nil {
		t.Fatalf("list restored first todo: %v", err)
	}
	if len(firstTasks) != 1 || firstTasks[0].Subject != "first todo" {
		t.Fatalf("first session todo was not restored: %#v", firstTasks)
	}

	if err := application.SwitchSession(secondID); err != nil {
		t.Fatalf("switch to second session: %v", err)
	}
	secondTasks, err = application.TaskStore.List()
	if err != nil {
		t.Fatalf("list restored second todo: %v", err)
	}
	if len(secondTasks) != 1 || secondTasks[0].Subject != "second todo" {
		t.Fatalf("second session todo was not restored: %#v", secondTasks)
	}
}

func TestNewSessionBusyWhileAgentRunning(t *testing.T) {
	app := &App{
		Sessions:  session.NewJSONLStore(t.TempDir()),
		SessionID: "session-current",
	}
	app.mu.Lock()
	defer app.mu.Unlock()

	newID, err := app.NewSession()
	if !IsSessionBusy(err) {
		t.Fatalf("expected busy error, got id=%q err=%v", newID, err)
	}
	if app.SessionID != "session-current" {
		t.Fatalf("expected SessionID unchanged while busy, got %q", app.SessionID)
	}
}

func TestSwitchSessionRebindsRegisteredSessionTools(t *testing.T) {
	dir := t.TempDir()
	store := session.NewJSONLStore(dir)
	target := "session-tool-target"
	if err := store.Append(session.Event{SessionID: target, Type: "message"}); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	registry := tool.NewRegistry()
	probe := &sessionBindingProbeTool{name: "session_probe"}
	if err := registry.Register(probe); err != nil {
		t.Fatal(err)
	}
	application := &App{
		Sessions:  store,
		SessionID: "session-current",
		Tools:     registry,
		Config:    &config.Config{Session: config.SessionConfig{Dir: dir}},
	}

	if err := application.SwitchSession(target); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if got := probe.sessionID(); got != target {
		t.Fatalf("session-bound tool remained on %q, want %q", got, target)
	}
}

func TestNewSessionRebindsRulesAndRegisteredSessionTools(t *testing.T) {
	dir := t.TempDir()
	store := session.NewJSONLStore(dir)
	current := "session-current"
	registry := tool.NewRegistry()
	probe := &sessionBindingProbeTool{name: "session_probe"}
	if err := registry.Register(probe); err != nil {
		t.Fatal(err)
	}
	rules := session.NewRuleSource(store, current)
	application := &App{
		Sessions:   store,
		SessionID:  current,
		Tools:      registry,
		RuleSource: rules,
		Config:     &config.Config{Session: config.SessionConfig{Dir: dir}},
	}

	newID, err := application.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if got := rules.SessionID(); got != newID {
		t.Fatalf("rule source remained on %q, want %q", got, newID)
	}
	if got := probe.sessionID(); got != newID {
		t.Fatalf("session-bound tool remained on %q, want %q", got, newID)
	}
}

type sessionBindingProbeTool struct {
	name  string
	bound string
}

func (t *sessionBindingProbeTool) Name() string                        { return t.name }
func (t *sessionBindingProbeTool) Description() string                 { return "session binding probe" }
func (t *sessionBindingProbeTool) Schema() any                         { return map[string]any{} }
func (t *sessionBindingProbeTool) Risk(json.RawMessage) tool.RiskLevel { return tool.RiskLow }
func (t *sessionBindingProbeTool) Execute(context.Context, json.RawMessage) (*tool.Result, error) {
	return tool.Success(t.name, "ok", "ok"), nil
}
func (t *sessionBindingProbeTool) BindSession(sessionID string) { t.bound = sessionID }
func (t *sessionBindingProbeTool) sessionID() string            { return t.bound }

func TestSwitchSessionDebugLoggerStageFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	store := session.NewJSONLStore(dir)
	current := "session-current"
	target := "session-target"
	if err := store.Append(session.Event{SessionID: target, Type: "message"}); err != nil {
		t.Fatal(err)
	}
	oldLogger := &trackingDebugLogger{}
	oldHistory := []llm.Message{{Role: llm.RoleUser, Content: "keep current"}}
	rules := session.NewRuleSource(store, current)
	application := &App{
		Sessions:    store,
		SessionID:   current,
		RuleSource:  rules,
		history:     oldHistory,
		Config:      &config.Config{Session: config.SessionConfig{Dir: dir}},
		debugLogger: oldLogger,
		debugLoggerFactory: func(string) (observe.Logger, error) {
			return nil, os.ErrPermission
		},
	}

	err := application.SwitchSession(target)
	if err == nil {
		t.Fatal("expected debug logger staging failure")
	}
	if application.SessionID != current {
		t.Fatalf("session changed on failed staging: %q", application.SessionID)
	}
	if got := application.History(); len(got) != 1 || got[0].Content != oldHistory[0].Content {
		t.Fatalf("history changed on failed staging: %#v", got)
	}
	if got := rules.SessionID(); got != current {
		t.Fatalf("rules rebound on failed staging: %q", got)
	}
	if oldLogger.closed {
		t.Fatal("active debug logger was closed on failed staging")
	}
}

type trackingDebugLogger struct {
	closed bool
}

func (*trackingDebugLogger) Log(observe.Record)       {}
func (l *trackingDebugLogger) Close() error           { l.closed = true; return nil }
func (*trackingDebugLogger) Verbose() observe.Verbose { return observe.VerboseDefault }

func guardedSessionRegistry(t *testing.T, sessionID string) (*tool.Registry, tool.Tool, tool.Tool) {
	t.Helper()
	observations := builtin.NewObservationStore()
	observations.BindSession(sessionID)
	read, err := builtin.NewReadFileToolWithObservations(observations)
	if err != nil {
		t.Fatal(err)
	}
	write, err := builtin.NewWriteFileToolWithObservations(observations, undo.NewStore(4))
	if err != nil {
		t.Fatal(err)
	}
	registry := tool.NewRegistry()
	if err := registry.Register(read); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(write); err != nil {
		t.Fatal(err)
	}
	return registry, read, write
}

func executeSessionFileTool(t *testing.T, candidate tool.Tool, input any) error {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	_, err = candidate.Execute(context.Background(), raw)
	return err
}

func TestSuccessfulSessionSwitchClearsFileObservations(t *testing.T) {
	dir := t.TempDir()
	current := "current"
	target := "target"
	sessions := session.NewJSONLStore(dir)
	if err := sessions.Append(session.Event{SessionID: target, Type: "message"}); err != nil {
		t.Fatal(err)
	}
	registry, read, write := guardedSessionRegistry(t, current)
	application := &App{Sessions: sessions, SessionID: current, Tools: registry, Config: &config.Config{Session: config.SessionConfig{Dir: dir}}}
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executeSessionFileTool(t, read, builtin.ReadFileInput{Path: path}); err != nil {
		t.Fatal(err)
	}
	if err := application.SwitchSession(target); err != nil {
		t.Fatal(err)
	}
	if err := executeSessionFileTool(t, write, builtin.WriteFileInput{Path: path, Content: "new"}); err == nil {
		t.Fatal("write succeeded with pre-switch observation")
	}
}

func TestFailedSessionSwitchPreservesFileObservations(t *testing.T) {
	dir := t.TempDir()
	current := "current"
	target := "target"
	sessions := session.NewJSONLStore(dir)
	if err := sessions.Append(session.Event{SessionID: target, Type: "message"}); err != nil {
		t.Fatal(err)
	}
	registry, read, write := guardedSessionRegistry(t, current)
	application := &App{Sessions: sessions, SessionID: current, Tools: registry, Config: &config.Config{Session: config.SessionConfig{Dir: dir}}, debugLoggerFactory: func(string) (observe.Logger, error) { return nil, os.ErrPermission }}
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executeSessionFileTool(t, read, builtin.ReadFileInput{Path: path}); err != nil {
		t.Fatal(err)
	}
	if err := application.SwitchSession(target); err == nil {
		t.Fatal("expected switch failure")
	}
	if err := executeSessionFileTool(t, write, builtin.WriteFileInput{Path: path, Content: "new"}); err != nil {
		t.Fatalf("failed switch invalidated observation: %v", err)
	}
}
