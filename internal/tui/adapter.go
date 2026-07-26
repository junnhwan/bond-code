package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/mattn/go-runewidth"
)

func (m Model) appendLiveChunk(kind BlockKind, chunk string, _ time.Time) Model {
	if chunk == "" {
		return m
	}

	state := liveStreamState{kind: kind}
	oldVisibleLen := 0
	if current := m.agent.LiveStream; current != nil && current.kind == kind {
		state = *current
		oldVisibleLen = current.visibleLen
	} else {
		m.agent.LiveGeneration++
		state.generation = m.agent.LiveGeneration
	}
	if !state.buffer.matches(state.body) {
		state.buffer = newStreamBodyBuffer(state.body)
	}

	state.body = state.buffer.append(chunk)
	// Line-gated reveal (not typewriter): only complete lines become visible.
	// Recompute from the full body so multi-chunk lines still flush correctly.
	state.visibleLen = liveVisibleLen(state.body)
	m.agent.LiveStream = &state
	switch kind {
	case BlockAssistant:
		m.agent.LiveDetail = "responding"
	case BlockReasoning:
		m.agent.LiveDetail = "thinking"
	}
	if state.visibleLen > oldVisibleLen {
		m = m.markNewOutputBelow()
	}
	return m
}

// liveVisibleLen returns the byte length through the last complete newline.
// Unfinished tails stay hidden until \n — terminal-friendly line streaming.
func liveVisibleLen(body string) int {
	if body == "" {
		return 0
	}
	if i := strings.LastIndexByte(body, '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

func (m Model) ApplyAgentEvent(event agent.Event) Model {
	// Direct adapter callers get the same terminal idempotence as Update's
	// run-generation gate. EventAgentStarted explicitly opens a new generation.
	if m.agent.TerminalHandled && event.Type != agent.EventAgentStarted {
		return m
	}
	hadAssistantLive := m.agent.LiveStream != nil &&
		m.agent.LiveStream.kind == BlockAssistant && m.agent.LiveStream.body != ""
	if live := m.agent.LiveStream; live != nil && eventEndsLiveStream(event, live.kind) {
		m = m.commitLiveStream()
	}

	switch classifyAgentEvent(event.Type) {
	case agentEventFamilyStream:
		return m.applyStreamEvent(event)
	case agentEventFamilyContext:
		return m.applyContextEvent(event)
	case agentEventFamilyTool:
		return m.applyToolEvent(event)
	case agentEventFamilySubagent:
		return m.applySubagentEvent(event)
	case agentEventFamilyTerminal:
		return m.applyTerminalEvent(event, hadAssistantLive)
	default:
		return m
	}
}

func (m Model) commitLiveStream() Model {
	live := m.agent.LiveStream
	if live == nil {
		return m
	}

	kind, body := live.kind, live.body
	m.agent.LiveStream = nil
	m.agent.LiveDetail = ""
	if body == "" {
		return m
	}

	switch kind {
	case BlockAssistant:
		m.timeline = m.timeline.AppendBlock(BlockAssistant, "agent", body)
	case BlockReasoning:
		// One thinking block per turn: later segments merge in; tools stay
		// as separate rows and are never dropped by the merge.
		m.timeline = m.timeline.MergeTurnReasoning(body)
	default:
		return m
	}
	return m.markNewOutputBelow()
}

// eventEndsLiveStream classifies only events that atomically separate live text
// from committed history. Context/usage and child-only trace events are explicit
// non-boundaries; unknown events preserve the stream.
func eventEndsLiveStream(event agent.Event, liveKind BlockKind) bool {
	switch event.Type {
	case agent.EventModelChunk:
		return event.Message != "" && liveKind != BlockAssistant
	case agent.EventReasoningChunk:
		return event.Message != "" && liveKind != BlockReasoning
	case agent.EventAgentStarted,
		agent.EventCompactionStarted, agent.EventCompactionFinished,
		agent.EventToolRequested, agent.EventToolConfirmationRequested,
		agent.EventToolApproved, agent.EventToolRejected, agent.EventToolResult,
		agent.EventLoopGuard, agent.EventTextDegeneration,
		agent.EventSubagentStarted, agent.EventSubagentProgress,
		agent.EventSubagentFinished, agent.EventSubagentFailed, agent.EventSubagentCancelled,
		agent.EventAgentFinished, agent.EventAgentError:
		return true
	case agent.EventContextUpdated, agent.EventContextMeasured, agent.EventSubagentToolCall:
		return false
	default:
		return false
	}
}

// cacheContextTokens stores the context-window usage carried by an event so the
// header can render a live ctx %. It is shared by EventContextUpdated (per-turn)
// and EventCompactionFinished (after /compact).
func (m Model) cacheContextTokens(event agent.Event) Model {
	if event.ContextTokens > 0 {
		m.agent.ContextTokens = event.ContextTokens
	}
	if event.ContextMaxTokens > 0 {
		m.agent.ContextMaxTokens = event.ContextMaxTokens
	}
	return m
}

// cacheMeasuredTokens stores the real input-token count the model reported, so
// the header's ctx % uses the same number /status shows (both prefer measured
// tokens and fall back to the governor estimate before the first reply).
func (m Model) cacheMeasuredTokens(event agent.Event) Model {
	if event.MeasuredInputTokens > 0 {
		m.agent.MeasuredTokens = event.MeasuredInputTokens
	}
	return m
}

func isTodoMutation(name string) bool {
	switch strings.TrimSpace(name) {
	case "todo_write", "todo_read":
		return true
	default:
		return false
	}
}

// refreshStatus pulls fresh context-window metadata via the optional RefreshStatus
// callback so the live's composition reflects the live window after compaction
// and at turn end, instead of the startup snapshot. Only the context fields are
// merged; other live state (tasks, subagents, ...) is left to its own event
// paths. No-op when the callback is unset (headless / test models).
func (m Model) refreshStatus() Model {
	if m.cfg.RefreshStatus == nil {
		return m
	}
	fresh := m.cfg.RefreshStatus()
	m.live.Breakdown = fresh.ContextBreakdown
	m.live.ContextSummary = fresh.ContextSummary
	m.live.Tasks = fresh.Tasks
	m.live.PlanningSummary = fresh.PlanningSummary
	m.live.Teams = fresh.Teams
	// Live usage (Phase 3.3): the footer shows cumulative token usage so the
	// user can watch cost accumulate without running /cost each turn.
	m.usage = fresh.Usage
	return m
}

// updateSubagentTrace folds a subagent lifecycle/tool event into the child's
// trace (keyed by taskID, carried in event.ToolCallID), creating the trace on
// first contact. Tool calls become Blocks so the agent window can render them
// with the existing tool activity renderer.
func (m Model) updateSubagentTrace(event agent.Event) Model {
	taskID := strings.TrimSpace(event.ToolCallID)
	if taskID == "" {
		return m
	}
	trace, ok := m.subagentTraces[taskID]
	if !ok {
		trace = &AgentTrace{TaskID: taskID}
		m = m.setSubagentTrace(taskID, trace)
	}
	if event.Generation != 0 && trace.Generation != 0 && event.Generation < trace.Generation {
		return m
	}
	if event.Type == agent.EventSubagentStarted && event.Generation > trace.Generation {
		trace.commitLiveStream()
		trace.Generation = event.Generation
	}
	if trace.Generation == 0 && event.Generation != 0 {
		trace.Generation = event.Generation
	}
	if event.Generation != 0 && trace.Generation != 0 && event.Generation != trace.Generation {
		return m
	}

	switch event.Type {
	case agent.EventSubagentStarted:
		trace.AgentType = event.ToolName
		trace.Title = firstNonEmpty(event.Message, event.ToolName, "subagent")
		trace.Status = "running"
	case agent.EventSubagentModelChunk:
		trace.appendLiveChunk(BlockAssistant, event.Message)
	case agent.EventSubagentReasoningChunk:
		trace.appendLiveChunk(BlockReasoning, event.Message)
	case agent.EventSubagentToolCall:
		trace.commitLiveStream()
		trace.upsertToolBlock(event)
	case agent.EventSubagentProgress:
		trace.commitLiveStream()
		if body := strings.TrimSpace(event.Message); body != "" {
			trace.Blocks = append(trace.Blocks, Block{ID: trace.nextBlockID(), Kind: BlockAssistant, Title: "agent", Body: body})
		}
	case agent.EventSubagentFinished:
		hadStream := trace.LiveStream != nil || trace.hasAssistantBlock()
		trace.commitLiveStream()
		trace.Status = "completed"
		if !hadStream {
			trace.FinalAnswer = event.Output
		} else {
			trace.FinalAnswer = ""
		}
	case agent.EventSubagentFailed:
		trace.commitLiveStream()
		trace.Status = "failed"
		trace.FinalAnswer = firstNonEmpty(event.Output, event.Error)
	case agent.EventSubagentCancelled:
		trace.commitLiveStream()
		trace.Status = "cancelled"
		trace.FinalAnswer = firstNonEmpty(event.Output, event.Message)
	}
	if m.focus == FocusAgentWindow && m.focusedTaskID == taskID {
		trace.Unread = false
		if m.scrollPaused && m.scroll > 0 {
			m = m.markNewOutputBelow()
		}
	} else {
		trace.Unread = true
	}
	return m
}

func (trace *AgentTrace) hasAssistantBlock() bool {
	for _, block := range trace.Blocks {
		if block.Kind == BlockAssistant {
			return true
		}
	}
	return false
}

func (trace *AgentTrace) appendLiveChunk(kind BlockKind, chunk string) {
	if chunk == "" {
		return
	}
	if trace.LiveStream != nil && trace.LiveStream.kind != kind {
		trace.commitLiveStream()
	}
	state := liveStreamState{kind: kind, generation: trace.Generation}
	if trace.LiveStream != nil {
		state = *trace.LiveStream
	}
	if !state.buffer.matches(state.body) {
		state.buffer = newStreamBodyBuffer(state.body)
	}
	state.body = state.buffer.append(chunk)
	state.visibleLen = liveVisibleLen(state.body)
	trace.LiveStream = &state
}

func (trace *AgentTrace) commitLiveStream() {
	live := trace.LiveStream
	if live == nil {
		return
	}
	trace.LiveStream = nil
	if live.body == "" {
		return
	}
	if live.kind == BlockReasoning {
		// One thinking block per child trace; never drop tool rows while merging.
		trace.Blocks = consolidateReasoningBlocks(trace.TaskID, trace.Blocks, live.body, true)
		return
	}
	trace.Blocks = append(trace.Blocks, Block{ID: trace.nextBlockID(), Kind: live.kind, Title: "agent", Body: live.body})
}

func eventTime(event agent.Event) time.Time {
	if event.CreatedAt.IsZero() {
		return time.Now()
	}
	return event.CreatedAt
}

func subagentBlockFromEvent(event agent.Event) (taskID, title, status, body string) {
	status = subagentStatusFromEvent(event)
	taskID = strings.TrimSpace(event.ToolCallID)
	title = "subagent"
	if name := strings.TrimSpace(event.ToolName); name != "" {
		title += " " + name
	}
	parts := []string{}
	if message := strings.TrimSpace(event.Message); message != "" {
		parts = append(parts, subagentMessageLabel(event.Type)+": "+message)
	}
	if name := strings.TrimSpace(event.ToolName); name != "" {
		parts = append(parts, "type: "+name)
	}
	if output := strings.TrimSpace(event.Output); output != "" && output != strings.TrimSpace(event.Message) {
		parts = append(parts, "output: "+output)
	}
	if errText := strings.TrimSpace(event.Error); errText != "" {
		parts = append(parts, "error: "+errText)
	}
	return taskID, title, status, strings.Join(parts, "\n")
}

func subagentStatusFromEvent(event agent.Event) string {
	if strings.TrimSpace(event.Error) != "" {
		return "failed"
	}
	switch event.Type {
	case agent.EventSubagentStarted, agent.EventSubagentProgress:
		return "running"
	case agent.EventSubagentFinished:
		return "completed"
	case agent.EventSubagentFailed:
		return "failed"
	case agent.EventSubagentCancelled:
		return "cancelled"
	default:
		return ""
	}
}

func subagentMessageLabel(eventType agent.EventType) string {
	switch eventType {
	case agent.EventSubagentFinished:
		return "result"
	case agent.EventSubagentFailed, agent.EventSubagentCancelled:
		return "message"
	default:
		return "description"
	}
}

func toolBlockFromEvent(event agent.Event, status ToolStatus) *ToolBlock {
	output := firstNonEmpty(event.Output, event.Message)
	if status == ToolRunning || status == ToolPending || status == ToolRejected {
		output = event.Output
	}
	tool := &ToolBlock{
		ID:        toolID(event),
		Name:      event.ToolName,
		Status:    status,
		Risk:      event.Risk,
		Input:     firstNonEmpty(event.Input),
		Output:    output,
		Error:     event.Error,
		StartedAt: event.CreatedAt,
		EndedAt:   event.CreatedAt,
	}
	tool.Summary = summarizeToolBlock(tool, event.Message)
	tool.Collapsed = shouldCollapseToolOutput(tool.Output)
	return tool
}

func mergeToolBlock(existing, next *ToolBlock) *ToolBlock {
	if existing == nil {
		return next
	}
	merged := *existing
	if next.ID != "" {
		merged.ID = next.ID
	}
	if next.Name != "" {
		merged.Name = next.Name
	}
	if next.Status != "" {
		merged.Status = next.Status
	}
	if next.Risk != "" {
		merged.Risk = next.Risk
	}
	if next.Input != "" {
		merged.Input = next.Input
	}
	if next.Output != "" {
		merged.Output = next.Output
	}
	if next.Error != "" {
		merged.Error = next.Error
	}
	if !next.StartedAt.IsZero() && (merged.StartedAt.IsZero() || next.StartedAt.Before(merged.StartedAt)) {
		merged.StartedAt = next.StartedAt
	}
	if !next.EndedAt.IsZero() {
		merged.EndedAt = next.EndedAt
	}
	if !merged.StartedAt.IsZero() && !merged.EndedAt.IsZero() && !merged.EndedAt.Before(merged.StartedAt) {
		merged.Duration = merged.EndedAt.Sub(merged.StartedAt)
	}
	if next.Summary != "" && next.Output == "" && next.Error == "" {
		merged.Summary = next.Summary
	} else {
		merged.Summary = summarizeToolBlock(&merged, next.Summary)
	}
	merged.Collapsed = existing.Collapsed || shouldCollapseToolOutput(merged.Output)
	return &merged
}

func toolID(event agent.Event) string {
	if event.ToolCallID != "" {
		return event.ToolCallID
	}
	return ""
}

func renderToolBody(tool *ToolBlock) string {
	parts := []string{
		fmt.Sprintf("status: %s", tool.Status),
	}
	if label := toolLabel(tool); label != "" {
		parts = append(parts, "label: "+label)
	}
	if tool.Risk != "" {
		parts = append(parts, fmt.Sprintf("risk: %s", tool.Risk))
	}
	if tool.Duration > 0 {
		parts = append(parts, fmt.Sprintf("duration: %s", tool.Duration.Round(time.Millisecond)))
	}
	if tool.Summary != "" {
		parts = append(parts, "summary: "+tool.Summary)
	}
	if tool.Input != "" {
		parts = append(parts, "input: "+tool.Input)
	}
	if tool.Output != "" {
		parts = append(parts, "output: "+tool.Output)
	}
	if tool.Error != "" {
		parts = append(parts, "error: "+tool.Error)
	}
	return strings.Join(parts, "\n")
}

func summarizeToolBlock(tool *ToolBlock, fallback string) string {
	if tool == nil {
		return fallback
	}
	label := toolLabel(tool)
	commandSummary := summarizeToolInput(tool.Name, tool.Input)
	outputSummary := summarizeToolOutput(commandSummary, tool.Output, tool.Error)
	return firstNonEmpty(outputSummary, label, commandSummary, fallback)
}

func toolTitle(tool *ToolBlock) string {
	if label := toolLabel(tool); label != "" {
		if tool != nil && tool.Name != "" {
			return fmt.Sprintf("tool %s · %s", tool.Name, label)
		}
		return "tool " + label
	}
	if tool == nil || tool.Name == "" {
		return "tool"
	}
	return "tool " + tool.Name
}

func toolLabel(tool *ToolBlock) string {
	if tool == nil {
		return ""
	}
	params := parseToolInput(tool.Input)
	value := func(keys ...string) string {
		for _, key := range keys {
			if v := strings.TrimSpace(params[key]); v != "" {
				return truncatePlain(v, 80)
			}
		}
		return ""
	}
	switch tool.Name {
	case "read_file":
		return callLabel("ReadFile", value("path"), false)
	case "write_file":
		return callLabel("WriteFile", value("path"), false)
	case "list_dir":
		return callLabel("ListDir", value("path"), false)
	case "run_command", "execute_command":
		return callLabel("Shell", value("command"), false)
	case "search_text", "search_code", "web_search":
		return callLabel("Search", value("query", "pattern"), true)
	case "web_fetch":
		return callLabel("Fetch", compactURL(value("url")), false)
	default:
		if strings.HasPrefix(tool.Name, "mcp__") {
			return "MCP " + formatMCPToolName(tool.Name)
		}
		return ""
	}
}

func parseToolInput(input string) map[string]string {
	params := map[string]string{}
	if strings.TrimSpace(input) == "" {
		return params
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		return params
	}
	for key, value := range raw {
		switch v := value.(type) {
		case string:
			params[key] = v
		case fmt.Stringer:
			params[key] = v.String()
		case float64, bool:
			params[key] = fmt.Sprint(v)
		}
	}
	return params
}

func callLabel(name, value string, quote bool) string {
	if strings.TrimSpace(value) == "" {
		return name
	}
	if quote {
		return fmt.Sprintf("%s(%q)", name, value)
	}
	return fmt.Sprintf("%s(%s)", name, value)
}

func formatMCPToolName(name string) string {
	parts := strings.SplitN(name, "__", 3)
	if len(parts) == 3 {
		return parts[1] + "." + parts[2]
	}
	return name
}

func compactURL(url string) string {
	url = strings.TrimSpace(url)
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimRight(url, "/")
	return truncatePlain(url, 80)
}

func summarizeToolInput(name, input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || trimmed == "{}" {
		return name
	}
	if strings.Contains(trimmed, "go test") {
		return "go test"
	}
	if strings.Contains(trimmed, "git diff") {
		return "git diff"
	}
	if name == "run_command" && strings.Contains(trimmed, "command") {
		return truncatePlain(trimmed, 80)
	}
	return truncatePlain(trimmed, 80)
}

func summarizeToolOutput(commandSummary, output, errText string) string {
	output = strings.TrimSpace(output)
	errText = strings.TrimSpace(errText)
	if commandSummary == "go test" {
		return summarizeGoTestOutput(output, errText)
	}
	if commandSummary == "git diff" {
		added, removed := summarizeGitDiffOutput(output)
		return fmt.Sprintf("git diff: +%d -%d", added, removed)
	}
	if errText != "" {
		return "failed: " + truncatePlain(errText, 80)
	}
	return truncatePlain(firstLine(output), 100)
}

func summarizeGoTestOutput(output, errText string) string {
	okCount := 0
	failCount := 0
	compileError := strings.TrimSpace(errText) != ""
	panicSeen := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "ok\t"), strings.HasPrefix(trimmed, "ok  "):
			okCount++
		case strings.HasPrefix(trimmed, "FAIL\t"), strings.HasPrefix(trimmed, "FAIL "):
			failCount++
		case strings.HasPrefix(trimmed, "# "):
			compileError = true
		case strings.Contains(trimmed, ": undefined:"), strings.Contains(trimmed, "undefined:"), strings.Contains(trimmed, "build failed"):
			compileError = true
		}
		if strings.Contains(strings.ToLower(trimmed), "panic:") {
			panicSeen = true
		}
	}

	parts := []string{fmt.Sprintf("ok %d", okCount)}
	if failCount > 0 {
		parts = append(parts, fmt.Sprintf("FAIL %d", failCount))
	}
	if compileError {
		parts = append(parts, "compile error")
	}
	if panicSeen {
		parts = append(parts, "panic")
	}
	if okCount == 0 && failCount == 0 && !compileError && !panicSeen {
		return truncatePlain(firstLine(output), 100)
	}
	return "go test: " + strings.Join(parts, ", ")
}

func summarizeGitDiffOutput(output string) (int, int) {
	added := 0
	removed := 0
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"):
			continue
		case strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

func shouldCollapseToolOutput(output string) bool {
	if output == "" {
		return false
	}
	return strings.Count(output, "\n") >= 20 || len(output) > 2000
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	line, _, _ := strings.Cut(value, "\n")
	return line
}

func truncatePlain(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	// Truncate by display width so multi-byte runes (CJK, ·, etc.) don't get
	// cut mid-character and the visible width stays within limit.
	tail := "..."
	if limit <= runewidth.StringWidth(tail) {
		tail = ""
	}
	return runewidth.Truncate(value, limit, tail)
}

// applyReasoningChunk updates only the live overlay; committed history stays
// immutable until a structural boundary calls commitLiveStream.
func (m Model) applyReasoningChunk(chunk string, at time.Time) Model {
	return m.appendLiveChunk(BlockReasoning, chunk, at)
}

// applyAssistantChunk updates only the live overlay; it does not mutate the
// committed timeline for each provider delta.
func (m Model) applyAssistantChunk(chunk string, at time.Time) Model {
	return m.appendLiveChunk(BlockAssistant, chunk, at)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
