package safety

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
)

// PatternKey derives a regex pattern that matches the "same kind" of tool call
// as the given input, for the Phase 5A "Allow always" feature. The returned
// pattern is matched against the raw tool-input JSON (the same string
// Policy.matchRule regexes), so it must be shaped to match that JSON form.
//
// Granularity is intentionally coarse — a command's first token, a path's top
// directory — so one grant covers a sensible family ("all go commands", "all
// files under internal/tui") rather than one exact call. An empty result means
// the tool does not support Allow-always: the TUI then hides the Always option
// and only offers Allow once / Reject (safe downgrade — never auto-grants).
//
// Each pattern is anchored to its JSON field so it cannot accidentally match
// unrelated content elsewhere in the input (e.g. a path appearing inside a
// command string). Configured deny rules and dangerous-command hard blocks
// still override any runtime allow built from a PatternKey (see Policy.Decide).
func PatternKey(toolName, rawInput string) string {
	switch toolName {
	case "run_command":
		return commandPatternKey(rawInput)
	case "write_file", "edit_file", "read_file", "list_dir":
		return pathPatternKey(rawInput)
	default:
		return ""
	}
}

// commandPatternKey extracts the first token of the command and anchors it to
// the "command" JSON field. `go test ./...` and `go build` both yield a pattern
// equivalent to `"command":"go(\s|$|")`, so one grant covers the whole go tool
// family without also matching `gopher`.
func commandPatternKey(rawInput string) string {
	cmd := strings.TrimSpace(extractStringField(rawInput, "command"))
	if cmd == "" {
		return ""
	}
	first := commandFirstToken(cmd)
	if first == "" {
		return ""
	}
	return `"command":\s*"` + regexp.QuoteMeta(first) + `(\s|$|")`
}

// pathPatternKey anchors to the "path" field. For a path under a top directory
// (e.g. internal/tui/x.go) it grants at that directory prefix so one
// Allow-always covers the whole package; for a bare filename it matches exactly.
func pathPatternKey(rawInput string) string {
	p := strings.TrimSpace(extractStringField(rawInput, "path"))
	if p == "" {
		return ""
	}
	p = strings.TrimPrefix(p, "./")
	if top := pathTopDir(p); top != "" && top != filepath.ToSlash(p) {
		return `"path":\s*"` + regexp.QuoteMeta(top) + `/`
	}
	return `"path":\s*"` + regexp.QuoteMeta(p) + `"`
}

// commandFirstToken returns the leading command token, splitting on shell
// separators so prefixes like `cd x && go test` reduce to `cd` (the user then
// learns to grant on the real command) and `go test` reduces to `go`.
func commandFirstToken(cmd string) string {
	for _, sep := range []string{" ", "\t", "|", "&", ";", "\n"} {
		if i := strings.IndexAny(cmd, sep); i >= 0 {
			return strings.TrimSpace(cmd[:i])
		}
	}
	return strings.TrimSpace(cmd)
}

// pathTopDir returns the first segment of a slash-separated path, or "" if the
// path has no directory component (bare filename).
func pathTopDir(p string) string {
	p = filepath.ToSlash(p)
	before, _, ok := strings.Cut(p, "/")
	if !ok {
		return ""
	}
	return before
}

// extractStringField parses a single string field from a JSON object without
// requiring the caller to know the full schema. Returns "" if the field is
// absent or the input is not an object — same defensive parsing style as the
// dangerous-command guard.
func extractStringField(rawInput, field string) string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(rawInput), &obj); err != nil {
		return ""
	}
	v, ok := obj[field].(string)
	if !ok {
		return ""
	}
	return v
}
