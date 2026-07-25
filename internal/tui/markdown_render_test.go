package tui

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestMarkdownRendererPreservesStructureWithinWidth(t *testing.T) {
	r, err := NewMarkdownRenderer(52)
	if err != nil {
		t.Fatal(err)
	}
	md := "# Heading\n\nParagraph with **strong**, *emphasis*, [link](https://example.com), and `inline`.\n\n- outer\n  - nested\n> quote\n\n```go\nfunc main() { println(\"hello\") }\n```\n\n| Name | Value |\n| --- | --- |\n| alpha | beta |"
	got, err := r.Render(md)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Heading", "strong", "nested", "quote", "hello", "alpha", "beta"} {
		if !strings.Contains(ansi.Strip(got), want) {
			t.Errorf("missing %q in %q", want, ansi.Strip(got))
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if ansi.StringWidth(line) > 52 {
			t.Errorf("line width %d > 52: %q", ansi.StringWidth(line), line)
		}
	}
}
func TestMarkdownRendererNarrowTableKeepsCellContent(t *testing.T) {
	r, err := NewMarkdownRenderer(24)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Render("| Component | Status |\n| --- | --- |\n| renderer | working |\n| composer | improved |")
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(got)
	for _, want := range []string{"Component", "Status", "renderer", "working", "composer", "improved"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in %q", want, plain)
		}
	}
}
