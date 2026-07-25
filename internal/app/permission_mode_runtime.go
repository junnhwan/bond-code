package app

import (
	"fmt"
	"time"

	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/session"
)

func (a *App) PermissionMode() safety.PermissionMode {
	if a == nil {
		return safety.ModeDefault
	}
	return a.Policy.EffectiveMode()
}

// SetPermissionMode records the transition before publishing it to all shared
// policy copies. A failed audit therefore leaves runtime behavior unchanged.
func (a *App) SetPermissionMode(raw string) error {
	if a == nil || a.Policy.RuntimeModeSource == nil {
		return fmt.Errorf("runtime permission mode is not configured")
	}
	next, err := safety.ParsePermissionMode(raw)
	if err != nil {
		return err
	}
	if next == safety.ModeBypass && !a.Policy.EffectiveBypassEnabled() {
		return fmt.Errorf("bypass mode requires trusted enablement")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	previous := a.Policy.EffectiveMode()
	if previous == next {
		return nil
	}
	if a.Sessions == nil || a.SessionID == "" {
		return fmt.Errorf("session audit store is not configured")
	}
	now := time.Now().UTC()
	event := session.Event{SessionID: a.SessionID, Type: "permission_mode_changed", CreatedAt: now, AgentEvent: &session.AgentEvent{Type: "permission_mode_changed", Message: "runtime permission mode changed", Input: string(previous), Output: string(next), CreatedAt: now}}
	if err := a.Sessions.Append(event); err != nil {
		return fmt.Errorf("audit permission mode transition: %w", err)
	}
	if err := a.Policy.RuntimeModeSource.Set(next); err != nil {
		return err
	}
	a.Policy.Mode = next
	return nil
}
