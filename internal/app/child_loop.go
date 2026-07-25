package app

import (
	"path/filepath"

	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/config"
	"github.com/junnhwan/bond-code/internal/contextx"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/subagent"
)

// childLoopDeps bundles the shared, session-scoped inputs every child agent.Loop
// reuses from the main session. Capturing them once (instead of per child) keeps
// the factory closure small and makes the wiring explicit.
type childLoopDeps struct {
	client     llm.Client
	policy     safety.Policy
	confirmer  safety.Confirmer
	agent      config.AgentConfig
	context    config.ContextConfig
	sessionDir string
}

// newChildLoopFactory builds the LoopFactory injected into the SubagentManager.
// Each child agent (a `task` subagent, including parallel/chain batch children)
// gets a fresh agent.Loop that reuses the main session's client + Policy +
// Confirmer, so child tool execution flows through the same safety boundary as
// the main agent, enforcing the "all tool execution must go through
// Policy+Confirmer" invariant.
//
// Child-scoped contextx mirrors the main session governor, but spill/summary
// land under <sessionDir>/subagents/<taskID> so a child's large tool results
// never pollute the main session audit.
func newChildLoopFactory(d childLoopDeps) subagent.LoopFactory {
	return func(req subagent.LoopRequest) *agent.Loop {
		maxSteps := req.MaxSteps
		if maxSteps <= 0 {
			maxSteps = req.Profile.MaxSteps
		}
		childLoop := agent.NewLoop(agent.LoopConfig{
			MaxSteps:                  maxSteps,
			MaxRepeatedToolCalls:      d.agent.MaxRepeatedToolCalls,
			MaxToolCallsPerStep:       d.agent.MaxToolCallsPerStep,
			MaxRepeatedTextChunks:     d.agent.MaxRepeatedTextChunks,
			MaxRepeatedTextSubstrings: d.agent.MaxRepeatedTextSubstrings,
		}, d.client, req.Tools, d.policy, d.confirmer)

		if d.context.Enabled {
			childDir := filepath.Join(d.sessionDir, "subagents", req.TaskID)
			store := contextx.NewToolResultStore(childDir, req.TaskID)
			childMgr := contextx.NewManager(contextx.NewGovernor(
				governorConfigFrom(d.context, store),
			))
			childLoop.SetContextManager(childMgr, d.context.MaxTokens)
			childLoop.SetContextSummaryStore(contextx.NewSummaryStore(childDir, req.TaskID))
		}

		return childLoop
	}
}
