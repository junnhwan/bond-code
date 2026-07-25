package app

import "github.com/junnhwan/bond-code/internal/tool"

// SetPlanMode toggles plan mode. In plan mode the agent is steered toward
// research and a written plan, and the mutating tools are disabled at the loop
// level so it cannot change state even if it tries.
func (a *App) SetPlanMode(plan bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.planMode = plan
	if a.Agent != nil {
		if plan {
			a.Agent.SetDisabledTools(planModeDisabledTools)
		} else {
			a.Agent.SetDisabledTools(nil)
		}
	}
}

// planModeDisabledTools are the tools blocked while planning: anything that
// writes files or runs state-changing commands. Read-only inspection, todo
// planning, memory and ask_user stay available.
var planModeDisabledTools = []string{
	tool.WriteFile, tool.EditFile, tool.RunCommand,
}
