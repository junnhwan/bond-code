package app

import (
	"fmt"
	"path/filepath"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/hook"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/observe"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
)

func selectBootstrapConfirmer(opts Options) safety.Confirmer {
	confirmer := opts.Confirmer
	if opts.AutoYes {
		confirmer = safety.AutoApproveConfirmer{MaxRisk: "medium", Fallback: confirmer}
	}
	if confirmer == nil {
		confirmer = safety.StaticConfirmer(false)
	}
	return confirmer
}

func newBootstrapLoop(
	cfg *config.Config,
	client llm.Client,
	registry *tool.Registry,
	policy safety.Policy,
	confirmer safety.Confirmer,
	sessionID string,
	enableLLMSummary bool,
) (*agent.Loop, *contextx.Manager, *contextx.SummaryStore) {
	var contextManager *contextx.Manager
	var summaryStore *contextx.SummaryStore
	if cfg.Context.Enabled {
		summaryStore = contextx.NewSummaryStore(cfg.Session.Dir, sessionID)
		store := contextx.NewToolResultStore(cfg.Session.Dir, sessionID)
		contextManager = contextx.NewManager(contextx.NewGovernor(
			governorConfigFrom(cfg.Context, store),
		))
	}

	loop := agent.NewLoop(agent.LoopConfig{
		MaxSteps:                  cfg.Agent.MaxSteps,
		MaxRepeatedToolCalls:      cfg.Agent.MaxRepeatedToolCalls,
		MaxToolCallsPerStep:       cfg.Agent.MaxToolCallsPerStep,
		MaxRepeatedTextChunks:     cfg.Agent.MaxRepeatedTextChunks,
		MaxRepeatedTextSubstrings: cfg.Agent.MaxRepeatedTextSubstrings,
	}, client, registry, policy, confirmer)
	hooks := &hook.Registry{}
	registerBuiltinHooks(hooks)
	loop.SetHooks(hooks)
	loop.SetLLMSummaryEnabled(enableLLMSummary)
	if contextManager != nil {
		loop.SetContextManager(contextManager, cfg.Context.MaxTokens)
		loop.SetContextSummaryStore(summaryStore)
	}
	return loop, contextManager, summaryStore
}

func configureBootstrapDebug(loop *agent.Loop, sessionDir, sessionID string, verbose observe.Verbose) (observe.Logger, debugLoggerFactory, error) {
	if verbose <= 0 {
		return nil, nil, nil
	}
	factory := func(id string) (observe.Logger, error) {
		return observe.NewDebugFileLogger(filepath.Join(sessionDir, id+".debug.jsonl"), verbose)
	}
	logger, err := factory(sessionID)
	if err != nil {
		return nil, nil, fmt.Errorf("debug logger: %w", err)
	}
	loop.SetDebugLogger(logger)
	return logger, factory, nil
}
