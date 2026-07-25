package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/tool"
)

// SubagentManager manages concurrent subagent execution.
type SubagentManager struct {
	client       llm.Client
	registry     *tool.Registry
	options      ManagerOptions
	runningTasks sync.Map // taskID (string) -> context.CancelFunc
	taskInputs   sync.Map // taskID (string) -> chan string, consumed by agent.Loop at turn boundaries
	sessionTasks sync.Map // sessionID (string) -> *taskSet
	results      chan SubagentResult
	counter      atomic.Int64
	activeTasks  atomic.Int64
	// resumable (Phase 4) maps a completed child's taskID to its saved message
	// history + profile, so a follow-up task call with resume_task_id can
	// continue that child's conversation instead of starting fresh. Bounded by
	// session lifecycle: CancelBySession drops the session's entries. Successful child runs populate it with their Loop message history.
	resumable sync.Map // taskID (string) -> *resumableSession
}

// resumableSession is the saved state a resume_task_id continues from.
type resumableSession struct {
	SessionID string
	AgentType AgentType
	Messages  []llm.Message
}

func NewSubagentManagerWithOptions(client llm.Client, registry *tool.Registry, options ManagerOptions) *SubagentManager {
	if registry == nil {
		registry = tool.NewRegistry()
	}
	defaults := DefaultManagerOptions()
	if options.MaxChildrenPerTurn <= 0 {
		options.MaxChildrenPerTurn = defaults.MaxChildrenPerTurn
	}
	if options.MaxDepth <= 0 {
		options.MaxDepth = defaults.MaxDepth
	}
	if options.DefaultTimeoutSeconds <= 0 {
		options.DefaultTimeoutSeconds = defaults.DefaultTimeoutSeconds
	}
	if options.MaxResultChars <= 0 {
		options.MaxResultChars = defaults.MaxResultChars
	}
	return &SubagentManager{
		client:   client,
		registry: registry,
		options:  options,
		results:  make(chan SubagentResult, 16),
	}
}

// Spawn starts a new subagent in the background. It is kept for experimental/debug use.
func (m *SubagentManager) Spawn(ctx context.Context, sessionID, prompt string) (string, error) {
	if err := m.validateChildExecution(); err != nil {
		return "", err
	}
	taskID := m.nextTaskID()
	taskCtx, cancel := context.WithCancel(context.Background())
	m.runningTasks.Store(taskID, cancel)

	ts := m.getOrCreateTaskSet(sessionID)
	ts.add(taskID)

	observe.SafeGo("subagent-spawn", func() { m.runAgentLoop(taskCtx, taskID, sessionID, prompt) })

	return taskID, nil
}

// RunTask runs a bounded subagent synchronously and returns the compact result.
func (m *SubagentManager) RunTask(ctx context.Context, req TaskRequest) (*SubagentResult, error) {
	if err := m.validateChildExecution(); err != nil {
		return nil, err
	}
	if err := ValidateAgentType(req.SubagentType); err != nil {
		return nil, err
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	req.Description = strings.TrimSpace(req.Description)
	if req.Prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if req.SubagentType == "" {
		req.SubagentType = AgentTypeResearch
	}
	if req.TaskID == "" {
		req.TaskID = m.nextTaskID()
	}

	limit := m.options.MaxChildrenPerTurn
	if limit <= 0 {
		limit = DefaultManagerOptions().MaxChildrenPerTurn
	}
	active := m.activeTasks.Add(1)
	if active > int64(limit) {
		m.activeTasks.Add(-1)
		return nil, fmt.Errorf("subagent concurrency limit exceeded: %d", limit)
	}
	defer m.activeTasks.Add(-1)

	timeout := m.options.DefaultTimeoutSeconds
	if timeout <= 0 {
		timeout = DefaultManagerOptions().DefaultTimeoutSeconds
	}
	taskCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	// Register the cancel so CancelTask can interrupt this otherwise-synchronous
	// run (RunTask blocks its caller until it returns). Cleared on exit.
	m.runningTasks.Store(req.TaskID, cancel)
	input := make(chan string, 32)
	m.taskInputs.Store(req.TaskID, input)
	defer func() {
		cancel()
		m.runningTasks.Delete(req.TaskID)
		m.taskInputs.Delete(req.TaskID)
	}()

	profile := DefaultAgentProfile(req.SubagentType)
	// Phase 4 resume: if the caller passed a resume_task_id, continue that prior
	// child's conversation. The prior child's profile wins (toolset must stay
	// consistent across a resumed conversation), and its saved message history is
	// threaded into the run as the initial messages. Unknown id => protocol-safe
	// failed result (the run never executes).
	var resumeHistory []llm.Message
	if strings.TrimSpace(req.ResumeTaskID) != "" {
		entry, ok := m.loadResumable(req.ResumeTaskID)
		if !ok || entry.SessionID != req.SessionID {
			return &SubagentResult{
				TaskID:      req.TaskID,
				Description: req.Description,
				Prompt:      req.Prompt,
				AgentType:   profile.Type,
				Status:      "failed",
				StartTime:   time.Now(),
				EndTime:     time.Now(),
				Error:       fmt.Sprintf("resume_task_id %q not found (no saved child context; resume is session-scoped and only available for tasks that ran to completion)", req.ResumeTaskID),
			}, nil
		}
		profile = DefaultAgentProfile(entry.AgentType)
		req.SubagentType = entry.AgentType
		resumeHistory = entry.Messages
	}
	m.emit(Event{
		Type:        EventStarted,
		TaskID:      req.TaskID,
		Description: req.Description,
		AgentType:   profile.Type,
		Status:      "running",
		Generation:  req.Generation,
		Message:     "subagent started",
	})
	// Every child runs through the injected agent.Loop. There is deliberately no
	// fallback executor: a private mini-loop could bypass Policy+Confirmer.
	result, err := m.runTaskViaLoop(taskCtx, req, profile, resumeHistory)
	if result != nil {
		result.FinalAnswer = m.capResultOutput(result.FinalAnswer)
		// Only successful runs become resumable. Failed/partial histories can end
		// in an unmatched tool protocol or an unusable budget fallback.
		if result.Status == "completed" && len(result.Messages) > 0 {
			m.saveResumable(req.TaskID, resumableSession{SessionID: req.SessionID, AgentType: profile.Type, Messages: result.Messages})
		}
	}
	if err != nil {
		m.emit(Event{
			Type:        EventFailed,
			TaskID:      req.TaskID,
			Description: req.Description,
			AgentType:   profile.Type,
			Status:      "failed",
			Generation:  req.Generation,
			Error:       err.Error(),
		})
		return result, err
	}
	terminal := eventFromResult(result)
	terminal.Generation = req.Generation
	m.emit(terminal)
	return result, nil
}

// PollResults non-blocking read from results channel.
func (m *SubagentManager) PollResults() []SubagentResult {
	results := []SubagentResult{}
	for {
		select {
		case result := <-m.results:
			results = append(results, result)
		default:
			return results
		}
	}
}

// CancelBySession cancels all tasks for a session.
func (m *SubagentManager) CancelBySession(sessionID string) int {
	val, ok := m.sessionTasks.Load(sessionID)
	if !ok {
		return 0
	}
	ts := val.(*taskSet)
	taskIDs := ts.snapshot()

	cancelledCount := 0
	for _, taskID := range taskIDs {
		if cancel, ok := m.runningTasks.LoadAndDelete(taskID); ok {
			cancelFunc := cancel.(context.CancelFunc)
			cancelFunc()
			cancelledCount++
		}
		// Drop any saved resumable context for this session's tasks so it does
		// not leak across sessions (resume is session-scoped by design).
		m.resumable.Delete(taskID)
	}

	m.sessionTasks.Delete(sessionID)

	return cancelledCount
}

// CancelTask cancels a single running task by id (best-effort). Returns true
// when a live cancel was found and invoked. Safe from any goroutine: a
// synchronous RunTask whose taskCtx gets cancelled returns a "cancelled" result
// on its next iteration check, unblocking its caller.
func (m *SubagentManager) CancelTask(taskID string) bool {
	if strings.TrimSpace(taskID) == "" {
		return false
	}
	cancel, ok := m.runningTasks.LoadAndDelete(taskID)
	if !ok {
		return false
	}
	cancel.(context.CancelFunc)()
	return true
}

func (m *SubagentManager) getOrCreateTaskSet(sessionID string) *taskSet {
	ts, _ := m.sessionTasks.LoadOrStore(sessionID, &taskSet{})
	return ts.(*taskSet)
}

func (m *SubagentManager) nextTaskID() string {
	counter := m.counter.Add(1)
	return fmt.Sprintf("sub-%d-%d", time.Now().Unix(), counter)
}

func (m *SubagentManager) emit(event Event) {
	if m.options.EventSink == nil {
		return
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	m.options.EventSink(event)
}

func (m *SubagentManager) validateChildExecution() error {
	if m.client == nil {
		return fmt.Errorf("subagent client is nil")
	}
	if m.options.LoopFactory == nil {
		return fmt.Errorf("subagent LoopFactory is required")
	}
	return nil
}

func (m *SubagentManager) capResultOutput(output string) string {
	return capString(output, m.resultCap(), "[subagent result truncated]")
}

func (m *SubagentManager) resultCap() int {
	if m.options.MaxResultChars <= 0 {
		return DefaultManagerOptions().MaxResultChars
	}
	return m.options.MaxResultChars
}

func (m *SubagentManager) runAgentLoop(ctx context.Context, taskID, sessionID, prompt string) {
	req := TaskRequest{
		TaskID:       taskID,
		Prompt:       prompt,
		SubagentType: AgentTypeResearch,
	}
	result, err := m.runTaskViaLoop(ctx, req, DefaultAgentProfile(AgentTypeResearch), nil)
	if err != nil {
		result = &SubagentResult{
			TaskID:    taskID,
			Prompt:    prompt,
			Status:    "failed",
			Error:     err.Error(),
			StartTime: time.Now(),
			EndTime:   time.Now(),
		}
	}

	m.runningTasks.Delete(taskID)
	ts := m.getOrCreateTaskSet(sessionID)
	ts.remove(taskID)

	select {
	case m.results <- *result:
	default:
	}
}

// runTaskViaLoop builds the profile's restricted tool registry and executes the
// child through the injected, Policy+Confirmer-protected agent.Loop.
func (m *SubagentManager) runTaskViaLoop(ctx context.Context, req TaskRequest, profile AgentProfile, resumeHistory []llm.Message) (*SubagentResult, error) {
	startTime := time.Now()
	result := &SubagentResult{
		TaskID:      req.TaskID,
		Description: req.Description,
		Prompt:      req.Prompt,
		AgentType:   profile.Type,
		Status:      "running",
		StartTime:   startTime,
		Metadata: map[string]any{
			"profile": profile.Name,
			"loop":    "agent.Loop",
		},
	}
	defer func() { result.EndTime = time.Now() }()
	if err := ctx.Err(); err != nil {
		result.Status = "cancelled"
		result.Error = err.Error()
		return result, nil
	}

	subagentTools := m.createSubagentToolsForProfile(profile)
	messages := m.buildChildMessages(profile, req, resumeHistory)
	loop := m.options.LoopFactory(LoopRequest{
		Profile:      profile,
		Tools:        subagentTools,
		MaxSteps:     req.MaxSteps,
		TaskID:       req.TaskID,
		WorktreePath: req.WorktreePath,
		RepoRoot:     req.RepoRoot,
	})
	if loop == nil {
		result.Status = "failed"
		result.Error = "subagent LoopFactory returned nil"
		return result, fmt.Errorf("%s", result.Error)
	}
	if input, ok := m.taskInputs.Load(req.TaskID); ok {
		loop.SetSteering(input.(chan string))
	}
	runResult, err := loop.RunMessagesWithEvents(ctx, messages, m.childEventSink(req, profile))
	result.Iterations = countAssistantMessages(runResult)
	if runResult != nil {
		result.FinalAnswer = strings.TrimSpace(runResult.FinalAnswer)
		// Keep the child's full message history for a future resume_task_id
		// (Phase 4). It is not part of the model-facing task result.
		result.Messages = runResult.Messages
	}
	validationErr := validateUsableFinalAnswer(result.FinalAnswer)
	switch {
	case ctx.Err() != nil:
		result.Status = "cancelled"
		result.Error = ctx.Err().Error()
	case err != nil:
		result.Status = "failed"
		result.Error = err.Error()
	case validationErr != nil:
		result.Status = "failed"
		result.Error = validationErr.Error()
	default:
		result.Status = "completed"
	}
	return result, nil
}

// childEventSink adapts the child agent.Loop's agent.Event stream onto the
// subagent event vocabulary, so the TUI still renders the child's tool stream
// sourced from inside the shared Loop:
// one "running" EventToolCall when the model requests a tool, one "done"/"failed"
// EventToolCall after it executes. Lifecycle start/finish are owned by RunTask
// (EventStarted/EventFinished), so the child's own agent_started/finished events
// are intentionally not forwarded (avoids duplicate frames).
func (m *SubagentManager) childEventSink(req TaskRequest, profile AgentProfile) agent.EventSink {
	return func(ae agent.Event) {
		switch ae.Type {
		case agent.EventModelChunk, agent.EventReasoningChunk:
			kind := "assistant"
			if ae.Type == agent.EventReasoningChunk {
				kind = "reasoning"
			}
			m.emit(Event{Type: EventTranscriptChunk, TaskID: req.TaskID, Generation: req.Generation, Description: req.Description, AgentType: profile.Type, TranscriptKind: kind, Message: ae.Message})
		case agent.EventToolRequested:
			m.emit(Event{
				Type:        EventToolCall,
				TaskID:      req.TaskID,
				Description: req.Description,
				AgentType:   profile.Type,
				Generation:  req.Generation,
				ToolName:    ae.ToolName,
				ToolInput:   ae.Input,
				ToolStatus:  "running",
				Message:     "tool: " + ae.ToolName,
			})
		case agent.EventToolResult:
			status := "done"
			if strings.TrimSpace(ae.Error) != "" {
				status = "failed"
			}
			m.emit(Event{
				Type:        EventToolCall,
				TaskID:      req.TaskID,
				Description: req.Description,
				AgentType:   profile.Type,
				Generation:  req.Generation,
				ToolName:    ae.ToolName,
				ToolInput:   ae.Input,
				ToolStatus:  status,
				ToolOutput:  ae.Output,
				ToolError:   ae.Error,
				Message:     ae.ToolName + " " + status,
			})
		}
	}
}

// countAssistantMessages derives the iteration count (one per model turn) from
// the child Loop's final message history.
func countAssistantMessages(res *agent.RunResult) int {
	if res == nil {
		return 0
	}
	n := 0
	for _, msg := range res.Messages {
		if msg.Role == llm.RoleAssistant {
			n++
		}
	}
	return n
}

func taskUserPrompt(req TaskRequest) string {
	if req.Description == "" {
		return req.Prompt
	}
	return strings.TrimSpace("Task: " + req.Description + "\n\nPrompt:\n" + req.Prompt)
}

// buildChildMessages assembles the initial message list for a child run. A fresh
// task is [system, user-prompt]. A resumed task (resumeHistory from a prior child)
// re-asserts the profile's system prompt, replays the prior conversation (minus
// its leading system message, which we replace), then appends the new user turn
// — so the child picks up exactly where it left off. The loop's contextx governor
// governs the replayed history like any other long context.
func (m *SubagentManager) buildChildMessages(profile AgentProfile, req TaskRequest, resumeHistory []llm.Message) []llm.Message {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: profile.SystemPrompt},
	}
	history := resumeHistory
	for len(history) > 0 && history[0].Role == llm.RoleSystem {
		history = history[1:]
	}
	messages = append(messages, history...)
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: taskUserPrompt(req)})
	return messages
}

// loadResumable returns the saved child session for taskID, if any.
func (m *SubagentManager) loadResumable(taskID string) (*resumableSession, bool) {
	val, ok := m.resumable.Load(taskID)
	if !ok {
		return nil, false
	}
	return val.(*resumableSession), true
}

// saveResumable stores a child's session so a later resume_task_id can continue it.
func (m *SubagentManager) saveResumable(taskID string, sess resumableSession) {
	if strings.TrimSpace(taskID) == "" {
		return
	}
	m.resumable.Store(taskID, &sess)
}

func eventFromResult(result *SubagentResult) Event {
	if result == nil {
		return Event{Type: EventFailed, Status: "failed", Error: "subagent returned no result"}
	}
	eventType := EventFinished
	if result.Status != "completed" {
		eventType = EventFailed
	}
	return Event{
		Type:        eventType,
		TaskID:      result.TaskID,
		Description: result.Description,
		AgentType:   result.AgentType,
		Status:      result.Status,
		Message:     strings.TrimSpace(result.FinalAnswer),
		Error:       result.Error,
		Iterations:  result.Iterations,
	}
}

func capString(value string, maxChars int, marker string) string {
	value = strings.TrimSpace(value)
	if maxChars <= 0 || len(value) <= maxChars {
		return value
	}
	return value[:maxChars] + "\n" + marker
}

// excludedTools is the denylist every child agent inherits on top of its profile
// allowlist. Memory and todo persistence are main-agent-only concerns (a child
// must not mutate the user's durable memory or session plan), and recursive
// delegation / cross-agent messaging are blocked to keep the delegation tree
// bounded. The list tracks the consolidated model surface (memory_search /
// memory_save / todo_read / todo_write), not the legacy CRUD names that are no
// longer registered for the main agent.
var excludedTools = map[string]bool{
	tool.Task:         true,
	tool.Spawn:        true,
	"message":         true,
	tool.MemorySearch: true,
	tool.MemorySave:   true,
	tool.TodoRead:     true,
	tool.TodoWrite:    true,
}

func (m *SubagentManager) createSubagentToolsForProfile(profile AgentProfile) *tool.Registry {
	subRegistry := tool.NewRegistry()
	if m.registry == nil {
		return subRegistry
	}

	for _, name := range m.registry.Names() {
		if excludedTools[name] {
			continue
		}
		if !m.options.AllowMCPTools && (strings.HasPrefix(name, "mcp_") || strings.HasPrefix(name, "mcp__")) {
			continue
		}
		if !m.options.AllowRecursiveSubtasks && (name == tool.Task || name == tool.Spawn) {
			continue
		}
		if profile.AllowedTools != nil && !profile.AllowedTools[name] {
			continue
		}
		t, ok := m.registry.Get(name)
		if !ok {
			continue
		}
		if !m.options.AllowHighRiskTools && t.Risk(json.RawMessage(`{}`)) != tool.RiskLow {
			// Coder-style profiles with CanWriteFiles still get write_file/edit_file
			// even though they are medium-risk; other medium/high-risk tools
			// stay blocked so subagents can't shell out autonomously.
			if !(profile.CanWriteFiles && (name == tool.WriteFile || name == tool.EditFile)) {
				continue
			}
		}
		_ = subRegistry.Register(t)
	}

	return subRegistry
}

// ErrTaskNotRunning indicates that interactive input cannot be delivered because the task has no live loop.
var ErrTaskNotRunning = errors.New("subagent task is not running")

// SendTaskInput queues input for injection at the next agent.Loop structural step boundary.
func (m *SubagentManager) SendTaskInput(ctx context.Context, taskID, input string) error {
	value, ok := m.taskInputs.Load(taskID)
	if !ok {
		return ErrTaskNotRunning
	}
	ch := value.(chan string)
	select {
	case ch <- input:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
