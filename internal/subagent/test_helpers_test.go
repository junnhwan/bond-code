package subagent

import (
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/tool"
)

// testLoopFactory keeps execution tests on the same agent.Loop boundary as
// production while using an explicitly permissive policy for fixture tools.
func testLoopFactory(client llm.Client) LoopFactory {
	return func(req LoopRequest) *agent.Loop {
		maxSteps := req.MaxSteps
		if maxSteps <= 0 {
			maxSteps = req.Profile.MaxSteps
		}
		return agent.NewLoop(agent.LoopConfig{
			MaxSteps:             maxSteps,
			MaxRepeatedToolCalls: 100,
		}, client, req.Tools, safety.Policy{}, safety.StaticConfirmer(true))
	}
}

func newTestManager(client llm.Client, registry *tool.Registry) *SubagentManager {
	return newTestManagerWithOptions(client, registry, DefaultManagerOptions())
}

func newUnconfiguredTestManager(client llm.Client, registry *tool.Registry) *SubagentManager {
	return NewSubagentManagerWithOptions(client, registry, DefaultManagerOptions())
}

func newTestManagerWithOptions(client llm.Client, registry *tool.Registry, options ManagerOptions) *SubagentManager {
	options.LoopFactory = testLoopFactory(client)
	return NewSubagentManagerWithOptions(client, registry, options)
}
