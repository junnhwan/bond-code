package skill

import (
	"context"
	"strings"
	"testing"
)

func TestSkillToolExpandsInlineWithBaseDirAndArgs(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "commit", "create a commit", "", "Run git commit with $ARGUMENTS\n")
	loader := NewLoaderFromRoot(root)

	result, err := NewTool(loader).Execute(context.Background(), []byte(`{"skill":"commit","args":"-m fix"}`))
	if err != nil {
		t.Fatalf("skill execute: %v", err)
	}
	for _, want := range []string{
		"Base directory for this skill:",
		"Run git commit with -m fix",
	} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("skill output missing %q:\n%s", want, result.Output)
		}
	}
	if result.Metadata["status"] != "inline" {
		t.Fatalf("metadata status = %#v", result.Metadata["status"])
	}
}

func TestSkillToolAcceptsLeadingSlash(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "review", "review code", "", "review body")
	loader := NewLoaderFromRoot(root)
	result, err := NewTool(loader).Execute(context.Background(), []byte(`{"skill":"/review"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "review body") {
		t.Fatalf("output:\n%s", result.Output)
	}
}

func TestFormatListingBudget(t *testing.T) {
	skills := []Skill{
		{Name: "a", Description: strings.Repeat("x", 300)},
		{Name: "b", Description: "short"},
	}
	out := FormatListing(skills, 8000)
	if !strings.Contains(out, "- a:") || !strings.Contains(out, "- b: short") {
		t.Fatalf("listing:\n%s", out)
	}
	if strings.Contains(out, strings.Repeat("x", 260)) {
		t.Fatal("expected description truncated to MaxListingDescChars")
	}
}
