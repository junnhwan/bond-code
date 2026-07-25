package app

import (
	"encoding/json"
	"fmt"

	"github.com/junnhwan/bond-code/internal/llm"
)

// saveHistorySnapshot persists the in-memory message history so a session can
// be resumed later (--resume). The full []llm.Message is rewritten each turn;
// for a personal project the size is modest and resume becomes crash-safe.
func (a *App) saveHistorySnapshot() error {
	if a.Sessions == nil || a.SessionID == "" {
		return nil
	}
	data, err := json.Marshal(a.history)
	if err != nil {
		return err
	}
	return a.Sessions.WriteHistory(a.SessionID, data)
}

// loadHistorySnapshot restores the message history for a session id. Missing
// snapshots are not an error (fresh session). Corrupt snapshots are treated as
// recoverable: keep the original file for inspection, warn, and continue with
// empty history so a bad cache cannot block startup or /resume.
func (a *App) loadHistorySnapshot(id string) error {
	msgs, warning, err := a.readHistorySnapshot(id)
	if err != nil {
		return err
	}
	a.history = msgs
	a.historyWarning = warning
	return nil
}

func (a *App) readHistorySnapshot(id string) ([]llm.Message, string, error) {
	if a.Sessions == nil || id == "" {
		return nil, "", nil
	}
	data, err := a.Sessions.ReadHistory(id)
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", nil
	}
	var msgs []llm.Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		warning := fmt.Sprintf("history snapshot warning: session %s has a corrupt history snapshot; continuing with empty history (%v)", id, err)
		return nil, warning, nil
	}
	return msgs, "", nil
}

// History returns a copy of the accumulated conversation history. It is used
// to seed the TUI timeline when a session is resumed.
func (a *App) History() []llm.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]llm.Message(nil), a.history...)
}
