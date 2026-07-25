package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/command"
)

func TestPushSessionHistoryTruncatesForward(t *testing.T) {
	m := NewModel(Config{Status: Status{SessionID: "a"}})
	m = m.pushSessionHistory("b")
	m = m.pushSessionHistory("c")
	if len(m.sessionHistory) != 3 || m.sessionHistIdx != 2 {
		t.Fatalf("expected [a b c] idx 2, got %v idx %d", m.sessionHistory, m.sessionHistIdx)
	}
	// Walk back to b, then push d: forward entry c must be dropped.
	m.sessionHistIdx = 1
	m = m.pushSessionHistory("d")
	if len(m.sessionHistory) != 3 || m.sessionHistory[2] != "d" {
		t.Fatalf("expected forward truncated to [a b d], got %v", m.sessionHistory)
	}
	if m.sessionHistIdx != 2 {
		t.Fatalf("expected idx 2 after push, got %d", m.sessionHistIdx)
	}
}

func TestPushSessionHistoryRepeatIsNoOp(t *testing.T) {
	m := NewModel(Config{Status: Status{SessionID: "a"}})
	m = m.pushSessionHistory("a")
	if len(m.sessionHistory) != 1 {
		t.Fatalf("expected no duplicate push of current id, got %v", m.sessionHistory)
	}
}

func sessionNavModel(t *testing.T) Model {
	t.Helper()
	return NewModel(Config{
		Status: Status{SessionID: "a"},
		CommandEnv: command.Env{SwitchSession: func(id string) error {
			return nil
		}},
		ReloadSessionSeed: func(id string) []SeedMessage {
			// Return a long enough seed that the rebuilt timeline's maxScroll
			// exceeds the test's scroll offsets, so clampScroll does not erase
			// the restored position when switching back.
			var msgs []SeedMessage
			for i := 0; i < 8; i++ {
				msgs = append(msgs,
					SeedMessage{Role: "user", Content: fmt.Sprintf("%s prompt %d", id, i)},
					SeedMessage{Role: "assistant", Content: strings.Repeat("line\n", 5)},
				)
			}
			return msgs
		},
	})
}

func TestNavigateSessionBackForwardWalksStack(t *testing.T) {
	m := sessionNavModel(t)
	// Simulate visiting b then c (the /resume path pushes + the app switches).
	m = m.pushSessionHistory("b")
	m, _ = m.switchSessionFull("b")
	m = m.pushSessionHistory("c")
	m, _ = m.switchSessionFull("c")
	if m.cfg.Status.SessionID != "c" {
		t.Fatalf("precondition: expected current c, got %q", m.cfg.Status.SessionID)
	}

	// Back: c -> b -> a.
	m, _ = m.navigateSession(-1)
	if m.cfg.Status.SessionID != "b" || m.sessionHistIdx != 1 {
		t.Fatalf("back: expected b/idx1, got %q/idx%d", m.cfg.Status.SessionID, m.sessionHistIdx)
	}
	m, _ = m.navigateSession(-1)
	if m.cfg.Status.SessionID != "a" || m.sessionHistIdx != 0 {
		t.Fatalf("back: expected a/idx0, got %q/idx%d", m.cfg.Status.SessionID, m.sessionHistIdx)
	}
	// Back past the earliest clamps.
	before := m.sessionHistIdx
	m, _ = m.navigateSession(-1)
	if m.sessionHistIdx != before {
		t.Fatalf("expected clamp at %d, got %d", before, m.sessionHistIdx)
	}
	// Forward: a -> b.
	m, _ = m.navigateSession(+1)
	if m.cfg.Status.SessionID != "b" || m.sessionHistIdx != 1 {
		t.Fatalf("forward: expected b/idx1, got %q/idx%d", m.cfg.Status.SessionID, m.sessionHistIdx)
	}
}

func TestSwitchSessionStoresAndRestoresScroll(t *testing.T) {
	m := sessionNavModel(t)
	m = m.SetSize(100, 30)
	for i := 0; i < 6; i++ {
		m.timeline = m.timeline.StartUserTurn(fmt.Sprintf("user %d", i))
		m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", strings.Repeat("reply line\n", 6))
	}
	m = m.scrollBy(5)
	scrollA := m.scroll
	if scrollA == 0 {
		t.Fatal("precondition: expected non-zero scroll on session a")
	}

	// a -> b: a's scroll is stored, b starts fresh at 0.
	m, _ = m.switchSessionFull("b")
	if m.scroll != 0 {
		t.Fatalf("session b should start at scroll 0, got %d", m.scroll)
	}

	// b -> a: a's scroll is restored.
	m, _ = m.switchSessionFull("a")
	if m.scroll != scrollA {
		t.Fatalf("expected session a scroll restored to %d, got %d", scrollA, m.scroll)
	}
}
