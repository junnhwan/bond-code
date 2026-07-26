package skill

import (
	"strings"
)

// frontmatter holds the subset of Claude Code skill frontmatter we support.
type frontmatter struct {
	Name                   string
	Description            string
	WhenToUse              string
	ArgumentHint           string
	AllowedTools           []string
	DisableModelInvocation bool
	UserInvocable          bool
}

// parseFrontmatter splits optional YAML-ish frontmatter from markdown body.
// Supports simple key: value lines and one-line lists (allowed-tools).
func parseFrontmatter(raw string) (frontmatter, string) {
	text := strings.ReplaceAll(raw, "\r\n", "\n")
	trimmed := strings.TrimLeft(text, " \t")
	// user-invocable defaults true (Claude Code): omit the key → slash-visible.
	if !strings.HasPrefix(trimmed, "---") {
		return frontmatter{UserInvocable: true}, strings.TrimSpace(text)
	}
	rest := strings.TrimPrefix(trimmed, "---")
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return frontmatter{UserInvocable: true}, strings.TrimSpace(text)
	}
	block := rest[:end]
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")

	fm := frontmatter{UserInvocable: true}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		switch key {
		case "name":
			fm.Name = val
		case "description":
			fm.Description = val
		case "when_to_use", "when-to-use":
			fm.WhenToUse = val
		case "argument-hint", "argument_hint":
			fm.ArgumentHint = val
		case "allowed-tools", "allowed_tools":
			fm.AllowedTools = parseStringList(val)
		case "disable-model-invocation", "disable_model_invocation":
			fm.DisableModelInvocation = parseBool(val)
		case "user-invocable", "user_invocable":
			if val != "" {
				fm.UserInvocable = parseBool(val)
			}
		}
	}
	if fm.Description == "" {
		fm.Description = extractDescriptionFromMarkdown(body)
	}
	return fm, strings.TrimSpace(body)
}

func parseBool(val string) bool {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseStringList(val string) []string {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}
	val = strings.TrimPrefix(val, "[")
	val = strings.TrimSuffix(val, "]")
	parts := strings.Split(val, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(strings.TrimSpace(p), `"'`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func extractDescriptionFromMarkdown(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) > 200 {
			return line[:200]
		}
		return line
	}
	return "Skill"
}
