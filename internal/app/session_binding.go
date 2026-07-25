package app

import (
	"fmt"

	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/observe"
)

// sessionBinder is implemented by long-lived runtime components that capture
// the active session identity. Session transitions notify every registered tool
// implementing this interface, mirroring the centralized session-switch signal
// used by mature agent runtimes.
type sessionBinder interface {
	BindSession(sessionID string)
}

type debugLoggerFactory func(sessionID string) (observe.Logger, error)

type stagedSessionState struct {
	contextManager *contextx.Manager
	contextSummary *contextx.SummaryStore
	debugLogger    observe.Logger
	replaceContext bool
	replaceDebug   bool
}

// stageSessionState constructs every fallible per-session dependency without
// mutating the active app. Callers may then rebind the todo store and commit all
// in-memory pointers together; on failure, discard closes staged resources.
func (a *App) stageSessionState(sessionID string) (*stagedSessionState, error) {
	staged := &stagedSessionState{}
	if a.Config != nil && a.Config.Context.Enabled {
		dir := a.Config.Session.Dir
		staged.contextSummary = contextx.NewSummaryStore(dir, sessionID)
		store := contextx.NewToolResultStore(dir, sessionID)
		staged.contextManager = contextx.NewManager(contextx.NewGovernor(
			governorConfigFrom(a.Config.Context, store),
		))
		staged.replaceContext = true
	}
	if a.debugLoggerFactory != nil {
		logger, err := a.debugLoggerFactory(sessionID)
		if err != nil {
			return nil, fmt.Errorf("open debug trace for session %q: %w", sessionID, err)
		}
		staged.debugLogger = logger
		staged.replaceDebug = true
	}
	return staged, nil
}

func (s *stagedSessionState) discard() {
	if s == nil || s.debugLogger == nil {
		return
	}
	_ = s.debugLogger.Close()
	s.debugLogger = nil
}

// commitSessionState is the single active-session commit point shared by
// /resume, /new, and fork-and-resume. It has no fallible operations: all
// resources are staged first, then identity and every dependent binding move
// together while the app turn mutex is held.
func (a *App) commitSessionState(sessionID string, history []llm.Message, historyWarning string, staged *stagedSessionState) {
	oldDebugLogger := a.debugLogger

	a.SessionID = sessionID
	if a.RuleSource != nil {
		a.RuleSource.Reset(sessionID)
	}
	a.bindSessionTools(sessionID)
	a.history = history
	a.historyWarning = historyWarning
	a.resetMeasuredUsage()

	if staged != nil && staged.replaceContext {
		a.ContextManager = staged.contextManager
		a.ContextSummary = staged.contextSummary
		if a.Agent != nil {
			a.Agent.SetContextManager(staged.contextManager, a.MaxContextTokens)
			a.Agent.SetContextSummaryStore(staged.contextSummary)
		}
	}
	if staged != nil && staged.replaceDebug {
		a.debugLogger = staged.debugLogger
		staged.debugLogger = nil
		if a.Agent != nil {
			a.Agent.SetDebugLogger(a.debugLogger)
		}
		if oldDebugLogger != nil && oldDebugLogger != a.debugLogger {
			observe.LogError("app.session-switch.debug-close", oldDebugLogger.Close())
		}
	}
}

func (a *App) bindSessionTools(sessionID string) {
	if a.Tools == nil {
		return
	}
	for _, name := range a.Tools.Names() {
		runtimeTool, ok := a.Tools.Get(name)
		if !ok {
			continue
		}
		if binder, ok := runtimeTool.(sessionBinder); ok {
			binder.BindSession(sessionID)
		}
	}
}
