package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
)

func loadBootstrapConfig(explicitPath string) (*config.Config, error) {
	cfg := defaultConfig()
	if path := resolveConfigPath(explicitPath); path != "" {
		loaded, err := config.Load(path)
		if err != nil {
			return nil, err
		}
		cfg = loaded
	}
	applyConfigDefaults(cfg)
	applyEnv(cfg)
	return cfg, nil
}

func selectBootstrapClient(cfg config.ModelConfig, opts Options) llm.Client {
	client := buildModelClient(cfg)
	if opts.UseFakeLLM {
		client = llm.NewFakeClient([]llm.Chunk{{Content: "hello from fake llm", Done: true}})
	}
	if opts.Client != nil {
		client = opts.Client
	}
	return client
}

func defaultConfig() *config.Config {
	return &config.Config{
		Model: config.ModelConfig{
			Provider:    "anthropic-compatible",
			BaseURL:     "",
			APIKeyEnv:   "BONDCODE_API_KEY",
			Model:       "",
			Temperature: 0.2,
			MaxTokens:   4096,
		},
		Session: config.SessionConfig{},
		Safety: config.SafetyConfig{
			PermissionMode:      safety.ModeDefault,
			RequireConfirmation: true,
			BlockedCommands:     []string{"rm -rf /", "git push --force", "curl | sh"},
		},
		Agent:         defaultAgentConfig(),
		Context:       defaultContextConfig(),
		Memory:        defaultMemoryConfig(),
		Planning:      defaultPlanningConfig(),
		Subagent:      defaultSubagentConfig(),
		Collaboration: defaultCollaborationConfig(),
		Skills:        defaultSkillsConfig(),
		MCP:           defaultMCPConfig(),
		Tools:         defaultToolsConfig(),
	}
}

func defaultToolsConfig() config.ToolsConfig {
	coreOnly := true
	return config.ToolsConfig{
		CoreOnly: &coreOnly,
	}
}

func defaultAgentConfig() config.AgentConfig {
	return config.AgentConfig{
		MaxSteps:                  200,
		MaxRepeatedToolCalls:      3,
		MaxToolCallsPerStep:       8,
		MaxRepeatedTextChunks:     16,
		MaxRepeatedTextSubstrings: 3,
	}
}

func defaultContextConfig() config.ContextConfig {
	return config.ContextConfig{
		Enabled:                true,
		MaxTokens:              100000,
		MicroCompactKeepRecent: 10,
		MicroCompactMinChars:   500,
		ToolResultBudget:       8000,
		ToolResultPreviewChars: 2000,
		ToolResultTurnBudget:   16000,
	}
}

func defaultMemoryConfig() config.MemoryConfig {
	return config.MemoryConfig{
		Enabled:        true,
		MaxChars:       4000,
		MaxRelevant:    5,
		DreamThreshold: 40,
	}
}

func defaultPlanningConfig() config.PlanningConfig {
	return config.PlanningConfig{
		Enabled:     true,
		InjectTasks: true,
	}
}

func defaultSubagentConfig() config.SubagentConfig {
	return config.SubagentConfig{
		Enabled:               true,
		MaxChildrenPerTurn:    3,
		MaxDepth:              1,
		DefaultTimeoutSeconds: 600,
	}
}

func defaultCollaborationConfig() config.CollaborationConfig {
	// On by default: Multi-Agent collaboration is part of the product surface.
	// Set collaboration.enabled: false to drop agent_task/team/mailbox tools.
	enabled := true
	return config.CollaborationConfig{Enabled: &enabled}
}

func defaultSkillsConfig() config.SkillsConfig {
	return config.SkillsConfig{
		Enabled:            true,
		MaxChars:           12000,
		ListingBudgetChars: 0, // package default (8000)
	}
}

func defaultMCPConfig() config.MCPConfig {
	return config.MCPConfig{
		Enabled:            false,
		InjectTools:        false,
		NamespaceTools:     true,
		CallTimeoutSeconds: 30,
	}
}

func applyConfigDefaults(cfg *config.Config) {
	if cfg.Session.Dir == "" {
		// Per-project data root under the BondCode home dir (mirrors Claude
		// Code's ~/.claude/projects/<encoded-cwd>), so running the agent from
		// any folder no longer drops sessions/memory/todos into the cwd.
		cfg.Session.Dir = DefaultProjectDataDir()
	}
	if cfg.Model.APIKeyEnv == "" {
		cfg.Model.APIKeyEnv = "BONDCODE_API_KEY"
	}
	// retry 段未配置（关键字段全零）时套用生产默认：429/5xx 指数退避重试。
	// RetryConfig 含 []string 切片不能用 == 整体比较，故按关键字段判零；
	// 用户在 yaml 显式配置过任一字段（含 enabled: false + max_attempts）则保留用户值。
	if r := cfg.Model.Retry; !r.Enabled && r.MaxAttempts == 0 && len(r.FallbackModels) == 0 {
		cfg.Model.Retry = config.RetryConfig{
			Enabled:                   true,
			MaxAttempts:               3,
			BaseBackoffMs:             1000,
			MaxBackoffMs:              8000,
			OverloadFallbackThreshold: 2,
		}
	}
	// rate_limit 段：省略时默认开启（父+子共享闸，避免多 Agent 打爆低 RPM 网关）。
	// 显式 enabled: false 关闭；显式 enabled: true 或缺省但写了其它字段则补齐默认。
	applyRateLimitDefaults(&cfg.Model.RateLimit)
	if cfg.Agent == (config.AgentConfig{}) {
		cfg.Agent = defaultAgentConfig()
	} else {
		if cfg.Agent.MaxSteps <= 0 {
			cfg.Agent.MaxSteps = 200
		}
		if cfg.Agent.MaxRepeatedToolCalls <= 0 {
			cfg.Agent.MaxRepeatedToolCalls = 3
		}
		if cfg.Agent.MaxToolCallsPerStep <= 0 {
			cfg.Agent.MaxToolCallsPerStep = 8
		}
		if cfg.Agent.MaxRepeatedTextChunks <= 0 {
			cfg.Agent.MaxRepeatedTextChunks = 16
		}
		if cfg.Agent.MaxRepeatedTextSubstrings <= 0 {
			cfg.Agent.MaxRepeatedTextSubstrings = 3
		}
	}
	if cfg.Context == (config.ContextConfig{}) {
		cfg.Context = defaultContextConfig()
	} else {
		// A non-empty context section means the user wants context management,
		// so default Enabled on. The bool zero value from a partial YAML section
		// (e.g. only max_tokens) would otherwise disable the governor — which also
		// stops EventContextUpdated, so the header's ctx % would never appear.
		if !cfg.Context.Enabled && !cfg.Context.EnabledExplicitlySet() {
			cfg.Context.Enabled = true
		}
		if cfg.Context.MaxTokens <= 0 {
			cfg.Context.MaxTokens = 100000
		}
	}
	if cfg.Context.MicroCompactKeepRecent <= 0 {
		cfg.Context.MicroCompactKeepRecent = 10
	}
	if cfg.Context.MicroCompactMinChars <= 0 {
		cfg.Context.MicroCompactMinChars = 500
	}
	if cfg.Context.ToolResultBudget <= 0 {
		cfg.Context.ToolResultBudget = 8000
	}
	if cfg.Context.ToolResultPreviewChars <= 0 {
		cfg.Context.ToolResultPreviewChars = 2000
	}
	if cfg.Context.ToolResultTurnBudget <= 0 {
		cfg.Context.ToolResultTurnBudget = 16000
	}
	if cfg.Memory == (config.MemoryConfig{}) {
		cfg.Memory = defaultMemoryConfig()
	} else {
		if cfg.Memory.MaxChars <= 0 {
			cfg.Memory.MaxChars = 4000
		}
		if cfg.Memory.MaxRelevant <= 0 {
			cfg.Memory.MaxRelevant = 5
		}
		if cfg.Memory.DreamThreshold <= 0 {
			cfg.Memory.DreamThreshold = 40
		}
	}
	if cfg.Planning == (config.PlanningConfig{}) {
		cfg.Planning = defaultPlanningConfig()
	}
	if cfg.Subagent == (config.SubagentConfig{}) {
		cfg.Subagent = defaultSubagentConfig()
	} else {
		if cfg.Subagent.MaxChildrenPerTurn <= 0 {
			cfg.Subagent.MaxChildrenPerTurn = 3
		}
		if cfg.Subagent.MaxDepth <= 0 {
			cfg.Subagent.MaxDepth = 1
		}
		if cfg.Subagent.DefaultTimeoutSeconds <= 0 {
			cfg.Subagent.DefaultTimeoutSeconds = 600
		}
	}
	if cfg.Skills == (config.SkillsConfig{}) {
		cfg.Skills = defaultSkillsConfig()
	} else {
		// Root is optional (extra dir). Empty means user+project discovery only.
		if cfg.Skills.MaxChars <= 0 {
			cfg.Skills.MaxChars = 12000
		}
	}
	if isZeroMCPConfig(cfg.MCP) {
		cfg.MCP = defaultMCPConfig()
	} else if !cfg.MCP.NamespaceTools && cfg.MCP.InjectTools {
		cfg.MCP.NamespaceTools = true
	}
	if cfg.MCP.CallTimeoutSeconds <= 0 {
		cfg.MCP.CallTimeoutSeconds = 30
	}
	// tools.core_only is a *bool that defaults to true via IsCoreOnly(), so no
	// assignment is needed here.
}

func isZeroMCPConfig(cfg config.MCPConfig) bool {
	return !cfg.Enabled && !cfg.InjectTools && !cfg.NamespaceTools && cfg.CallTimeoutSeconds == 0 && cfg.ReconnectBackoffSeconds == 0 && len(cfg.Servers) == 0
}

// applyRateLimitDefaults fills model.rate_limit when omitted. Explicit
// enabled: false keeps the gate off; otherwise missing fields get production
// defaults (max_concurrent=1, 60s 429 cooldown).
func applyRateLimitDefaults(rl *config.RateLimitConfig) {
	if rl == nil {
		return
	}
	if rl.Enabled != nil && !*rl.Enabled {
		return
	}
	on := true
	if rl.Enabled == nil {
		// Section omitted entirely, or only partial fields without enabled.
		// Turn on unless every field is still zero (same as omitted).
		if rl.MaxConcurrent == 0 && rl.MaxRequestsPerMinute == 0 && rl.CooldownOnRateLimitMs == 0 {
			rl.Enabled = &on
			rl.MaxConcurrent = 1
			rl.CooldownOnRateLimitMs = 60000
			return
		}
		rl.Enabled = &on
	}
	if rl.MaxConcurrent <= 0 {
		rl.MaxConcurrent = 1
	}
	if rl.CooldownOnRateLimitMs <= 0 {
		rl.CooldownOnRateLimitMs = 60000
	}
}

func applyEnv(cfg *config.Config) {
	if v := os.Getenv("BONDCODE_BASE_URL"); v != "" {
		cfg.Model.BaseURL = v
	}
	if v := os.Getenv("BONDCODE_MODEL"); v != "" {
		cfg.Model.Model = v
	}
	if cfg.Model.APIKeyEnv == "" {
		cfg.Model.APIKeyEnv = "BONDCODE_API_KEY"
	}
	// Extended thinking can be toggled without a config file, matching the
	// BONDCODE_* env-driven setup. Budget is optional (the llm layer raises it
	// to 4096 when below the 1024 protocol floor).
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("BONDCODE_THINKING_ENABLED"))); v == "1" || v == "true" || v == "yes" || v == "on" {
		cfg.Model.Thinking.Enabled = true
	}
	if v := os.Getenv("BONDCODE_THINKING_BUDGET"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			cfg.Model.Thinking.BudgetTokens = n
		}
	}
	// Prompt cache can be toggled without a config file, so it's easy to A/B
	// whether the target gateway honors cache_control (watch cache_read_input_tokens
	// in the usage stream). Off by default to avoid surprises on providers that
	// don't support it.
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("BONDCODE_PROMPT_CACHE"))); v == "1" || v == "true" || v == "yes" || v == "on" {
		cfg.Model.PromptCache = true
	}
	// Memory auto-extraction toggle (roadmap B1): opt-in, off by default so the
	// extra per-turn LLM pass only runs when explicitly enabled.
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("BONDCODE_MEMORY_EXTRACT"))); v == "1" || v == "true" || v == "yes" || v == "on" {
		cfg.Memory.AutoExtract = true
	}
	// Memory LLM selection toggle (roadmap B2): replaces keyword matching with
	// an LLM side-query when the memory pool is large. Opt-in, off by default.
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("BONDCODE_MEMORY_LLM_SELECT"))); v == "1" || v == "true" || v == "yes" || v == "on" {
		cfg.Memory.LLMSelect = true
	}
	// Memory "Dream" consolidation toggle (roadmap B3): LLM archives stale /
	// redundant / contradicted memories when the pool exceeds the threshold.
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("BONDCODE_MEMORY_DREAM"))); v == "1" || v == "true" || v == "yes" || v == "on" {
		cfg.Memory.DreamEnabled = true
	}
}

// resolveConfigPath picks the config file to load. An explicit --config path
// always wins; otherwise the first existing default location is used, so a
// project-local bondcode.yaml or a user-level ~/.bondcode/config.yaml is
// picked up automatically and `go run ./cmd/bondcode` just works without a
// per-invocation flag.
func resolveConfigPath(explicit string) string {
	return resolveConfigPathFrom(defaultConfigSearchPaths(), explicit)
}

func resolveConfigPathFrom(paths []string, explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	for _, candidate := range paths {
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

// defaultConfigSearchPaths is the ordered lookup list when no --config is
// given: project-local first (sharable per-repo), then the user-level global
// config (<bondcodeHome>/config.yaml) for cross-project settings like MCP
// servers.
func defaultConfigSearchPaths() []string {
	return []string{
		"bondcode.yaml",
		filepath.Join(bondcodeHome(), "config.yaml"),
	}
}

// bondcodeHome is the root for all user-level BondCode state — config plus
// per-project sessions, memory, todos and trust. It defaults to ~/.bondcode
// (mirroring Claude Code's ~/.claude); BONDCODE_HOME relocates the whole tree,
// which tests use to keep state out of the real home directory.
func bondcodeHome() string {
	if v := strings.TrimSpace(os.Getenv("BONDCODE_HOME")); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".bondcode")
	}
	return ".bondcode"
}

// DefaultProjectDataDir is the per-project data root: <bondcodeHome>/projects/
// <encoded-cwd>. Session audit logs, memory, todos, trust state, context
// summaries and spilled tool results all live beneath it, so a single relative
// default no longer litters the user's working directory.
func DefaultProjectDataDir() string {
	cwd, _ := os.Getwd()
	return defaultProjectDataDirFor(cwd)
}

func defaultProjectDataDirFor(cwd string) string {
	return filepath.Join(bondcodeHome(), "projects", encodeProjectDir(cwd))
}

// encodeProjectDir turns an absolute path into one filesystem-safe segment,
// matching Claude Code's ~/.claude/projects encoding (D:\a\b -> D--a-b).
func encodeProjectDir(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return "default"
	}
	return strings.NewReplacer(":", "-", "\\", "-", "/", "-").Replace(dir)
}

// ResolveSessionDir returns the effective session directory for a --config path
// and an optional explicit --dir override, using the same resolution as
// Bootstrap so the hidden `session` CLI subcommand reads where the agent writes.
func ResolveSessionDir(configPath, explicitDir string) string {
	if strings.TrimSpace(explicitDir) != "" {
		return explicitDir
	}
	cfg := defaultConfig()
	if path := resolveConfigPath(configPath); path != "" {
		if loaded, err := config.Load(path); err == nil {
			cfg = loaded
		}
	}
	applyConfigDefaults(cfg)
	return cfg.Session.Dir
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
