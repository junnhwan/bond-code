package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/session"
)

// latestSessionID returns the most recently modified session id in dir, so
// `session trace` with no argument reviews the session the user just ran.
func latestSessionID(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var best string
	var bestTime time.Time
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if best == "" || info.ModTime().After(bestTime) {
			best = strings.TrimSuffix(entry.Name(), ".jsonl")
			bestTime = info.ModTime()
		}
	}
	if best == "" {
		return "", fmt.Errorf("no sessions found in %s", dir)
	}
	return best, nil
}

// runSessionTrace prints a diagnostic summary of a session: one line per agent
// turn (steps, tools, finish state), an anomalies section (loop guards,
// rejections, errors, unfinished turns), and global tool stats. Streaming noise
// (model_chunk / context_* / reasoning_chunk) is collapsed so the structure is
// scannable — this is the "what actually happened" view that the raw JSONL
// buries under thousands of chunk events.
//
// When debugRecords is non-empty (loaded from <id>.debug.jsonl via --debug), each
// turn is augmented with the model-decision layer for that turn: prompt-cache hit
// rate, token usage, and the per-tool risk/decision/timing. The debug records are
// main-agent-only (child loops don't log to the debug file), so each turn maps 1:1
// to one debug segment (a RunMessagesWithEvents call whose records start at step 0).
func runSessionTrace(out io.Writer, sessionID string, events []session.Event, debugRecords []observe.Record) {
	debugSegments := segmentDebugRecords(debugRecords)
	type turn struct {
		index     int
		steps     int
		tools     map[string]int
		state     string // done | error | no-finish | ""
		detail    string
		anomalies []string
		lastType  string
	}
	var turns []*turn
	var cur *turn
	closeTurn := func(state, detail string) {
		if cur == nil {
			return
		}
		cur.state = state
		if detail != "" {
			cur.detail = detail
		}
	}
	globalTools := map[string]int{}

	for _, ev := range events {
		ae := ev.AgentEvent
		if ae == nil {
			continue
		}
		switch ae.Type {
		case "agent_started":
			if cur != nil && cur.state == "" {
				cur.state = "no-finish"
				cur.detail = "last event: " + cur.lastType
			}
			cur = &turn{index: len(turns) + 1, tools: map[string]int{}}
			turns = append(turns, cur)
		case "agent_finished":
			closeTurn("done", preview(ae.Message, 60))
		case "agent_error":
			closeTurn("error", preview(ae.Error, 80))
			if cur != nil {
				cur.anomalies = append(cur.anomalies, "agent_error: "+preview(ae.Error, 80))
			}
		case "tool_requested":
			if cur != nil {
				cur.steps++
				cur.tools[ae.ToolName]++
				globalTools[ae.ToolName]++
			}
		case "loop_guard":
			if cur != nil {
				cur.anomalies = append(cur.anomalies, fmt.Sprintf("loop_guard: %s — %s", ae.ToolName, preview(ae.Message, 70)))
			}
		case "tool_rejected":
			if cur != nil {
				msg := ae.Message
				if ae.Error != "" {
					msg = ae.Error
				}
				cur.anomalies = append(cur.anomalies, fmt.Sprintf("rejected: %s — %s", ae.ToolName, preview(msg, 70)))
			}
		}
		if cur != nil {
			cur.lastType = ae.Type
		}
	}
	if cur != nil && cur.state == "" {
		cur.state = "no-finish"
		cur.detail = "last event: " + cur.lastType
	}

	fmt.Fprintf(out, "=== %s · %d turns ===\n", sessionID, len(turns))
	debugAligned := len(debugSegments) == len(turns) && len(debugSegments) > 0
	for _, t := range turns {
		fmt.Fprintf(out, "turn %-3d %-9s %d steps   %s\n", t.index, t.state, t.steps, toolSummary(t.tools))
		if t.detail != "" && t.state != "done" {
			fmt.Fprintf(out, "          %s\n", t.detail)
		}
		if debugAligned {
			fmt.Fprint(out, formatTurnDebug(debugSegments[t.index-1]))
		}
	}

	var anomalies []string
	for _, t := range turns {
		for _, a := range t.anomalies {
			anomalies = append(anomalies, fmt.Sprintf("[turn %d] %s", t.index, a))
		}
	}
	if len(anomalies) > 0 {
		fmt.Fprintln(out, "\nanomalies:")
		for _, a := range anomalies {
			fmt.Fprintf(out, "  %s\n", a)
		}
	}

	if len(globalTools) > 0 {
		fmt.Fprintln(out, "\ntool stats:", toolStats(globalTools))
	}

	// When the debug segment count doesn't line up 1:1 with audit turns (e.g. the
	// session was Ctrl+C'd mid-turn, or a future change logs child-loop records),
	// fall back to printing the model-decision layer as its own chronologically
	// ordered section instead of risking a misaligned inline augmentation.
	if len(debugSegments) > 0 && !debugAligned {
		fmt.Fprintln(out, "\nmodel decisions (--debug):")
		for i, seg := range debugSegments {
			fmt.Fprintf(out, "  [call %d]\n", i+1)
			fmt.Fprint(out, formatTurnDebug(seg))
		}
	}
}

// segmentDebugRecords splits a flat debug-record stream into one segment per
// main-agent RunMessagesWithEvents call. Each such call logs an llm_req at step 0
// first (steps then rise 0..MaxSteps-1, optionally followed by a finalize call at
// step MaxSteps), so a new segment begins at every llm_req whose step is 0. Tools
// and decide records attach to the segment they appear in. Returns nil for empty
// input.
func segmentDebugRecords(records []observe.Record) [][]observe.Record {
	var segments [][]observe.Record
	var cur []observe.Record
	for _, r := range records {
		if r.T == "llm_req" && r.Step == 0 && cur != nil {
			segments = append(segments, cur)
			cur = nil
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		segments = append(segments, cur)
	}
	return segments
}

// formatTurnDebug renders one debug segment's model-decision summary as indented
// lines: cache hit rate + token totals across the turn's model calls, then each
// tool's risk/decision/timing, then any safety/context decide details. Returns ""
// if the segment has no decision-worthy content.
func formatTurnDebug(segment []observe.Record) string {
	var (
		calls                      int
		in, out, cacheRead, cacheC int
		tools                      []observe.Record
		decides                    []observe.Record
	)
	for _, r := range segment {
		switch r.T {
		case "llm_resp":
			calls++
			if r.Usage != nil {
				in += r.Usage.In
				out += r.Usage.Out
				cacheRead += r.Usage.CacheRead
				cacheC += r.Usage.CacheCreate
			}
		case "tool":
			tools = append(tools, r)
		case "decide":
			decides = append(decides, r)
		}
	}
	if calls == 0 && len(tools) == 0 && len(decides) == 0 {
		return ""
	}
	var b strings.Builder
	if calls > 0 {
		cacheNote := "no cache"
		if in > 0 {
			pct := 100 * cacheRead / in
			cacheNote = fmt.Sprintf("%s/%s in (%d%%)", fmtTokens(cacheRead), fmtTokens(in), pct)
		}
		fmt.Fprintf(&b, "          debug: %d model call(s) · in %s out %s · cache %s · create %s\n",
			calls, fmtTokens(in), fmtTokens(out), cacheNote, fmtTokens(cacheC))
	}
	if len(tools) > 0 {
		parts := make([]string, 0, len(tools))
		for _, tk := range tools {
			approved := "denied"
			if tk.Approved {
				approved = "ok"
			}
			parts = append(parts, fmt.Sprintf("%s(%s:%s %s)", tk.Name, firstNonEmptyField(tk.Risk, "?"), tk.Decision, approved))
		}
		fmt.Fprintf(&b, "          tools: %s\n", strings.Join(parts, "  "))
	}
	for _, d := range decides {
		fmt.Fprintf(&b, "          decide[%s]: %s\n", d.Kind, preview(d.Detail, 90))
	}
	return b.String()
}

func firstNonEmptyField(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// fmtTokens renders a token count compactly (e.g. 12000 -> "12.0k", 340 -> "340").
func fmtTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// loadDebugRecords reads <dir>/<id>.debug.jsonl (one observe.Record per line) if it
// exists. A missing file is not an error: it just means the session ran without
// --debug, so the trace falls back to the audit-only view.
func loadDebugRecords(dir, sessionID string) ([]observe.Record, error) {
	path := filepath.Join(dir, sessionID+".debug.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var records []observe.Record
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var r observe.Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			// Skip unparseable lines rather than aborting the whole trace — a
			// partially flushed last line after SIGINT shouldn't hide the rest.
			continue
		}
		records = append(records, r)
	}
	return records, scanner.Err()
}

func toolSummary(tools map[string]int) string {
	if len(tools) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(tools))
	for _, k := range sortedKeys(tools) {
		parts = append(parts, fmt.Sprintf("%s×%d", k, tools[k]))
	}
	return strings.Join(parts, "  ")
}

func toolStats(tools map[string]int) string {
	parts := make([]string, 0, len(tools))
	for _, k := range sortedKeys(tools) {
		parts = append(parts, fmt.Sprintf("%s %d", k, tools[k]))
	}
	return strings.Join(parts, "  ")
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func preview(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
