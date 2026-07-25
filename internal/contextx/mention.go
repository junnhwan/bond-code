package contextx

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	MaxMentionFileBytes        = 120 * 1024
	MaxMentionDirectoryEntries = 80
)

var localPathMentionRE = regexp.MustCompile(`(^|[\s"'(\[{<,:;])@(<[^>]+>|[^\s<>"')\]\}]+)`)

func ExpandPathMentions(input string, projectRoot string) string {
	if strings.TrimSpace(input) == "" || !strings.Contains(input, "@") {
		return input
	}
	root := realPathOrClean(projectRoot)
	return localPathMentionRE.ReplaceAllStringFunc(input, func(match string) string {
		leading := ""
		token := match
		if len(match) > 0 && match[0] != '@' {
			leading = match[:1]
			token = match[1:]
		}
		raw := strings.TrimPrefix(token, "@")
		return leading + expandPathMentionToken(raw, root)
	})
}

func expandPathMentionToken(raw string, root string) string {
	if raw == "" {
		return "@" + raw
	}
	value := stripMentionAngles(raw)
	value, lineRange, hasLineRange := parseMentionLineRange(value)
	if value == "" || strings.Contains(value, ":") {
		return "@" + raw
	}
	candidate := value
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate = filepath.Clean(candidate)
	info, err := os.Stat(candidate)
	if err != nil {
		return "@" + raw
	}
	realCandidate := realPathOrClean(candidate)
	if !isWithinRoot(realCandidate, root) {
		return "@" + raw
	}
	if info.IsDir() {
		if hasLineRange {
			return "@" + raw
		}
		block, err := renderMentionDirectory(realCandidate, root)
		if err != nil {
			return "@" + raw
		}
		return "@" + value + "\n" + block
	}
	if info.Mode().IsRegular() {
		block, err := renderMentionFile(realCandidate, root, lineRange, hasLineRange)
		if err != nil {
			return "@" + raw
		}
		return "@" + stripMentionAngles(raw) + "\n" + block
	}
	return "@" + raw
}

type mentionLineRange struct {
	Start int
	End   int
}

func parseMentionLineRange(value string) (string, mentionLineRange, bool) {
	idx := strings.LastIndex(value, ":")
	if idx < 0 {
		return value, mentionLineRange{}, false
	}
	pathPart := strings.TrimSpace(value[:idx])
	spec := strings.TrimSpace(value[idx+1:])
	if pathPart == "" || spec == "" {
		return value, mentionLineRange{}, false
	}
	startText, endText, hasDash := strings.Cut(spec, "-")
	start, err := strconv.Atoi(strings.TrimSpace(startText))
	if err != nil || start < 1 {
		return value, mentionLineRange{}, false
	}
	end := start
	if hasDash {
		end, err = strconv.Atoi(strings.TrimSpace(endText))
		if err != nil || end < start {
			return value, mentionLineRange{}, false
		}
	}
	return pathPart, mentionLineRange{Start: start, End: end}, true
}

func renderMentionFile(path string, root string, lineRange mentionLineRange, hasLineRange bool) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	rel := mentionDisplayPath(path, root)
	limit := len(b)
	truncated := false
	if limit > MaxMentionFileBytes {
		limit = MaxMentionFileBytes
		truncated = true
	}
	if looksBinary(b[:limit]) {
		return fmt.Sprintf(`<file path="%s" binary="true">binary omitted</file>`, escapeMentionXML(rel)), nil
	}
	body := string(b[:limit])
	if hasLineRange {
		selected, err := selectMentionLines(body, lineRange)
		if err != nil {
			return "", err
		}
		lineAttr := strconv.Itoa(lineRange.Start)
		if lineRange.End != lineRange.Start {
			lineAttr = fmt.Sprintf("%d-%d", lineRange.Start, lineRange.End)
		}
		return fmt.Sprintf("<file path=\"%s\" lines=\"%s\">\n%s\n</file>", escapeMentionXML(rel), escapeMentionXML(lineAttr), selected), nil
	}
	if truncated {
		body += fmt.Sprintf("\n[file truncated at %d bytes]", MaxMentionFileBytes)
	}
	return fmt.Sprintf("<file path=\"%s\">\n%s\n</file>", escapeMentionXML(rel), body), nil
}

func selectMentionLines(body string, lineRange mentionLineRange) (string, error) {
	lines := strings.Split(body, "\n")
	if lineRange.Start > len(lines) {
		return "", fmt.Errorf("line range starts past end of file")
	}
	end := lineRange.End
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[lineRange.Start-1:end], "\n"), nil
}

func renderMentionDirectory(path string, root string) (string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	rel := mentionDisplayPath(path, root)
	var out strings.Builder
	fmt.Fprintf(&out, "<directory path=\"%s\">\n", escapeMentionXML(rel))
	count := len(entries)
	if count > MaxMentionDirectoryEntries {
		count = MaxMentionDirectoryEntries
	}
	for i := 0; i < count; i++ {
		entry := entries[i]
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		out.WriteString("- ")
		out.WriteString(name)
		out.WriteByte('\n')
	}
	if len(entries) > MaxMentionDirectoryEntries {
		fmt.Fprintf(&out, "[directory truncated at %d entries]\n", MaxMentionDirectoryEntries)
	}
	out.WriteString("</directory>")
	return out.String(), nil
}

func realPathOrClean(path string) string {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	if realPath, err := filepath.EvalSymlinks(path); err == nil {
		if abs, err := filepath.Abs(realPath); err == nil {
			return filepath.Clean(abs)
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func isWithinRoot(path string, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func mentionDisplayPath(path string, root string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return "."
	}
	return filepath.ToSlash(rel)
}

func stripMentionAngles(raw string) string {
	if strings.HasPrefix(raw, "<") && strings.HasSuffix(raw, ">") && len(raw) >= 2 {
		return strings.TrimSpace(raw[1 : len(raw)-1])
	}
	return raw
}

func looksBinary(b []byte) bool {
	return strings.IndexByte(string(b), 0) >= 0
}

func escapeMentionXML(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, `"`, "&quot;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	return value
}
