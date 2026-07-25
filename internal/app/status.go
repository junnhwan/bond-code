package app

import (
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/llm"
)

type RuntimeStatus struct {
	SessionID  string
	Model      string
	ToolCount  int
	Permission string
	Context    ContextStatus
	Usage      UsageStatus
	Memory     MemoryStatus
	Planning   PlanningStatus
	Subagents  SubagentStatus
	Skills     SkillsStatus
	MCP        MCPStatus
}

type ContextStatus struct {
	MaxTokens   int
	UsedTokens  int    // real input tokens from the model API (true occupancy); 0 until the first reply
	Stats       string // governor's estimated stats, when available
	Summary     string
	SummaryText string
	Breakdown   ContextBreakdown
}

type ContextBreakdown struct {
	SystemTokens       int
	ConversationTokens int
	ToolResultTokens   int
	SummaryTokens      int
}

type UsageStatus struct {
	ModelCalls        int
	LastInputTokens   int
	LastOutputTokens  int
	TotalInputTokens  int
	TotalOutputTokens int
}

type MemoryStatus struct {
	Enabled  bool
	MaxChars int
	Chars    int // MEMORY.md index size
	Topics   int // topic .md file count
	Error    string
}

type PlanningStatus struct {
	Enabled bool
	Summary string
	Error   string
}

type SubagentStatus struct {
	Enabled bool
	Active  int
	Latest  string
}

type SkillsStatus struct {
	Enabled bool
	Root    string
	Count   int
	Error   string
}

type MCPStatus struct {
	Enabled bool
	Servers int
	Tools   int
	Errors  int
}

func (a *App) StatusSnapshot() RuntimeStatus {
	used, _ := a.MeasuredUsage()
	snap := RuntimeStatus{
		SessionID: a.SessionID,
		Context:   ContextStatus{MaxTokens: a.MaxContextTokens, UsedTokens: used},
		Usage:     a.UsageSnapshot(),
	}
	snap.Context.Breakdown = a.contextBreakdown()
	if a.Config != nil {
		snap.Model = a.Config.Model.Model
		snap.Memory.Enabled = a.Config.Memory.Enabled
		snap.Memory.MaxChars = a.Config.Memory.MaxChars
		snap.Planning.Enabled = a.Config.Planning.Enabled
		snap.Subagents.Enabled = a.Config.Subagent.Enabled
		snap.Skills.Enabled = a.Config.Skills.Enabled
		snap.Skills.Root = a.Config.Skills.Root
		snap.MCP.Enabled = a.Config.MCP.Enabled
	}
	if a.Policy.RequireConfirmation {
		snap.Permission = "confirm"
	} else {
		snap.Permission = "auto"
	}
	if a.Tools != nil {
		snap.ToolCount = len(a.Tools.Names())
	}
	if a.ContextSummary != nil {
		artifact, err := a.ContextSummary.Load()
		if err != nil {
			snap.Context.Stats = "summary error: " + err.Error()
		} else if artifact != nil {
			snap.Context.Summary = artifact.CreatedAt.Format("2006-01-02 15:04:05Z")
			snap.Context.SummaryText = artifact.Summary
			snap.Context.Breakdown.SummaryTokens = contextEstimator(a).EstimateTokens(artifact.PromptSection(4000))
		}
	}
	if a.historyWarning != "" {
		appendContextStat(&snap.Context, a.historyWarning)
	}
	if a.MemoryStore != nil {
		memoryText, err := a.MemoryStore.GetMemoryContext(a.MemoryMaxChars)
		if err != nil {
			snap.Memory.Error = err.Error()
		} else {
			snap.Memory.Chars = len(memoryText)
		}
		n, err := a.MemoryStore.Count()
		if err != nil {
			snap.Memory.Error = err.Error()
		} else {
			snap.Memory.Topics = n
		}
	}
	if a.TaskStore != nil {
		summary, err := a.TaskStore.Summary()
		if err != nil {
			snap.Planning.Error = err.Error()
		} else {
			snap.Planning.Summary = summary
		}
	}
	a.subagentMu.Lock()
	snap.Subagents.Latest = a.subagentLatest
	for _, state := range a.subagentTasks {
		if state.active {
			snap.Subagents.Active++
		}
	}
	a.subagentMu.Unlock()
	if a.SkillLoader != nil {
		if snap.Skills.Root == "" {
			// Prefer project root; status may also show all roots later.
			snap.Skills.Root = a.SkillLoader.Root()
		}
		index, err := a.SkillLoader.IndexAll()
		if err != nil {
			snap.Skills.Error = err.Error()
		} else {
			snap.Skills.Count = len(index)
		}
	}
	if a.MCPManager != nil {
		for _, status := range a.MCPManager.Status() {
			snap.MCP.Servers++
			snap.MCP.Tools += status.ToolCount
			if status.LastError != "" {
				snap.MCP.Errors++
			}
		}
	}
	return snap
}

func (a *App) contextBreakdown() ContextBreakdown {
	var breakdown ContextBreakdown
	estimator := contextEstimator(a)
	breakdown.SystemTokens = estimator.EstimateTokens(agent.BuildSystemPrompt(a.RuntimePromptContext))
	for _, msg := range a.history {
		if msg.Role == llm.RoleTool {
			breakdown.ToolResultTokens += estimator.EstimateMessage(msg)
			continue
		}
		if msg.Role == llm.RoleSystem {
			breakdown.SystemTokens += estimator.EstimateMessage(msg)
			continue
		}
		breakdown.ConversationTokens += estimator.EstimateMessage(msg)
	}
	return breakdown
}

func contextEstimator(a *App) interface {
	EstimateTokens(string) int
	EstimateMessage(llm.Message) int
} {
	if a != nil && a.ContextManager != nil {
		return appContextEstimator{manager: a.ContextManager}
	}
	return defaultContextEstimator{}
}

type appContextEstimator struct {
	manager interface {
		EstimateTokens([]llm.Message) int
	}
}

func (e appContextEstimator) EstimateTokens(text string) int {
	return e.manager.EstimateTokens([]llm.Message{{Role: llm.RoleUser, Content: text}})
}

func (e appContextEstimator) EstimateMessage(msg llm.Message) int {
	return e.manager.EstimateTokens([]llm.Message{msg})
}

type defaultContextEstimator struct{}

func (defaultContextEstimator) EstimateTokens(text string) int {
	return len(text) / 3
}

func (defaultContextEstimator) EstimateMessage(msg llm.Message) int {
	total := len(msg.Content) / 3
	for _, tc := range msg.ToolCalls {
		total += len(tc.Name) / 3
		total += len(tc.Arguments) / 3
	}
	return total
}

func appendContextStat(status *ContextStatus, line string) {
	if status.Stats == "" {
		status.Stats = line
		return
	}
	status.Stats += "\n" + line
}
