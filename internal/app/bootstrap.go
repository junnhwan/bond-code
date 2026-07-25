package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/ask"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
	"github.com/junnhwan/bond-code/internal/trust"
)

type Options struct {
	ConfigPath string
	UseFakeLLM bool
	AutoYes    bool
	Confirmer  safety.Confirmer
	Questioner ask.Questioner
	Client     llm.Client
	// ResumeSessionID, when set, resumes a previous session: its message history
	// is loaded and new appends continue the same session file (so the context
	// summary and tool-result store for that session carry over too).
	ResumeSessionID string
	// Debug enables the opt-in per-session debug trace (the model-decision layer
	// that complements session.jsonl). Zero means off; observe.VerboseDefault /
	// observe.VerboseFull select how much is recorded. The trace lands at
	// <session-dir>/<id>.debug.jsonl.
	Debug observe.Verbose
	// PermissionMode overrides config when set by an explicit CLI flag.
	PermissionMode safety.PermissionMode
	// EnableBypass is the explicit top-level acknowledgement for bypass mode.
	EnableBypass bool
}

func Bootstrap(opts Options) (*App, error) {
	cfg, err := loadBootstrapConfig(opts.ConfigPath)
	if err != nil {
		return nil, err
	}

	mode := cfg.Safety.PermissionMode
	if opts.PermissionMode != "" {
		mode = opts.PermissionMode
	}
	bypassEnabled := cfg.Safety.EnableBypass || opts.EnableBypass
	modeSource, err := safety.NewPermissionModeSource(mode, bypassEnabled)
	if err != nil {
		return nil, err
	}
	policy := safety.Policy{
		Mode:                mode,
		BypassEnabled:       bypassEnabled,
		RuntimeModeSource:   modeSource,
		RequireConfirmation: cfg.Safety.RequireConfirmation,
		BlockedSubstrings:   cfg.Safety.BlockedCommands,
		Rules:               cfg.Safety.Rules,
	}
	// Fake and injected clients override the retried production client at the
	// bootstrap boundary, so tests and offline mode are never wrapped in retries.
	client := selectBootstrapClient(cfg.Model, opts)

	projectRoot, _ := os.Getwd()
	stores, err := openBootstrapStores(cfg, opts.ResumeSessionID, projectRoot)
	if err != nil {
		return nil, err
	}
	store := stores.sessions
	sessionID := stores.sessionID
	ruleSource := stores.ruleSource
	memoryStore := stores.memory
	taskStore := stores.tasks
	skillLoader := stores.skills
	policy.RuntimeRuleSource = ruleSource
	// Resolve confirmation before building child loops: every child captures the
	// same Policy + Confirmer safety boundary as the main agent.
	confirmer := selectBootstrapConfirmer(opts)
	// loopFactory makes every child agent run on a real agent.Loop wired with the
	// shared policy/confirmer + child-scoped contextx. The manager fails closed
	// when this factory is unavailable, so no child can bypass safety.
	loopFactory := newChildLoopFactory(childLoopDeps{
		client:     client,
		policy:     policy,
		confirmer:  confirmer,
		agent:      cfg.Agent,
		context:    cfg.Context,
		sessionDir: cfg.Session.Dir,
	})

	var application *App
	registry := tool.NewRegistry()
	eventSink := newSubagentEventSink(func() *App { return application })
	subagentManager, agentTaskService, collaborationStore, executionBackends, backendSupervisor, err := registerRuntimeTools(runtimeToolDeps{
		registry:            registry,
		client:              client,
		sessionID:           sessionID,
		sessionDir:          cfg.Session.Dir,
		memoryStore:         memoryStore,
		taskStore:           taskStore,
		skillLoader:         skillLoader,
		memoryConfig:        cfg.Memory,
		subagentConfig:      cfg.Subagent,
		collaborationConfig: cfg.Collaboration,
		skillsConfig:        cfg.Skills,
		questioner:          opts.Questioner,
		eventSink:           eventSink,
		loopFactory:         loopFactory,
		policy:              policy,
	})
	if err != nil {
		return nil, err
	}
	mcpManager, err := injectConfiguredMCPTools(context.Background(), registry, cfg.MCP)
	if err != nil {
		return nil, err
	}
	loop, contextManager, summaryStore := newBootstrapLoop(
		cfg,
		client,
		registry,
		policy,
		confirmer,
		sessionID,
		!opts.UseFakeLLM && opts.Client == nil,
	)

	debugLogger, debugLoggerFactory, err := configureBootstrapDebug(loop, cfg.Session.Dir, sessionID, opts.Debug)
	if err != nil {
		return nil, err
	}

	var memoryExtractor *MemoryExtractor
	if cfg.Memory.Enabled && cfg.Memory.AutoExtract {
		memoryExtractor = NewMemoryExtractor(client, memoryStore, MemoryExtractorConfig{Enabled: true})
	}

	application = &App{
		Config:             cfg,
		Tools:              registry,
		Sessions:           store,
		SessionID:          sessionID,
		Policy:             policy,
		Confirmer:          confirmer,
		RuleSource:         ruleSource,
		Questioner:         opts.Questioner,
		LLM:                client,
		Agent:              loop,
		ContextManager:     contextManager,
		ContextSummary:     summaryStore,
		MaxContextTokens:   cfg.Context.MaxTokens,
		MemoryStore:        memoryStore,
		MemoryMaxChars:     cfg.Memory.MaxChars,
		MemoryExtractor:    memoryExtractor,
		SkillLoader:        skillLoader,
		SkillMaxChars:      cfg.Skills.MaxChars,
		TaskStore:          taskStore,
		SubagentManager:    subagentManager,
		AgentTasks:         agentTaskService,
		Collaboration:      collaborationStore,
		ExecutionBackends:  executionBackends,
		BackendSupervisor:  backendSupervisor,
		MCPManager:         mcpManager,
		TrustManager:       trust.NewManager(filepath.Join(cfg.Session.Dir, "trust.json")),
		debugLogger:        debugLogger,
		debugLoggerFactory: debugLoggerFactory,
		RuntimePromptContext: agent.RuntimePromptContext{
			ProjectRoot: projectRoot,
		},
	}
	if mode == safety.ModePlan {
		application.SetPlanMode(true)
	}
	if opts.ResumeSessionID != "" {
		if err := application.loadHistorySnapshot(sessionID); err != nil {
			return nil, fmt.Errorf("resume session %q: %w", sessionID, err)
		}
	}
	return application, nil
}
