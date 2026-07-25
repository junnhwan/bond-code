package subagent

import (
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/tool"
)

type AgentType string

const (
	AgentTypeResearch AgentType = "research"
	AgentTypeCoder    AgentType = "coder"
	AgentTypeReviewer AgentType = "reviewer"
)

type AgentProfile struct {
	Type        AgentType
	Name        string
	Description string
	// WhenToUse is the one-line "what this profile is for" surfaced to the main
	// agent in the task tool's description, so it knows which subagent_type to
	// pick. Mirrors Claude Code's AgentTool agent listing (`- type: whenToUse`).
	WhenToUse     string
	SystemPrompt  string
	AllowedTools  map[string]bool
	MaxSteps      int
	CanWriteFiles bool
}

type TaskRequest struct {
	// SessionID owns resumable child context. A resume_task_id is valid only
	// when it was saved by a task in the same parent session.
	SessionID    string
	Description  string
	Prompt       string
	SubagentType AgentType
	TaskID       string
	Generation   uint64
	// MaxSteps optionally overrides the profile's step budget for this task.
	// Zero or negative means "use the profile default". Callers that delegate
	// open-ended work (e.g. batch/parallel work) set this higher so a subagent has
	// room to gather evidence before the forced wrap-up.
	MaxSteps int
	// ResumeTaskID (Phase 4) continues a prior child agent's context. When set,
	// RunTask looks up the child's saved message history by this id and runs the
	// new prompt as a continuation of that conversation (same profile), instead
	// of starting a fresh child. Unknown id => protocol-safe error envelope.
	// Empty => a normal fresh delegation.
	ResumeTaskID string
}

type TaskMode string

const (
	TaskModeSingle   TaskMode = "single"
	TaskModeParallel TaskMode = "parallel"
	TaskModeChain    TaskMode = "chain"
)

type BatchRequest struct {
	Mode  TaskMode
	Tasks []TaskRequest
}

type BatchResult struct {
	Mode    TaskMode
	Results []SubagentResult
	Status  string
	Error   string
}

// LoopRequest 描述构造一个子 agent Loop 所需的全部上下文。app 层的 LoopFactory
// 据此构造一个复用主 agent.Loop 基础设施（client/policy/confirmer + child-scoped
// contextx）的子 Loop，让 subagent 节点的工具执行与主 agent 走同一条
// Policy+Confirmer 安全边界。
type LoopRequest struct {
	Profile AgentProfile
	Tools   *tool.Registry
	// MaxSteps overrides the profile step budget for this task. Zero/negative
	// means "use Profile.MaxSteps".
	MaxSteps int
	// TaskID scopes the child contextx spill/summary directory so a child's large
	// tool results never pollute the main session audit.
	TaskID string
}

// LoopFactory 构造一个子 agent.Loop。SubagentManager 对缺失或返回 nil 的工厂
// 采用 fail-closed 策略，确保所有子 agent 都复用主 Loop 的安全边界。
type LoopFactory func(req LoopRequest) *agent.Loop

type ManagerOptions struct {
	MaxChildrenPerTurn     int
	MaxDepth               int
	DefaultTimeoutSeconds  int
	AllowMCPTools          bool
	AllowHighRiskTools     bool
	AllowRecursiveSubtasks bool
	EventSink              EventSink
	MaxResultChars         int
	// LoopFactory is required for child execution and must return a real
	// agent.Loop wired with the shared policy/confirmer and child context.
	LoopFactory LoopFactory
}

func DefaultManagerOptions() ManagerOptions {
	return ManagerOptions{
		MaxChildrenPerTurn:    3,
		MaxDepth:              1,
		DefaultTimeoutSeconds: 600,
		MaxResultChars:        12000,
	}
}

func DefaultAgentProfile(agentType AgentType) AgentProfile {
	baseReadTools := map[string]bool{
		tool.ReadFile:   true,
		tool.ListDir:    true,
		tool.SearchText: true,
	}
	switch agentType {
	case "", AgentTypeResearch:
		return AgentProfile{
			Type:        AgentTypeResearch,
			Name:        "Research",
			Description: "Read-only exploration and evidence gathering.",
			WhenToUse:   "Read-only exploration: locate where a feature is implemented, map dependencies, or answer 'how does X work' across many files. Use when you'd otherwise read a lot of files yourself to understand something.",
			SystemPrompt: strings.Join([]string{
				"You are a research subagent running in isolated context.",
				"Inspect only the evidence needed for the delegated question.",
				"Return a concise summary with file/path evidence when available.",
				"Do not modify files, memory, todos, or launch other agents.",
			}, "\n"),
			AllowedTools: baseReadTools,
			MaxSteps:     6,
		}
	case AgentTypeCoder:
		tools := copyToolSet(baseReadTools)
		tools[tool.WriteFile] = true
		tools[tool.EditFile] = true
		tools[tool.RunCommand] = true
		return AgentProfile{
			Type:        AgentTypeCoder,
			Name:        "Coder",
			Description: "Bounded implementation: edits existing files (edit_file) or creates new ones (write_file) to implement the delegated change, then verifies via run_command (e.g. go test / go build).",
			WhenToUse:   "A bounded change you can specify precisely — exact files/lines and what to edit. The child edits existing files or creates new ones and verifies with run_command. Hand it paths and concrete diff intent, not an open-ended goal.",
			SystemPrompt: strings.Join([]string{
				"You are a coder subagent running in isolated context.",
				"Implement the delegated change: use the edit_file tool (old_string -> new_string) to change existing files, or write_file to create new ones.",
				"Prefer edit_file over write_file for changes to existing files.",
				"After editing, verify with run_command (go test, go build, or the project's test command) when relevant, and report the result.",
				"Return a concise summary of what you changed plus the verification outcome.",
				"Do not modify memory, todos, or launch other agents.",
			}, "\n"),
			AllowedTools:  tools,
			MaxSteps:      8,
			CanWriteFiles: true,
		}
	case AgentTypeReviewer:
		tools := copyToolSet(baseReadTools)
		tools[tool.RunCommand] = true
		return AgentProfile{
			Type:        AgentTypeReviewer,
			Name:        "Reviewer",
			Description: "Read-only review with verification commands via run_command.",
			WhenToUse:   "Independent read-only review of a concrete change or design — bugs, risks, missing verification — citing file or command evidence. Use for a second pass over work you've done or are about to do.",
			SystemPrompt: strings.Join([]string{
				"You are a reviewer subagent running in isolated context.",
				"Find concrete bugs, risks, and missing verification.",
				"Lead with findings and cite files or command evidence.",
				"Use run_command for tests/builds when needed; do not modify files.",
				"Do not modify files, memory, todos, or launch other agents.",
			}, "\n"),
			AllowedTools: tools,
			MaxSteps:     8,
		}
	default:
		return AgentProfile{}
	}
}

func ValidateAgentType(agentType AgentType) error {
	switch agentType {
	case "", AgentTypeResearch, AgentTypeCoder, AgentTypeReviewer:
		return nil
	default:
		return fmt.Errorf("unsupported subagent_type %q", agentType)
	}
}

// AgentTypeListing renders the profile catalogue as "- <type>: <whenToUse>"
// lines for the task tool's description, mirroring Claude Code's AgentTool
// agent listing. Sourced from DefaultAgentProfile so the whenToUse text lives
// in exactly one place (the profile definitions) and never drifts from what
// the runtime actually instantiates.
func AgentTypeListing() string {
	var b strings.Builder
	for _, at := range []AgentType{AgentTypeResearch, AgentTypeCoder, AgentTypeReviewer} {
		p := DefaultAgentProfile(at)
		fmt.Fprintf(&b, "- %s: %s\n", at, p.WhenToUse)
	}
	return b.String()
}

func copyToolSet(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for name, allowed := range in {
		out[name] = allowed
	}
	return out
}
