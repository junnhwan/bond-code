package builtin

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/junnhwan/bond-code/internal/command"
	"github.com/junnhwan/bond-code/internal/session"
)

// SessionsCommand preserves /sessions as a hidden compatibility alias for
// /resume. It intentionally delegates the runner so both routes retain the
// same list, switch, and TUI-overlay semantics.
func SessionsCommand() command.Command {
	resume := ResumeCommand()
	return command.Command{
		Name:        "sessions",
		Description: resume.Description,
		RemoteSafe:  resume.RemoteSafe,
		Run:         resume.Run,
	}
}

// SessionCommand shows details for the current session. It remains distinct
// from /resume and /sessions, which list sessions and can open the picker.
func SessionCommand() command.Command {
	return command.Command{
		Name:        "session",
		Description: "Show current session details",
		RemoteSafe:  true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			current := currentSessionStats(env)
			panel := &command.Panel{
				Title: "session",
				Sections: []command.PanelSection{{
					Label: "CURRENT SESSION",
					Rows: []command.PanelRow{
						{Key: "id", Value: orDash(env.SessionID)},
						{Key: "messages", Value: fmt.Sprintf("%d (%d user, %d assistant)", current.totalMessages(), current.userMsgs, current.assistantMsgs)},
						{Key: "tool calls", Value: fmt.Sprintf("%d", current.toolCalls)},
					},
				}},
			}
			return command.Result{Output: renderPanelText(panel), Panel: panel}, nil
		},
	}
}

// listSessionsOutput renders the session list newest-first (current session
// marked with ▶). Each line is "<id>  <relative age>  <first-prompt preview>
// (<user msg count>)"; resumeHint is appended as a footer so /sessions and
// /resume can point at different resume mechanisms (CLI flag vs in-app command).
// Shared by SessionsCommand and ResumeCommand to keep the two lists identical.
func listSessionsOutput(env command.Env, resumeHint string) (string, error) {
	if env.Sessions == nil {
		return "session storage is not configured", nil
	}
	ids, err := env.Sessions.List()
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "no previous sessions", nil
	}
	// Session ids embed a UTC timestamp, so ascending sort is chronological;
	// iterate newest-first.
	sort.Strings(ids)
	var b strings.Builder
	listed := 0
	for i := len(ids) - 1; i >= 0; i-- {
		id := ids[i]
		preview, count, last := sessionMeta(env.Sessions, id)
		meta, _ := env.Sessions.LoadMeta(id)
		active := id == env.SessionID
		if !session.KeepInResumeList(active, meta.Pinned, meta.Title, count) {
			continue
		}
		marker := "  "
		if active {
			marker = "▶ "
		}
		line := marker + id
		if age := humanizeAge(last); age != "" {
			line += "  " + age
		}
		if preview != "" {
			line += "  " + preview
		}
		if count > 0 {
			line += fmt.Sprintf("  (%d msgs)", count)
		}
		fmt.Fprintln(&b, line)
		listed++
	}
	if listed == 0 {
		out := "no previous sessions"
		if resumeHint != "" {
			out += "\n\n" + resumeHint
		}
		return out, nil
	}
	out := strings.TrimSpace(b.String())
	if resumeHint != "" {
		out += "\n\n" + resumeHint
	}
	return out, nil
}

// sessionMeta returns the first user message (truncated), the total user message
// count, and the last event's CreatedAt (for the relative "age") of a session —
// everything the list needs to identify a session at a glance.
// sessionMeta is a thin wrapper around session.SessionPreview so the command
// keeps its local signature while sharing derivation with the TUI manager.
func sessionMeta(store *session.JSONLStore, id string) (preview string, count int, lastActivity time.Time) {
	return session.SessionPreview(store, id)
}

// humanizeAge renders a timestamp as a short relative-age label matching the
// English style used elsewhere in the list: "just now" / "Nm ago" / "Nh ago" /
// "Nd ago", then an absolute MM-DD past a week. Empty for a zero timestamp.
func humanizeAge(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	d := time.Since(at)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return at.Format("01-02")
	}
}
