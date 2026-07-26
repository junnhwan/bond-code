package app

import (
	"errors"
	"sync"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/agenttask"
	"github.com/junnhwan/bond-code/internal/ask"
	"github.com/junnhwan/bond-code/internal/collaboration"
	execution "github.com/junnhwan/bond-code/internal/collaboration/backend"
	"github.com/junnhwan/bond-code/internal/collaboration/backendipc"
	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/mcp"
	"github.com/junnhwan/bond-code/internal/memory"
	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/session"
	"github.com/junnhwan/bond-code/internal/skill"
	"github.com/junnhwan/bond-code/internal/subagent"
	"github.com/junnhwan/bond-code/internal/todo"
	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/junnhwan/bond-code/internal/trust"
)

type App struct {
	Config    *config.Config
	Tools     *tool.Registry
	Sessions  *session.JSONLStore
	SessionID string
	Policy    safety.Policy
	Confirmer safety.Confirmer
	// RuleSource caches Phase 5A "Allow always" grants for Policy.Decide and
	// shares them with the TUI confirmation panel. Reset on session switch so a
	// grant never leaks across sessions.
	RuleSource *session.RuleSource
	Questioner ask.Questioner
	LLM        llm.Client
	Agent      *agent.Loop

	ContextManager    *contextx.Manager
	ContextSummary    *contextx.SummaryStore
	MaxContextTokens  int
	MemoryStore       *memory.MemoryStore
	MemoryMaxChars    int
	MemoryExtractor   *MemoryExtractor
	SkillLoader       *skill.Loader
	SkillMaxChars     int
	TaskStore         *todo.TaskStore
	SubagentManager   *subagent.SubagentManager
	AgentTasks        *agenttask.Service
	Collaboration     *collaboration.Store
	ExecutionBackends *execution.Registry
	BackendSupervisor *backendipc.Supervisor
	MCPManager        *mcp.ProcessManager
	TrustManager      *trust.Manager

	RuntimePromptContext agent.RuntimePromptContext
	// OpenSessionPickerOnStart asks the TUI to open the session manager on
	// cold start (bare `bondcode --resume`). False for normal launches.
	OpenSessionPickerOnStart bool
	// planMode puts the agent into read-only planning posture: the system
	// prompt asks for a plan and mutating tools are disabled at the loop level.
	planMode bool
	// debugLogger is the opt-in per-session debug trace; nil when --debug is off.
	// The factory stages a new logger before each session commit so trace identity
	// cannot drift from SessionID. Both are nil when tracing is disabled.
	debugLogger        observe.Logger
	debugLoggerFactory debugLoggerFactory

	mu                sync.Mutex
	history           []llm.Message
	historyWarning    string
	subagentMu        sync.Mutex
	subagentEvents    []bufferedSubagentEvent
	liveSink          agent.EventSink
	subagentTasks     map[string]subagentRuntimeState
	subagentLatest    string
	measuredMu        sync.Mutex
	lastInputTokens   int
	lastOutputTokens  int
	totalInputTokens  int
	totalOutputTokens int
	measuredCalls     int
}

// Close releases resources held by the app: debug trace logger, collaboration
// supervisor, and MCP server processes. Safe to call when those are unset.
func (a *App) Close() error {
	var closeErrors []error
	if a.debugLogger != nil {
		closeErrors = append(closeErrors, a.debugLogger.Close())
	}
	if a.BackendSupervisor != nil {
		closeErrors = append(closeErrors, a.BackendSupervisor.Close())
	}
	if a.MCPManager != nil {
		closeErrors = append(closeErrors, a.MCPManager.Close())
	}
	return errors.Join(closeErrors...)
}
