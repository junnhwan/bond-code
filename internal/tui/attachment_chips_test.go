package tui

import (
	"strings"
	"testing"
)

// TestExtractFileMentions covers the @word and @<path with spaces> forms, the
// boundary rule (mid-token @ is not a mention), and multiple mentions in one
// draft.
func TestExtractFileMentions(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"plain word", "fix @internal/tui/model.go", []string{"internal/tui/model.go"}},
		{"bracketed", "look at @<my file.go> and @<other dir>", []string{"my file.go", "other dir"}},
		{"multiple", "@a.go @b.go then @c.go", []string{"a.go", "b.go", "c.go"}},
		{"email not a mention", "contact me@x.com please", nil},
		{"none", "no mentions here", nil},
		{"trailing partial", "edit @inter", []string{"inter"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractFileMentions(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("extractFileMentions(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("mention %d: got %q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestPromptAttachmentsLineEmptyAndRendered checks the chip row is empty with no
// mentions and renders each mention as a chip when present.
func TestPromptAttachmentsLineEmptyAndRendered(t *testing.T) {
	model := NewModel(Config{})
	if line := model.promptAttachmentsLine(); line != "" {
		t.Fatalf("expected empty attachments line with no mentions, got %q", line)
	}
	model = model.SetInput("fix @internal/tui/model.go and @<a dir>")
	line := model.promptAttachmentsLine()
	if line == "" {
		t.Fatal("expected a chip row with mentions present")
	}
	if !strings.Contains(line, "internal/tui/model.go") {
		t.Errorf("expected chip to name the file, got %q", line)
	}
	if !strings.Contains(line, "a dir") {
		t.Errorf("expected chip to name the bracketed dir, got %q", line)
	}
}

// TestComposerHeightAccountsForChips verifies the layout reserve grows by one
// row when the chip line is present (so the timeline does not overlap).
func TestComposerHeightAccountsForChips(t *testing.T) {
	model := NewModel(Config{})
	base := model.composerHeight()
	model = model.SetInput("@some/file.go fix it")
	withChips := model.composerHeight()
	if withChips != base+1 {
		t.Fatalf("expected composer height +1 with chips (%d -> %d)", base, withChips)
	}
}
