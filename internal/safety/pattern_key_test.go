package safety

import (
	"testing"

	"github.com/junnhwan/bond-code/internal/tool"
)

// TestPatternKeyEmptyForUnsupportedTool confirms tools outside the supported
// set return "" — the TUI uses this to hide the Always option (safe downgrade).
func TestPatternKeyEmptyForUnsupportedTool(t *testing.T) {
	if got := PatternKey("search_text", `{"pattern":"x"}`); got != "" {
		t.Fatalf("unsupported tool should yield empty pattern, got %q", got)
	}
}

// TestPatternKeyCommandFamily confirms one grant covers a command family: go
// test and go build match the same pattern, while an unrelated command does not.
func TestPatternKeyCommandFamily(t *testing.T) {
	pk := PatternKey("run_command", `{"command":"go test ./..."}`)
	if pk == "" {
		t.Fatal("expected non-empty command pattern")
	}
	p := Policy{RuntimeRuleSource: testRuleSource{
		{Tools: []string{"run_command"}, Pattern: pk, Decision: "allow"},
	}}
	// Same first token, different args -> match.
	if d := p.Decide("run_command", tool.RiskMedium, `{"command":"go build ./..."}`); d != Allow {
		t.Fatalf("same command family should match, got %s", d)
	}
	// Different command -> no match (falls back to risk default).
	if _, ok := p.matchRule("run_command", `{"command":"ls -la"}`); ok {
		t.Fatal("different command should not match the runtime allow")
	}
	// A lookalike token (gopher) must NOT match the go grant.
	if _, ok := p.matchRule("run_command", `{"command":"gopherize"}`); ok {
		t.Fatal("lookalike token should not match")
	}
}

// TestPatternKeyCommandToleratesJsonSpaces confirms the pattern matches whether
// or not the JSON serializer put a space after the colon.
func TestPatternKeyCommandToleratesJsonSpaces(t *testing.T) {
	pk := PatternKey("run_command", `{"command": "go test"}`)
	p := Policy{RuntimeRuleSource: testRuleSource{
		{Tools: []string{"run_command"}, Pattern: pk, Decision: "allow"},
	}}
	if d := p.Decide("run_command", tool.RiskMedium, `{"command":"go build"}`); d != Allow {
		t.Fatalf("should match across JSON spacing, got %s", d)
	}
}

// TestPatternKeyPathDirectory confirms a path grant covers the top directory so
// one Allow-always covers a package, while a bare filename matches exactly.
func TestPatternKeyPathDirectory(t *testing.T) {
	pk := PatternKey("write_file", `{"path":"internal/tui/x.go"}`)
	if pk == "" {
		t.Fatal("expected non-empty path pattern")
	}
	p := Policy{RuntimeRuleSource: testRuleSource{
		{Tools: []string{"write_file"}, Pattern: pk, Decision: "allow"},
	}}
	// Same top directory -> match.
	if d := p.Decide("write_file", tool.RiskMedium, `{"path":"internal/tui/y.go"}`); d != Allow {
		t.Fatalf("same directory should match, got %s", d)
	}
	// Different top directory -> no match.
	if _, ok := p.matchRule("write_file", `{"path":"README.md"}`); ok {
		t.Fatal("different path should not match the runtime allow")
	}
}

// TestPatternKeyPathBareFilename confirms a bare filename (no directory) yields
// an exact-match pattern rather than a directory prefix.
func TestPatternKeyPathBareFilename(t *testing.T) {
	pk := PatternKey("read_file", `{"path":"README.md"}`)
	p := Policy{RuntimeRuleSource: testRuleSource{
		{Tools: []string{"read_file"}, Pattern: pk, Decision: "allow"},
	}}
	if d := p.Decide("read_file", tool.RiskMedium, `{"path":"README.md"}`); d != Allow {
		t.Fatalf("exact filename should match, got %s", d)
	}
	if _, ok := p.matchRule("read_file", `{"path":"OTHER.md"}`); ok {
		t.Fatal("different filename should not match")
	}
}

// TestPatternKeyCommandEmptyOnMissingField confirms a malformed/empty command
// yields "" rather than a permissive pattern.
func TestPatternKeyCommandEmptyOnMissingField(t *testing.T) {
	if got := PatternKey("run_command", `{"command":""}`); got != "" {
		t.Fatalf("empty command should yield empty pattern, got %q", got)
	}
	if got := PatternKey("run_command", `not json`); got != "" {
		t.Fatalf("malformed input should yield empty pattern, got %q", got)
	}
}
