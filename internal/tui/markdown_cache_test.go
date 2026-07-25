package tui

import (
	"strings"
	"testing"
)

func TestMarkdownRenderIsCachedPerBlock(t *testing.T) {
	renderer, err := NewMarkdownRenderer(80)
	if err != nil {
		t.Fatalf("new markdown renderer: %v", err)
	}
	model := NewModel(Config{})
	model.markdownRenderer = renderer

	first := model.renderCachedMarkdownForWidth("block-1", "# Title", 80)
	if !strings.Contains(first, "Title") {
		t.Fatalf("expected rendered markdown to keep the title text, got:\n%s", first)
	}
	if _, ok := model.markdownCache["block-1"]; !ok {
		t.Fatal("expected render result to be cached by block id")
	}

	cached := model.renderCachedMarkdownForWidth("block-1", "# Title", 80)
	if cached != first {
		t.Fatalf("expected second render to hit the cache, got:\n%s", cached)
	}

	updated := model.renderCachedMarkdownForWidth("block-1", "# Other", 80)
	if updated == first {
		t.Fatal("expected a changed body to re-render instead of returning the stale cache")
	}
	if entry := model.markdownCache["block-1"]; entry.body != "# Other" {
		t.Fatalf("expected cache to track the new body, got %q", entry.body)
	}
}

func TestMarkdownRenderCacheIsScopedByWidth(t *testing.T) {
	model := NewModel(Config{})

	_ = model.renderCachedMarkdownForWidth("block-1", strings.Repeat("word ", 20), 80)
	if entry := model.markdownCache["block-1"]; entry.width != 80 {
		t.Fatalf("expected cache width 80, got %#v", entry)
	}

	_ = model.renderCachedMarkdownForWidth("block-1", strings.Repeat("word ", 20), 32)
	if entry := model.markdownCache["block-1"]; entry.width != 32 {
		t.Fatalf("expected width-specific cache refresh to 32, got %#v", entry)
	}
}

func TestMarkdownRendererUsesStableCodeThemeAcrossResize(t *testing.T) {
	renderer, err := NewMarkdownRenderer(80)
	if err != nil {
		t.Fatalf("new markdown renderer: %v", err)
	}
	if renderer.style != markdownRendererStyle {
		t.Fatalf("expected markdown renderer style %q, got %q", markdownRendererStyle, renderer.style)
	}

	if err := renderer.UpdateWidth(100); err != nil {
		t.Fatalf("update width: %v", err)
	}
	if renderer.style != markdownRendererStyle {
		t.Fatalf("expected resize to preserve style %q, got %q", markdownRendererStyle, renderer.style)
	}
}

func TestMarkdownRendererKeepsPlainTextSearchable(t *testing.T) {
	renderer, err := NewMarkdownRenderer(80)
	if err != nil {
		t.Fatalf("new markdown renderer: %v", err)
	}

	rendered, err := renderer.Render("reading README")
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	if !strings.Contains(rendered, "reading README") {
		t.Fatalf("expected rendered plain text to remain searchable, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "\x1b[") {
		t.Fatalf("plain text should not receive ANSI styling, got:\n%q", rendered)
	}
}

func TestInvalidateMarkdownCacheDropsEntries(t *testing.T) {
	model := NewModel(Config{})
	renderer, err := NewMarkdownRenderer(80)
	if err != nil {
		t.Fatalf("new markdown renderer: %v", err)
	}
	model.markdownRenderer = renderer

	_ = model.renderCachedMarkdownForWidth("block-1", "# Title", 80)
	if len(model.markdownCache) == 0 {
		t.Fatal("expected cache entry after render")
	}
	model.invalidateMarkdownCache()
	if len(model.markdownCache) != 0 {
		t.Fatalf("expected cache to be empty after invalidation, got %#v", model.markdownCache)
	}
}
