package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/ask"
)

func TestInitSetsTerminalWindowTitle(t *testing.T) {
	model := NewModel(Config{Status: Status{ProjectRoot: `D:\dev\my_proj\go\bond-code`}})

	cmd := model.Init()
	if cmd == nil {
		t.Fatal("expected Init to return commands")
	}

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected Init to return a command batch, got %T", msg)
	}

	if !batchContainsImmediateMessage(batch, "BondCode · bond-code") {
		t.Fatalf("expected Init command batch to set terminal title, got %d commands", len(batch))
	}
}

func TestTerminalWindowTitleFallsBackWithoutProjectRoot(t *testing.T) {
	model := NewModel(Config{})
	if got := model.terminalTitle(); got != "BondCode" {
		t.Fatalf("expected fallback title BondCode, got %q", got)
	}
}

func TestTerminalTitleBusyIncludesActivity(t *testing.T) {
	m := NewModel(Config{Status: Status{ProjectRoot: `D:\x\bond-code`}})
	m.agent.Busy = true
	m.agent.LiveDetail = "Read file"
	got := m.terminalTitle()
	if !strings.Contains(got, "BondCode") {
		t.Fatalf("busy title should include brand, got %q", got)
	}
	if !strings.Contains(got, "Read") {
		t.Fatalf("busy title should include activity, got %q", got)
	}
	if !strings.Contains(got, "bond-code") {
		t.Fatalf("busy title should include project, got %q", got)
	}
}

func TestTerminalTitleActionRequired(t *testing.T) {
	m := NewModel(Config{Status: Status{ProjectRoot: `D:\x\bond-code`}})
	m.agent.Pending = &agent.Event{Type: agent.EventToolRequested, ToolName: "write_file", Risk: "medium"}
	got := m.terminalTitle()
	if !strings.Contains(got, "Action Required") {
		t.Fatalf("expected Action Required, got %q", got)
	}
}

func TestTerminalTitleActionRequiredWithQuestion(t *testing.T) {
	m := NewModel(Config{Status: Status{ProjectRoot: `D:\x\bond-code`}})
	m.question = &ask.Question{Prompt: "ok?"}
	got := m.terminalTitle()
	if !strings.Contains(got, "Action Required") {
		t.Fatalf("expected Action Required for question, got %q", got)
	}
}

func TestComposeTerminalTitleSanitizesControls(t *testing.T) {
	got := composeTerminalTitle("proj\x1b", "act\nivity", true, false, 0)
	if strings.ContainsAny(got, "\x1b\n") {
		t.Fatalf("title must strip controls, got %q", got)
	}
}

func TestMaybeSetTerminalTitleDedups(t *testing.T) {
	m := NewModel(Config{Status: Status{ProjectRoot: `D:\x\bond-code`}})
	m.lastTerminalTitle = m.terminalTitle()
	m, cmd := m.maybeSetTerminalTitle()
	if cmd != nil {
		t.Fatal("expected nil cmd when title unchanged")
	}
	m.agent.Busy = true
	m.agent.LiveDetail = "working"
	m, cmd = m.maybeSetTerminalTitle()
	if cmd == nil {
		t.Fatal("expected title cmd when busy state changes title")
	}
}

func TestBusyTitleGlyphDwellsForDivisorFrames(t *testing.T) {
	// Pure composition path used by terminalTitle(): glyph must not change
	// within a titleSpinnerDivisor window of raw tick values.
	base := composeTerminalTitle("bond-code", "Read file", true, false, 0)
	for i := 1; i < titleSpinnerDivisor; i++ {
		if got := composeTerminalTitle("bond-code", "Read file", true, false, i); got != base {
			t.Fatalf("frame %d title %q != frame 0 %q (glyph must dwell)", i, got, base)
		}
	}
	if got := composeTerminalTitle("bond-code", "Read file", true, false, titleSpinnerDivisor); got == base {
		t.Fatal("expected glyph to advance after titleSpinnerDivisor frames")
	}
}

// TestBusySpinnerTicksDoNotThrashTitle drives the real spinner.TickMsg handler
// and asserts lastTerminalTitle (the SetWindowTitle dedupe cache) advances at
// most ~once per titleSpinnerDivisor ticks while busy — not every frame.
func TestBusySpinnerTicksDoNotThrashTitle(t *testing.T) {
	m := NewModel(Config{Status: Status{ProjectRoot: `D:\x\bond-code`}})
	m.agent.Busy = true
	m.agent.LiveDetail = "Read file"
	m.lastTerminalTitle = m.terminalTitle()
	m.titleSpinnerFrame = 0

	emissions := 0
	const ticks = 24
	for i := 0; i < ticks; i++ {
		before := m.lastTerminalTitle
		next, _ := m.Update(spinner.TickMsg{Time: time.Now(), ID: m.spinner.ID()})
		m = next.(Model)
		if m.lastTerminalTitle != before {
			emissions++
		}
	}
	maxWant := ticks/titleSpinnerDivisor + 1
	if emissions > maxWant {
		t.Fatalf("busy title thrash: %d title updates in %d ticks (max %d)", emissions, ticks, maxWant)
	}
	if emissions < 1 {
		t.Fatal("expected at least one throttled title update over busy ticks")
	}
	// Within a single dwell window, composed title must stay stable.
	m.titleSpinnerFrame = titleSpinnerDivisor * 3
	stable := m.terminalTitle()
	for i := 0; i < titleSpinnerDivisor-1; i++ {
		m.titleSpinnerFrame++
		if got := m.terminalTitle(); got != stable {
			t.Fatalf("title changed mid-dwell at offset %d: %q vs %q", i+1, got, stable)
		}
	}
}

func TestIdleTerminalTitleStable(t *testing.T) {
	m := NewModel(Config{Status: Status{ProjectRoot: `D:\x\bond-code`}})
	m.agent.Busy = true
	idle := m.idleTerminalTitle()
	if strings.Contains(idle, "Action") || strings.Contains(idle, "Read") {
		t.Fatalf("idle title must not include busy/action, got %q", idle)
	}
	if idle != "BondCode · bond-code" {
		t.Fatalf("got %q", idle)
	}
}

func batchContainsImmediateMessage(batch tea.BatchMsg, want string) bool {
	for _, cmd := range batch {
		if cmd == nil {
			continue
		}
		ch := make(chan tea.Msg, 1)
		go func(cmd tea.Cmd) {
			ch <- cmd()
		}(cmd)
		select {
		case msg := <-ch:
			if strings.Contains(fmt.Sprint(msg), want) {
				return true
			}
		case <-time.After(20 * time.Millisecond):
		}
	}
	return false
}
