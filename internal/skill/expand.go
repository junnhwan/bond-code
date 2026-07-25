package skill

import (
	"path/filepath"
	"runtime"
	"strings"
)

// ExpandContent builds the inline skill prompt the model should follow.
// Mirrors Claude Code createSkillCommand.getPromptForCommand (without shell injection):
// base directory prefix, then body, then $ARGUMENTS / $N substitution.
func ExpandContent(s Skill, args string) string {
	body := strings.TrimSpace(s.Body)
	baseDir := s.Dir
	if baseDir != "" {
		if runtime.GOOS == "windows" {
			baseDir = filepath.ToSlash(baseDir)
		}
		body = "Base directory for this skill: " + baseDir + "\n\n" + body
	}
	body = substituteArguments(body, args)
	if s.Dir != "" {
		skillDir := s.Dir
		if runtime.GOOS == "windows" {
			skillDir = filepath.ToSlash(skillDir)
		}
		body = strings.ReplaceAll(body, "${BONDCODE_SKILL_DIR}", skillDir)
		body = strings.ReplaceAll(body, "${CLAUDE_SKILL_DIR}", skillDir)
	}
	return body
}

func substituteArguments(content, args string) string {
	args = strings.TrimSpace(args)
	content = strings.ReplaceAll(content, "$ARGUMENTS", args)
	content = strings.ReplaceAll(content, "{{args}}", args)
	if args == "" {
		return content
	}
	parts := strings.Fields(args)
	for i, p := range parts {
		placeholder := "$" + itoa(i+1)
		content = strings.ReplaceAll(content, placeholder, p)
	}
	return content
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
