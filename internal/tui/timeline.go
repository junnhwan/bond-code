package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/junnhwan/bond-code/internal/command"
)

type MessageBlock struct {
	ID        string
	Title     string
	Body      string
	CreatedAt time.Time
}

type TimelineState struct {
	Turns          []Turn
	Scroll         int
	FocusedBlockID string
	// Version bumps on every committed structural or content change (new
	// turn/block, tool status update, collapse toggle) so the view layer can
	// memoize rendered history. Live model/reasoning deltas stay in
	// AgentRunState.LiveStream and deliberately do not bump this version.
	// View-only mutations (Scroll, focused block) do not bump, by design.
	Version int
}

type Turn struct {
	ID        string
	User      MessageBlock
	Blocks    []Block
	StartedAt time.Time
	EndedAt   time.Time
	Run       TurnRunStatus
}

type BlockKind string

const (
	BlockAssistant    BlockKind = "assistant"
	BlockTool         BlockKind = "tool"
	BlockConfirmation BlockKind = "confirmation"
	BlockCommand      BlockKind = "command"
	BlockSubagent     BlockKind = "subagent"
	BlockReasoning    BlockKind = "reasoning"
	BlockError        BlockKind = "error"
	// BlockCompaction marks where the context window was summarized. It renders
	// as a dim divider (── compacted before → after tokens ──) so the user can
	// see the memory boundary instead of wondering why the agent "forgot".
	BlockCompaction BlockKind = "compaction"
)

type Block struct {
	ID      string
	Kind    BlockKind
	Title   string
	Summary string
	Body    string
	Tool    *ToolBlock
	// Panel, when set on a BlockCommand, renders as a bordered panel instead
	// of the plain-text Body. Nil for every other block kind.
	Panel     *command.Panel
	CreatedAt time.Time
}

type TurnRunStatus struct {
	State     string
	Detail    string
	StartedAt time.Time
	EndedAt   time.Time
}

func (s TimelineState) StartUserTurn(prompt string) TimelineState {
	now := time.Now()
	turn := Turn{
		ID:        fmt.Sprintf("turn-%d", len(s.Turns)+1),
		StartedAt: now,
		User: MessageBlock{
			ID:        fmt.Sprintf("user-%d", len(s.Turns)+1),
			Title:     "user",
			Body:      prompt,
			CreatedAt: now,
		},
	}
	s.Turns = append(append([]Turn(nil), s.Turns...), turn)
	s.Version++
	return s
}

func (s TimelineState) MarkAgentStarted(at time.Time) TimelineState {
	if at.IsZero() {
		at = time.Now()
	}
	s = s.ensureTurn()
	idx := len(s.Turns) - 1
	s.Turns = append([]Turn(nil), s.Turns...)
	s.Version++
	turn := s.Turns[idx]
	if turn.StartedAt.IsZero() {
		turn.StartedAt = at
	}
	turn.EndedAt = time.Time{}
	turn.Run = TurnRunStatus{
		State:     "working",
		Detail:    "thinking",
		StartedAt: firstTime(turn.Run.StartedAt, turn.StartedAt, at),
	}
	s.Turns[idx] = turn
	return s
}

func (s TimelineState) UpdateAgentStatus(state, detail string, at time.Time) TimelineState {
	if at.IsZero() {
		at = time.Now()
	}
	s = s.ensureTurn()
	idx := len(s.Turns) - 1
	s.Turns = append([]Turn(nil), s.Turns...)
	s.Version++
	turn := s.Turns[idx]
	if turn.StartedAt.IsZero() {
		turn.StartedAt = at
	}
	run := turn.Run
	run.State = firstNonEmpty(state, run.State, "working")
	run.Detail = firstNonEmpty(detail, run.Detail, "thinking")
	run.StartedAt = firstTime(run.StartedAt, turn.StartedAt, at)
	run.EndedAt = time.Time{}
	turn.Run = run
	s.Turns[idx] = turn
	return s
}

func (s TimelineState) MarkAgentEnded(state, detail string, at time.Time) TimelineState {
	if at.IsZero() {
		at = time.Now()
	}
	s = s.ensureTurn()
	idx := len(s.Turns) - 1
	s.Turns = append([]Turn(nil), s.Turns...)
	s.Version++
	turn := s.Turns[idx]
	if turn.StartedAt.IsZero() {
		turn.StartedAt = at
	}
	turn.EndedAt = at
	run := turn.Run
	run.State = firstNonEmpty(state, "done")
	run.Detail = detail
	run.StartedAt = firstTime(run.StartedAt, turn.StartedAt, at)
	run.EndedAt = at
	turn.Run = run
	s.Turns[idx] = turn
	return s
}

func (s TimelineState) ensureTurn() TimelineState {
	if len(s.Turns) == 0 {
		return s.StartUserTurn("")
	}
	return s
}

func (s TimelineState) latestToolBlock() *ToolBlock {
	for turnIdx := len(s.Turns) - 1; turnIdx >= 0; turnIdx-- {
		blocks := s.Turns[turnIdx].Blocks
		for blockIdx := len(blocks) - 1; blockIdx >= 0; blockIdx-- {
			if blocks[blockIdx].Tool != nil {
				return blocks[blockIdx].Tool
			}
		}
	}
	return nil
}

func (s TimelineState) AppendAssistantChunk(chunk string) TimelineState {
	s = s.ensureTurn()
	body := chunk
	turn := s.Turns[len(s.Turns)-1]
	if len(turn.Blocks) > 0 && turn.Blocks[len(turn.Blocks)-1].Kind == BlockAssistant {
		body = turn.Blocks[len(turn.Blocks)-1].Body + chunk
	}
	return s.SetAssistantBody(body)
}

// SetAssistantBody replaces the complete body of the latest consecutive
// assistant block, or creates that block when the turn currently ends in a
// different kind. Callers that already maintain an amortized stream buffer can
// use this path without concatenating and copying the full growing body again.
func (s TimelineState) SetAssistantBody(body string) TimelineState {
	s = s.ensureTurn()
	idx := len(s.Turns) - 1
	s.Turns = append([]Turn(nil), s.Turns...)
	s.Version++
	turn := s.Turns[idx]
	turn.Blocks = append([]Block(nil), turn.Blocks...)
	if len(turn.Blocks) > 0 {
		lastIdx := len(turn.Blocks) - 1
		if turn.Blocks[lastIdx].Kind == BlockAssistant {
			turn.Blocks[lastIdx].Body = body
			s.Turns[idx] = turn
			return s
		}
	}
	turn.Blocks = append(turn.Blocks, Block{
		ID:        fmt.Sprintf("%s-assistant-%d", turn.ID, len(turn.Blocks)+1),
		Kind:      BlockAssistant,
		Title:     "agent",
		Body:      body,
		CreatedAt: time.Now(),
	})
	s.Turns[idx] = turn
	return s
}

// AppendReasoningChunk streams a raw reasoning delta into the turn's single
// thinking block (one per turn). Tool/assistant rows are never created or
// removed here — live streaming normally uses LiveStream instead.
func (s TimelineState) AppendReasoningChunk(chunk string) TimelineState {
	if chunk == "" {
		return s
	}
	s = s.ensureTurn()
	turn := s.Turns[len(s.Turns)-1]
	if idx := firstReasoningBlockIndex(turn.Blocks); idx >= 0 {
		return s.withTurnBlocks(consolidateReasoningBlocks(turn.ID, turn.Blocks, turn.Blocks[idx].Body+chunk, false))
	}
	return s.withTurnBlocks(consolidateReasoningBlocks(turn.ID, turn.Blocks, chunk, false))
}

// SetReasoningBody replaces the body of the turn's single reasoning block, or
// creates that block when the turn has none yet. Extra reasoning blocks left
// by older multi-block commits are collapsed into the first; tool/assistant
// rows keep their relative order and are never dropped.
func (s TimelineState) SetReasoningBody(body string) TimelineState {
	s = s.ensureTurn()
	turn := s.Turns[len(s.Turns)-1]
	return s.withTurnBlocks(consolidateReasoningBlocks(turn.ID, turn.Blocks, body, false))
}

// MergeTurnReasoning commits a live reasoning segment at a structural boundary
// (tool call, assistant text, turn end).
//
// Product rule: one thinking block per user turn (default folded). Every
// later reasoning segment is absorbed into that same block. Tool rows stay
// as their own blocks in commit order — they are never deleted, hidden, or
// folded into the thinking body. Typical transcript:
//
//	thinking (one header) → tool → tool → tool → answer
//
// not N thinking headers interleaved with tools, and not a rebuild that
// drops tool rows while merging prose.
func (s TimelineState) MergeTurnReasoning(segment string) TimelineState {
	segment = strings.TrimRight(segment, "\n")
	if segment == "" {
		return s
	}
	s = s.ensureTurn()
	turn := s.Turns[len(s.Turns)-1]
	return s.withTurnBlocks(consolidateReasoningBlocks(turn.ID, turn.Blocks, segment, true))
}

// withTurnBlocks writes a new Blocks slice onto the latest turn and bumps Version.
func (s TimelineState) withTurnBlocks(blocks []Block) TimelineState {
	idx := len(s.Turns) - 1
	s.Turns = append([]Turn(nil), s.Turns...)
	s.Version++
	turn := s.Turns[idx]
	turn.Blocks = blocks
	s.Turns[idx] = turn
	return s
}

// firstReasoningBlockIndex returns the index of the first reasoning block, or -1.
func firstReasoningBlockIndex(blocks []Block) int {
	for i := range blocks {
		if blocks[i].Kind == BlockReasoning {
			return i
		}
	}
	return -1
}

// joinReasoningBodies concatenates non-empty reasoning bodies with a blank line.
func joinReasoningBodies(parts ...string) string {
	var b strings.Builder
	for _, part := range parts {
		part = strings.TrimRight(part, "\n")
		if part == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(part)
	}
	return b.String()
}

// consolidateReasoningBlocks keeps exactly one reasoning block in the turn.
// Non-reasoning blocks (tools, assistant, …) are copied through in order and
// never dropped. appendSegment=true appends body onto existing reasoning text
// (commit boundary); false replaces the body (SetReasoningBody).
func consolidateReasoningBlocks(turnID string, blocks []Block, body string, appendSegment bool) []Block {
	body = strings.TrimRight(body, "\n")
	kept := make([]Block, 0, len(blocks)+1)
	reasonIdx := -1
	var existingParts []string
	for _, b := range blocks {
		if b.Kind == BlockReasoning {
			if reasonIdx < 0 {
				reasonIdx = len(kept)
				kept = append(kept, b)
			}
			if part := strings.TrimRight(b.Body, "\n"); part != "" {
				existingParts = append(existingParts, part)
			}
			continue
		}
		kept = append(kept, b)
	}

	existing := joinReasoningBodies(existingParts...)
	var finalBody string
	if appendSegment {
		switch {
		case body == "":
			finalBody = existing
		case existing == "":
			finalBody = body
		case body == existing || strings.HasSuffix(existing, "\n\n"+body):
			// Idempotent re-commit of the same live segment.
			finalBody = existing
		default:
			finalBody = joinReasoningBodies(existing, body)
		}
	} else {
		finalBody = body
	}

	if reasonIdx >= 0 {
		kept[reasonIdx].Body = finalBody
		kept[reasonIdx].Kind = BlockReasoning
		if kept[reasonIdx].Title == "" {
			kept[reasonIdx].Title = "thinking"
		}
		return kept
	}
	if finalBody == "" {
		return kept
	}
	id := fmt.Sprintf("%s-reasoning-1", turnID)
	if turnID == "" {
		id = fmt.Sprintf("reasoning-%d", len(kept)+1)
	}
	return append(kept, Block{
		ID:        id,
		Kind:      BlockReasoning,
		Title:     "thinking",
		Body:      finalBody,
		CreatedAt: time.Now(),
	})
}

func (s TimelineState) UpsertToolBlock(tool *ToolBlock) TimelineState {
	if tool == nil {
		return s
	}
	s = s.ensureTurn()
	idx := len(s.Turns) - 1
	s.Turns = append([]Turn(nil), s.Turns...)
	s.Version++
	turn := s.Turns[idx]
	turn.Blocks = append([]Block(nil), turn.Blocks...)
	for i, block := range turn.Blocks {
		if block.Tool == nil {
			continue
		}
		if tool.ID != "" && block.Tool.ID == tool.ID {
			merged := mergeToolBlock(block.Tool, tool)
			turn.Blocks[i] = toolBlockToBlock(merged, block.ID)
			s.Turns[idx] = turn
			return s
		}
		if tool.ID == "" && block.Tool.ID == "" && block.Tool.Name == tool.Name && !isTerminalToolStatus(block.Tool.Status) {
			merged := mergeToolBlock(block.Tool, tool)
			turn.Blocks[i] = toolBlockToBlock(merged, block.ID)
			s.Turns[idx] = turn
			return s
		}
	}
	fallbackID := fmt.Sprintf("%s-tool-%d", turn.ID, len(turn.Blocks)+1)
	turn.Blocks = append(turn.Blocks, toolBlockToBlock(tool, fallbackID))
	s.Turns[idx] = turn
	return s
}

func (s TimelineState) AppendBlock(kind BlockKind, title, body string) TimelineState {
	return s.appendBlock(kind, title, "", body)
}

// AppendCommandBlock appends a BlockCommand. When panel is non-nil the TUI
// renders it as a bordered panel; body stays the canonical plain-text output
// used for logs and snapshots.
func (s TimelineState) AppendCommandBlock(title, body string, panel *command.Panel) TimelineState {
	s = s.ensureTurn()
	idx := len(s.Turns) - 1
	s.Turns = append([]Turn(nil), s.Turns...)
	s.Version++
	turn := s.Turns[idx]
	turn.Blocks = append([]Block(nil), turn.Blocks...)
	turn.Blocks = append(turn.Blocks, Block{
		ID:        fmt.Sprintf("%s-command-%d", turn.ID, len(turn.Blocks)+1),
		Kind:      BlockCommand,
		Title:     title,
		Body:      body,
		Panel:     panel,
		CreatedAt: time.Now(),
	})
	s.Turns[idx] = turn
	return s
}

// SeedTimeline rebuilds a timeline from a resumed conversation so prior turns
// stay visible alongside new ones. User messages open turns; assistant/tool
// messages append blocks to the current turn.
func SeedTimeline(messages []SeedMessage) TimelineState {
	var s TimelineState
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			s = s.StartUserTurn(msg.Content)
		case "assistant":
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			s = s.AppendBlock(BlockAssistant, "agent", msg.Content)
		case "tool":
			title := "tool"
			if msg.ToolName != "" {
				title = "tool " + msg.ToolName
			}
			s = s.AppendBlock(BlockTool, title, msg.Content)
		}
	}
	return s
}

func (s TimelineState) AppendSubagentBlock(title, status, body string) TimelineState {
	return s.appendBlock(BlockSubagent, title, status, body)
}

// UpsertSubagentBlock adds or updates a subagent block by taskID so a child
// agent's started/progress/finished events merge into one lifecycle block
// instead of producing one block per event.
func (s TimelineState) UpsertSubagentBlock(taskID, title, status, body string) TimelineState {
	s = s.ensureTurn()
	idx := len(s.Turns) - 1
	s.Turns = append([]Turn(nil), s.Turns...)
	s.Version++
	turn := s.Turns[idx]
	turn.Blocks = append([]Block(nil), turn.Blocks...)
	if taskID != "" {
		for i, block := range turn.Blocks {
			if block.Kind == BlockSubagent && block.ID == taskID {
				if title != "" {
					block.Title = title
				}
				if status != "" {
					block.Summary = status
				}
				if strings.TrimSpace(body) != "" {
					block.Body = body
				}
				turn.Blocks[i] = block
				s.Turns[idx] = turn
				return s
			}
		}
	}
	id := taskID
	if id == "" {
		id = fmt.Sprintf("%s-subagent-%d", turn.ID, len(turn.Blocks)+1)
	}
	turn.Blocks = append(turn.Blocks, Block{
		ID:        id,
		Kind:      BlockSubagent,
		Title:     title,
		Summary:   status,
		Body:      body,
		CreatedAt: time.Now(),
	})
	s.Turns[idx] = turn
	return s
}

func (s TimelineState) appendBlock(kind BlockKind, title, summary, body string) TimelineState {
	if kind == BlockReasoning {
		return s.MergeTurnReasoning(body)
	}
	s = s.ensureTurn()
	idx := len(s.Turns) - 1
	s.Turns = append([]Turn(nil), s.Turns...)
	s.Version++
	turn := s.Turns[idx]
	turn.Blocks = append([]Block(nil), turn.Blocks...)
	turn.Blocks = append(turn.Blocks, Block{
		ID:        fmt.Sprintf("%s-%s-%d", turn.ID, kind, len(turn.Blocks)+1),
		Kind:      kind,
		Title:     title,
		Summary:   summary,
		Body:      body,
		CreatedAt: time.Now(),
	})
	s.Turns[idx] = turn
	return s
}

func (s TimelineState) SetLatestToolCollapsed(tool *ToolBlock, collapsed bool) TimelineState {
	if tool == nil {
		return s
	}
	for turnIdx := len(s.Turns) - 1; turnIdx >= 0; turnIdx-- {
		turn := s.Turns[turnIdx]
		for blockIdx := len(turn.Blocks) - 1; blockIdx >= 0; blockIdx-- {
			block := turn.Blocks[blockIdx]
			if !toolBlockMatches(block.Tool, tool) {
				continue
			}
			return s.setToolCollapsed(turnIdx, blockIdx, collapsed)
		}
	}
	return s
}

func (s TimelineState) SetAllToolCollapsed(collapsed bool) TimelineState {
	for turnIdx, turn := range s.Turns {
		for blockIdx, block := range turn.Blocks {
			if block.Tool == nil || !shouldCollapseToolOutput(block.Tool.Output) {
				continue
			}
			s = s.setToolCollapsed(turnIdx, blockIdx, collapsed)
		}
	}
	return s
}

func (s TimelineState) setToolCollapsed(turnIdx int, blockIdx int, collapsed bool) TimelineState {
	if turnIdx < 0 || turnIdx >= len(s.Turns) {
		return s
	}
	s.Turns = append([]Turn(nil), s.Turns...)
	s.Version++
	turn := s.Turns[turnIdx]
	if blockIdx < 0 || blockIdx >= len(turn.Blocks) {
		return s
	}
	turn.Blocks = append([]Block(nil), turn.Blocks...)
	block := turn.Blocks[blockIdx]
	tool := cloneToolBlock(block.Tool)
	if tool == nil {
		return s
	}
	tool.Collapsed = collapsed
	block.Tool = tool
	block.Body = renderToolBody(tool)
	turn.Blocks[blockIdx] = block
	s.Turns[turnIdx] = turn
	return s
}

func toolBlockMatches(existing *ToolBlock, target *ToolBlock) bool {
	if existing == nil || target == nil {
		return false
	}
	if target.ID != "" {
		return existing.ID == target.ID
	}
	return existing.ID == "" && existing.Name == target.Name
}

func toolBlockToBlock(tool *ToolBlock, fallbackID string) Block {
	cloned := cloneToolBlock(tool)
	if cloned == nil {
		return Block{ID: fallbackID, Kind: BlockTool}
	}
	return Block{
		ID:      firstNonEmpty(cloned.ID, fallbackID, cloned.Name),
		Kind:    BlockTool,
		Title:   toolTitle(cloned),
		Summary: cloned.Summary,
		Body:    renderToolBody(cloned),
		Tool:    cloned,
	}
}

func cloneToolBlock(tool *ToolBlock) *ToolBlock {
	if tool == nil {
		return nil
	}
	cloned := *tool
	return &cloned
}

func firstTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}
