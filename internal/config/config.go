package config

import "github.com/junnhwan/bond-code/internal/safety"

type Config struct {
	Model         ModelConfig         `yaml:"model"`
	Session       SessionConfig       `yaml:"session"`
	Safety        SafetyConfig        `yaml:"safety"`
	Agent         AgentConfig         `yaml:"agent"`
	Context       ContextConfig       `yaml:"context"`
	Memory        MemoryConfig        `yaml:"memory"`
	Planning      PlanningConfig      `yaml:"planning"`
	Subagent      SubagentConfig      `yaml:"subagent"`
	Collaboration CollaborationConfig `yaml:"collaboration"`
	Skills        SkillsConfig        `yaml:"skills"`
	MCP           MCPConfig           `yaml:"mcp"`
	TUI           TUIConfig           `yaml:"tui"`
	Tools         ToolsConfig         `yaml:"tools"`
}

type ModelConfig struct {
	Provider                 string         `yaml:"provider"`
	BaseURL                  string         `yaml:"base_url"`
	APIKeyEnv                string         `yaml:"api_key_env"`
	Model                    string         `yaml:"model"`
	Temperature              float32        `yaml:"temperature"`
	MaxTokens                int            `yaml:"max_tokens"`
	StreamIdleTimeoutSeconds int            `yaml:"stream_idle_timeout_seconds"`
	Thinking                 ThinkingConfig `yaml:"thinking"`
	Retry                    RetryConfig    `yaml:"retry"`
	// PromptCache enables Anthropic prompt-caching breakpoints (cache_control:
	// ephemeral) on the system prompt and tool definitions. Off by default so
	// providers that don't honor cache_control are unaffected; enable for caches
	// that do (Anthropic and most Anthropic-compatible gateways). Breakpoints on
	// the system prompt only pay off once volatile sections (memory body, context
	// summary) are moved out of it — see roadmap A2/A3.
	PromptCache bool `yaml:"prompt_cache"`
}

// RetryConfig 控制 LLM 客户端的传输层韧性：对 429/5xx/网络错误做有界指数退避重试，
// 并在持续过载时回退到备份模型。零值（Enabled=false）保留旧的 fail-fast 行为。
type RetryConfig struct {
	Enabled                   bool     `yaml:"enabled"`
	MaxAttempts               int      `yaml:"max_attempts"`
	BaseBackoffMs             int      `yaml:"base_backoff_ms"`
	MaxBackoffMs              int      `yaml:"max_backoff_ms"`
	OverloadFallbackThreshold int      `yaml:"overload_fallback_threshold"`
	FallbackModels            []string `yaml:"fallback_models"`
}

// ThinkingConfig toggles the provider's extended-thinking / reasoning stream.
// glm-5.x's Anthropic-compatible endpoint speaks the standard "thinking_delta"
// protocol: enabling this makes the model emit reasoning the TUI shows as a
// folded preview. Disabled by default so behavior is unchanged for providers
// that do not support it.
type ThinkingConfig struct {
	Enabled      bool `yaml:"enabled"`
	BudgetTokens int  `yaml:"budget_tokens"`
}

type SessionConfig struct {
	Dir string `yaml:"dir"`
}

type SafetyConfig struct {
	PermissionMode      safety.PermissionMode   `yaml:"permission_mode"`
	EnableBypass        bool                    `yaml:"enable_bypass"`
	RequireConfirmation bool                    `yaml:"require_confirmation"`
	BlockedCommands     []string                `yaml:"blocked_commands"`
	Rules               []safety.PermissionRule `yaml:"rules"`
}

type AgentConfig struct {
	MaxSteps                  int `yaml:"max_steps"`
	MaxRepeatedToolCalls      int `yaml:"max_repeated_tool_calls"`
	MaxToolCallsPerStep       int `yaml:"max_tool_calls_per_step"`
	MaxRepeatedTextChunks     int `yaml:"max_repeated_text_chunks"`
	MaxRepeatedTextSubstrings int `yaml:"max_repeated_text_substrings"`
}

type ContextConfig struct {
	MaxTokens              int  `yaml:"max_tokens"`
	ReserveTokens          int  `yaml:"reserve_tokens"`
	KeepRecentTokens       int  `yaml:"keep_recent_tokens"`
	AutoCompact            bool `yaml:"auto_compact"`
	MicroCompactKeepRecent int  `yaml:"micro_compact_keep_recent"`
	MicroCompactMinChars   int  `yaml:"micro_compact_min_chars"`
	ToolResultBudget       int  `yaml:"tool_result_budget"`
	ToolResultPreviewChars int  `yaml:"tool_result_preview_chars"`
	ToolResultTurnBudget   int  `yaml:"tool_result_turn_budget"`
	Enabled                bool `yaml:"enabled"`
	enabledSet             bool
	autoCompactSet         bool
}

// AutoCompactExplicitlySet reports whether auto_compact appeared in YAML.
func (c ContextConfig) AutoCompactExplicitlySet() bool {
	return c.autoCompactSet
}

// MemoryConfig controls the Claude Code-style file-based auto-memory (memdir).
// Topic files live under the project data dir's memory/ with MEMORY.md as index.
type MemoryConfig struct {
	Enabled  bool `yaml:"enabled"`
	MaxChars int  `yaml:"max_chars"`
	// MaxRelevant caps per-turn topic memories injected into the user reminder
	// (Claude Code findRelevantMemories default is 5).
	MaxRelevant int `yaml:"max_relevant"`
	// AutoExtract enables background extraction after each turn when the main
	// agent did not call memory_save. Off by default (API cost); enable via
	// memory.auto_extract or BONDCODE_MEMORY_EXTRACT.
	AutoExtract bool `yaml:"auto_extract"`
	// LLMSelect uses a side-query over memory frontmatter to pick relevant
	// topic files (falls back to keyword search). Off by default; enable via
	// memory.llm_select or BONDCODE_MEMORY_LLM_SELECT.
	LLMSelect bool `yaml:"llm_select"`
	// DreamEnabled turns on the consolidation pass when topic file count
	// exceeds DreamThreshold. Off by default; enable via memory.dream_enabled
	// or BONDCODE_MEMORY_DREAM.
	DreamEnabled   bool `yaml:"dream_enabled"`
	DreamThreshold int  `yaml:"dream_threshold"`
}

type PlanningConfig struct {
	Enabled     bool `yaml:"enabled"`
	InjectTasks bool `yaml:"inject_tasks"`
}

type SubagentConfig struct {
	Enabled               bool `yaml:"enabled"`
	MaxChildrenPerTurn    int  `yaml:"max_children_per_turn"`
	MaxDepth              int  `yaml:"max_depth"`
	DefaultTimeoutSeconds int  `yaml:"default_timeout_seconds"`
	EnableSpawn           bool `yaml:"enable_spawn"`
}

// CollaborationConfig gates the Multi-Agent collaboration surface:
// agent_task lifecycle tools, team_*, mailbox_*, and optional terminal backends.
// Default is on (product keeps Multi-Agent available out of the box). Use a
// pointer so an explicit collaboration.enabled: false is distinguishable from
// an omitted section. Synchronous `task` subagent is controlled by
// SubagentConfig, not this flag.
type CollaborationConfig struct {
	Enabled *bool `yaml:"enabled"`
}

// IsEnabled resolves collaboration.enabled with its default (true). An explicit
// collaboration.enabled: false opts out of agent_task/team/mailbox wiring.
func (c CollaborationConfig) IsEnabled() bool {
	if c.Enabled != nil {
		return *c.Enabled
	}
	return true
}

// SkillsConfig controls Claude Code–style local skills (name/SKILL.md).
// Discovery roots (always BondCode-owned paths):
//   - ~/.bondcode/skills  (or $BONDCODE_HOME/skills)
//   - <project>/.bondcode/skills
//   - optional skills.root extra directory
type SkillsConfig struct {
	Enabled bool `yaml:"enabled"`
	// Root is an optional extra skills directory (absolute, or relative to project).
	// Default discovery does not require this field.
	Root string `yaml:"root"`
	// MaxChars caps expanded skill body returned by the skill tool.
	MaxChars int `yaml:"max_chars"`
	// ListingBudgetChars caps the Available Skills listing in the dynamic reminder.
	// 0 uses the package default (8000, ~1% of a 200k context).
	ListingBudgetChars int `yaml:"listing_budget_chars"`
}

type MCPConfig struct {
	Enabled                 bool              `yaml:"enabled"`
	InjectTools             bool              `yaml:"inject_tools"`
	NamespaceTools          bool              `yaml:"namespace_tools"`
	CallTimeoutSeconds      int               `yaml:"call_timeout_seconds"`
	ReconnectBackoffSeconds int               `yaml:"reconnect_backoff_seconds"`
	Servers                 []MCPServerConfig `yaml:"servers"`
}

type MCPServerConfig struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Enabled bool     `yaml:"enabled"`
}

// TUIConfig configures terminal UI behavior. All fields are optional with
// sensible defaults so an older bondcode.yaml without a `tui:` section still
// works unchanged.
type TUIConfig struct {
	// MouseCapture enables mouse tracking (wheel scroll / click / hover).
	// Defaults to true so the mouse wheel scrolls the transcript. Set false
	// (or /mouse off) for free terminal drag-select/copy; with capture on,
	// most terminals still allow Shift+drag to select.
	MouseCapture *bool `yaml:"mouse_capture"`
	// StreamingMarkdownThreshold is the assistant body length above which a
	// streaming block skips glamour and appends raw text. Defaults to 2000.
	StreamingMarkdownThreshold int `yaml:"streaming_markdown_threshold"`
	// Accent names an accent-color preset for the TUI (peach/blue/green/amber/
	// magenta/cyan). Empty or unrecognized falls back to the default (peach). A
	// runtime /theme choice overrides this and is persisted in
	// tui-preferences.json, so this is the startup default only.
	Accent string `yaml:"accent"`
}

// Mouse resolves the mouse-capture flag with its default (true).
// On by default so wheel scroll reaches the TUI; turn off with mouse_capture:
// false or /mouse when you need free drag-select without holding Shift.
func (c TUIConfig) Mouse() bool {
	if c.MouseCapture != nil {
		return *c.MouseCapture
	}
	return true
}

// MarkdownThreshold resolves the streaming markdown threshold with its default.
func (c TUIConfig) MarkdownThreshold() int {
	if c.StreamingMarkdownThreshold > 0 {
		return c.StreamingMarkdownThreshold
	}
	return 2000
}

// ToolsConfig controls which tools the main agent exposes to the model. The
// model-facing surface is a fixed core set of coding primitives plus runtime
// meta tools; thin git/go wrappers are not registered (use run_command).
type ToolsConfig struct {
	// CoreOnly is retained for config compatibility. The model surface is already
	// consolidated (todo is TodoWrite-only); false no longer re-enables deleted
	// todo CRUD tools. Pointer so default true is distinguishable from explicit false.
	CoreOnly *bool `yaml:"core_only"`
}

// IsCoreOnly resolves the core-only flag with its default (true).
func (c ToolsConfig) IsCoreOnly() bool {
	if c.CoreOnly != nil {
		return *c.CoreOnly
	}
	return true
}
