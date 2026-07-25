package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/junnhwan/bond-code/internal/fsx"
)

// ExportTextSummary records what ExportText wrote so the caller (the /export
// command) can report the path and message/tool counts back to the user.
type ExportTextSummary struct {
	Path              string
	UserMessages      int
	AssistantMessages int
	ToolCalls         int
}

func (s ExportTextSummary) TotalMessages() int {
	return s.UserMessages + s.AssistantMessages
}

// ExportText renders a session's event log as human-readable text and writes it
// to targetPath. Unlike Export (a verbatim JSONL copy meant for Import/Fork),
// this produces a readable transcript: each user/assistant message and tool call
// becomes a labelled section so the file can be read or shared without the app.
func (s *JSONLStore) ExportText(sessionID, targetPath string) (ExportTextSummary, error) {
	var summary ExportTextSummary
	if err := validateSessionID(sessionID); err != nil {
		return summary, err
	}
	events, err := s.Load(sessionID)
	if err != nil {
		return summary, err
	}
	if dir := filepath.Dir(targetPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return summary, err
		}
	}
	body, counts := renderTranscript(events)
	header := renderExportHeader(sessionID, counts)
	if err := fsx.WriteFileAtomic(targetPath, []byte(header+body), 0o600); err != nil {
		return summary, err
	}
	counts.Path = targetPath
	return counts, nil
}

func renderExportHeader(sessionID string, counts ExportTextSummary) string {
	var b strings.Builder
	b.WriteString("# BondCode Session Export\n\n")
	fmt.Fprintf(&b, "session: %s\n", sessionID)
	fmt.Fprintf(&b, "exported: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "messages: %d (%d user, %d assistant) · %d tool calls\n",
		counts.TotalMessages(), counts.UserMessages, counts.AssistantMessages, counts.ToolCalls)
	b.WriteString("\n---\n")
	return b.String()
}

func renderTranscript(events []Event) (string, ExportTextSummary) {
	var summary ExportTextSummary
	var b strings.Builder
	for _, e := range events {
		// Only render the readable conversation: message events (user input,
		// the full assistant reply recorded once per turn) and tool_result
		// events. agent_event entries are the streaming progress stream
		// (per-chunk model/reasoning updates, tool requests); rendering each
		// one turns the transcript into a choppy stream of fragments.
		switch {
		case e.Message != nil:
			renderMessageSection(&b, e.Message)
			switch e.Message.Role {
			case RoleUser:
				summary.UserMessages++
			case RoleAssistant:
				summary.AssistantMessages++
			}
		case e.ToolCall != nil:
			renderToolSection(&b, e.ToolCall)
			summary.ToolCalls++
		}
	}
	return b.String(), summary
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return "  · " + t.Local().Format("2006-01-02 15:04:05")
}

func renderMessageSection(b *strings.Builder, m *Message) {
	fmt.Fprintf(b, "\n## %s%s\n\n", string(m.Role), stamp(m.CreatedAt))
	if c := strings.TrimSpace(m.Content); c != "" {
		b.WriteString(c)
		b.WriteString("\n")
	}
}

func renderToolSection(b *strings.Builder, t *ToolCall) {
	status := "done"
	if strings.TrimSpace(t.Error) != "" {
		status = "failed"
	}
	fmt.Fprintf(b, "\n## tool · %s  [%s]%s\n", t.Name, status, stamp(t.CreatedAt))
	writeField(b, "input", t.Input)
	writeField(b, "output", t.Output)
	writeField(b, "error", t.Error)
}

func writeField(b *strings.Builder, key, value string) {
	if v := strings.TrimSpace(value); v != "" {
		fmt.Fprintf(b, "%s:\n%s\n", key, v)
	}
}
