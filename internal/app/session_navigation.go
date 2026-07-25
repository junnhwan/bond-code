package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/session"
)

// NavigateToEvent 从 session 历史沿事件树路径重建消息列表（navigate 回溯到历史节点）。
// 返回沿路径的消息，app 层可用它从该节点继续对话；实际 resume/TUI 集成后续。
func (a *App) NavigateToEvent(sessionID, eventID string) ([]llm.Message, error) {
	events, err := a.Sessions.Load(sessionID)
	if err != nil {
		return nil, err
	}
	msgs := session.MessagesAlongPath(events, eventID)
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, llm.Message{Role: llm.Role(m.Role), Content: m.Content})
	}
	return out, nil
}

// ForkAndResume realizes the session-tree "exploratory backtracking" flow:
// it forks the current session into a brand-new branch and rebuilds the
// conversation context along the path to eventID, so the agent continues from
// that historical node with a fresh line of thought.
//
// Semantics (see docs/planning/tui-navigate-design.md §4.2, §5):
//   - Never overwrite: a new SessionID is always produced; the original branch
//     file is left untouched so it can be backtracked to again.
//   - Fork copies the full event list via Import, preserving EventID/ParentID,
//     so the tree still holds in the new session and NavigateToEvent resolves
//     the path against the forked branch.
//   - After the call the App is switched onto the new branch: SessionID points
//     at it and history is replaced with the rebuilt messages, so the next
//     RunWithEvents continues from the fork point (and context governance runs
//     normally on the rebuilt history, invariant 3).
//
// Returns the new session id and the rebuilt messages for the caller (TUI) to
// refresh its view / seed the timeline.
func (a *App) ForkAndResume(sessionID, eventID string) (forkedSessionID string, messages []llm.Message, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Sessions == nil {
		return "", nil, fmt.Errorf("session store is not configured")
	}
	forkedSessionID = newSessionID()
	if err := a.Sessions.Fork(sessionID, forkedSessionID); err != nil {
		return "", nil, fmt.Errorf("fork session %q: %w", sessionID, err)
	}
	// Rebuild against the forked branch; Fork preserves the tree, so the result
	// is identical to NavigateToEvent on the original session for this eventID.
	messages, err = a.NavigateToEvent(forkedSessionID, eventID)
	if err != nil {
		return "", nil, fmt.Errorf("rebuild messages on forked session: %w", err)
	}
	staged, err := a.stageSessionState(forkedSessionID)
	if err != nil {
		return "", nil, err
	}
	defer staged.discard()
	// A fork is a distinct session, so it starts with its own empty todo scope.
	if a.TaskStore != nil {
		if err := a.TaskStore.SwitchSession(forkedSessionID); err != nil {
			return "", nil, fmt.Errorf("bind todo store to forked session: %w", err)
		}
	}
	// Switch onto the new branch: subsequent turns append here, and all
	// session-bound dependencies move to the branch in the same commit.
	a.commitSessionState(forkedSessionID, messages, "", staged)
	return forkedSessionID, messages, nil
}

// errSessionBusy is returned by SwitchSession when the agent is mid-turn and
// holding a.mu, so the per-session state cannot be swapped safely. Callers can
// detect it with errors.Is.
var errSessionBusy = errors.New("agent is busy; finish or cancel the current turn before switching sessions")

// SwitchSession hot-switches the running app onto targetSessionID: it reloads
// that session's history snapshot and rebuilds its per-session context stores
// (summary + tool-result + governor), so subsequent turns append to the target
// session and context governance runs against its window. This is the runtime
// behind TUI /resume <id> — a continue-the-target switch, NOT a fork (contrast
// ForkAndResume, which branches into a new session first).
//
// Invariants (docs/planning/tui-resume-design.md §5):
//   - busy: RunWithEvents holds a.mu for the whole turn, so TryLock fails and we
//     return errSessionBusy without touching any state (invariant 1).
//   - per-session store rebuild: SummaryStore/ToolResultStore/Manager are all
//     bound to a session id; switching without rebuilding would keep reading the
//     old session's summaries and spills (invariant 3, the easiest to miss).
//   - atomic rollback: load/rebuild failures leave SessionID/history/stores
//     untouched — everything is staged in locals and committed only on success
//     (invariant 6).
//
// Memory is project-level; todos are rebound to the target session.
func (a *App) SwitchSession(targetSessionID string) error {
	// RunWithEvents holds a.mu for the entire turn; a blocking Lock() here would
	// freeze the UI thread that issues /resume. TryLock fails fast instead.
	if !a.mu.TryLock() {
		return errSessionBusy
	}
	defer a.mu.Unlock()

	if a.Sessions == nil {
		return fmt.Errorf("session store is not configured")
	}
	if strings.TrimSpace(targetSessionID) == "" {
		return fmt.Errorf("target session id is empty")
	}
	if _, err := a.Sessions.Load(targetSessionID); err != nil {
		return fmt.Errorf("load target session %q: %w", targetSessionID, err)
	}

	// Reload the target's resumable history snapshot (same convention as
	// bootstrap --resume: each turn writes the full []llm.Message). Missing,
	// empty, or corrupt snapshots yield a fresh history; corrupt data is kept on
	// disk and exposed as a warning rather than blocking the switch.
	rebuilt, historyWarning, err := a.readHistorySnapshot(targetSessionID)
	if err != nil {
		return fmt.Errorf("read history snapshot for %q: %w", targetSessionID, err)
	}

	staged, err := a.stageSessionState(targetSessionID)
	if err != nil {
		return err
	}
	defer staged.discard()

	if a.TaskStore != nil {
		if err := a.TaskStore.SwitchSession(targetSessionID); err != nil {
			return fmt.Errorf("bind todo store to target session: %w", err)
		}
	}

	// Commit only after every fallible stage succeeds. Session identity, context,
	// rules, debug trace, and session-aware tools then move together.
	a.commitSessionState(targetSessionID, rebuilt, historyWarning, staged)
	return nil
}

// IsSessionBusy reports whether err is the busy sentinel from SwitchSession.
func IsSessionBusy(err error) bool { return errors.Is(err, errSessionBusy) }

// errModelBusy is returned by SwitchModel when the agent is mid-turn and holding
// a.mu, so the client cannot be swapped without racing the active Stream. Like
// errSessionBusy, detectable with errors.Is via IsModelBusy.
var errModelBusy = errors.New("agent is busy; finish or cancel the current turn before switching models")

// IsModelBusy reports whether err is the busy sentinel from SwitchModel.
func IsModelBusy(err error) bool { return errors.Is(err, errModelBusy) }

// SwitchModel swaps the active model without restarting: it updates the model
// name, rebuilds a fresh LLM client (raw + retry decorator) via buildModelClient,
// and pushes it onto the loop (SetClient) and a.LLM. The next turn uses the new
// model; /status reflects it immediately because StatusSnapshot reads
// Config.Model.Model.
//
// Mirrors SwitchSession's busy-guard: RunWithEvents holds a.mu for the whole
// turn, so a mid-turn SwitchModel would race the in-flight Stream — TryLock
// fails fast (errModelBusy) instead of deadlocking. The caller can retry or wait
// for the turn to end.
//
// Limitation: the subagent manager and orchestrator captured the original client
// at bootstrap, so already-spawned child agents keep the prior model. The main
// agent — what /model is for — switches cleanly; making future-spawned children
// follow requires re-pointing the manager and is left as a follow-up.
func (a *App) SwitchModel(model string) error {
	if !a.mu.TryLock() {
		return errModelBusy
	}
	defer a.mu.Unlock()
	if a.Config == nil {
		return fmt.Errorf("app config is not configured")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model name is empty")
	}
	a.Config.Model.Model = model
	client := buildModelClient(a.Config.Model)
	a.LLM = client
	if a.Agent != nil {
		a.Agent.SetClient(client)
	}
	return nil
}

// NewSession switches the running app onto a fresh empty session id. It clears
// conversation history and rebuilds per-session context stores so the next turn
// starts clean without leaving the TUI.
func (a *App) NewSession() (string, error) {
	if !a.mu.TryLock() {
		return "", errSessionBusy
	}
	defer a.mu.Unlock()

	if a.Sessions == nil {
		return "", fmt.Errorf("session store is not configured")
	}
	oldID := a.SessionID
	newID := newSessionID()

	staged, err := a.stageSessionState(newID)
	if err != nil {
		return "", err
	}
	defer staged.discard()

	if a.TaskStore != nil {
		if err := a.TaskStore.SwitchSession(newID); err != nil {
			return "", fmt.Errorf("bind todo store to new session: %w", err)
		}
	}

	a.commitSessionState(newID, nil, "", staged)
	// /clear and /new should not leave empty abandoned sessions behind. Drop the
	// previous id when it has no user messages, is unpinned, and has no custom
	// title (same keep-rule as the resume list).
	a.pruneEmptyAbandonedSession(oldID)
	return newID, nil
}

// pruneEmptyAbandonedSession best-effort deletes a session that would be hidden
// from resume lists. No-op when the id is empty, still active, missing, or has
// content/pin/title worth keeping.
func (a *App) pruneEmptyAbandonedSession(oldID string) {
	if a == nil || a.Sessions == nil || oldID == "" || oldID == a.SessionID {
		return
	}
	meta, err := a.Sessions.LoadMeta(oldID)
	if err != nil {
		return
	}
	_, count, _ := session.SessionPreview(a.Sessions, oldID)
	if session.KeepInResumeList(false, meta.Pinned, meta.Title, count) {
		return
	}
	// Only delete if an audit file actually exists; never-written sessions have
	// nothing on disk and List/Delete are no-ops either way.
	_ = a.Sessions.Delete(oldID)
}
