package skill

import (
	"strings"
	"testing"
)

func TestExpandContentSubstitutesArgsAndSkillDir(t *testing.T) {
	s := Skill{
		Name: "demo",
		Dir:  `/tmp/skills/demo`,
		Body: "Use ${BONDCODE_SKILL_DIR}/script.sh with $ARGUMENTS ($1)",
	}
	got := ExpandContent(s, "alpha beta")
	if !strings.Contains(got, "Base directory for this skill: /tmp/skills/demo") {
		t.Fatalf("missing base dir:\n%s", got)
	}
	if !strings.Contains(got, "/tmp/skills/demo/script.sh with alpha beta (alpha)") {
		t.Fatalf("substitution failed:\n%s", got)
	}
}
