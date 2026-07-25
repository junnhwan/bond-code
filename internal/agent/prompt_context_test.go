package agent

import (
	"strings"
	"testing"
)

func TestBuildSystemPromptPlanModeInjectsInstructions(t *testing.T) {
	got := BuildSystemPrompt(RuntimePromptContext{PlanMode: true})
	if !strings.Contains(got, "Plan Mode (ACTIVE)") {
		t.Fatalf("plan mode section missing:\n%s", got)
	}
	if !strings.Contains(got, "PLAN MODE") {
		t.Fatalf("expected plan-mode guidance in prompt:\n%s", got)
	}
}

func TestBuildSystemPromptNormalOmitsPlanMode(t *testing.T) {
	got := BuildSystemPrompt(RuntimePromptContext{PlanMode: false})
	if strings.Contains(got, "Plan Mode") {
		t.Fatalf("plan mode section should be absent in normal mode:\n%s", got)
	}
}

func TestBuildSystemPromptPrioritizesExplicitTeamAgentTaskDispatch(t *testing.T) {
	prompt := BuildSystemPrompt(RuntimePromptContext{})
	for _, want := range []string{
		"explicitly asks for team_create, team_add_member, or agent_task",
		"dispatch them immediately in the requested order",
		"Do not pause to create a todo plan before explicit orchestration",
		"Do not first call list_dir, search_text, read_file",
		"return the Team, Member, and Task IDs immediately",
		"Do not call task_output with wait=true",
		"queued is only the submission-time snapshot",
		"do not later claim the task is currently queued",
		"Do not delegate trivial questions",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("explicit multi-agent dispatch policy missing %q:\n%s", want, prompt)
		}
	}
}
