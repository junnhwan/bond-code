package memory

import (
	"fmt"
	"strings"
)

// parseFrontmatter splits optional YAML-ish frontmatter from markdown body.
// Only the simple key: value lines we emit are supported (name/description/type).
func parseFrontmatter(raw string) (map[string]string, string) {
	text := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(strings.TrimLeft(text, " \t"), "---") {
		return nil, strings.TrimSpace(text)
	}
	// Find opening ---
	rest := strings.TrimLeft(text, " \t")
	rest = strings.TrimPrefix(rest, "---")
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, strings.TrimSpace(text)
	}
	fmBlock := rest[:end]
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")

	meta := map[string]string{}
	for _, line := range strings.Split(fmBlock, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		meta[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(val)
	}
	return meta, strings.TrimSpace(body)
}

func formatTopicFile(name, description string, typ MemoryType, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: ")
	b.WriteString(strings.TrimSpace(name))
	b.WriteString("\n")
	b.WriteString("description: ")
	b.WriteString(strings.TrimSpace(description))
	b.WriteString("\n")
	b.WriteString("type: ")
	b.WriteString(string(typ))
	b.WriteString("\n")
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")
	return b.String()
}

func indexLine(name, filename, description string) string {
	title := strings.TrimSpace(name)
	if title == "" {
		title = strings.TrimSuffix(filename, ".md")
	}
	hook := strings.TrimSpace(description)
	if hook == "" {
		hook = title
	}
	// Keep under ~150 chars like CC.
	line := fmt.Sprintf("- [%s](%s) — %s", title, filename, hook)
	if len(line) > 150 {
		line = line[:147] + "..."
	}
	return line
}
