package app

import (
	"context"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/memory"
)

func TestMemoryExtractorAppliesValidMemories(t *testing.T) {
	store, err := memory.NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := llm.NewFakeClient([]llm.Chunk{
		{Content: `[{"type":"user","name":"Indent style","description":"User prefers tabs","content":"User prefers tabs over spaces for indentation."}]`, Done: true},
	})
	ext := NewMemoryExtractor(client, store, MemoryExtractorConfig{Enabled: true, MaxDialogueMessages: 5, MaxDialogueChars: 1000})
	n, err := ext.Extract(context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: "Please always use tabs, not spaces."},
		{Role: llm.RoleAssistant, Content: "Got it, I'll use tabs."},
	})
	if err != nil {
		t.Fatalf("extract returned error: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 written, got %d", n)
	}
	files, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Type != memory.TypeUser {
		t.Fatalf("expected user type, got %s", files[0].Type)
	}
}

func TestMemoryExtractorSkipsWhenModelAlreadyWrote(t *testing.T) {
	store, err := memory.NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.MarkModelWrite()
	client := llm.NewFakeClient([]llm.Chunk{
		{Content: `[{"type":"user","name":"X","description":"x","content":"should not write"}]`, Done: true},
	})
	ext := NewMemoryExtractor(client, store, MemoryExtractorConfig{Enabled: true})
	n, err := ext.Extract(context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: "remember tabs"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected skip, got %d", n)
	}
	count, _ := store.Count()
	if count != 0 {
		t.Fatalf("expected no files, got %d", count)
	}
}

func TestMemoryExtractorEmptyArrayWritesNothing(t *testing.T) {
	store, err := memory.NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := llm.NewFakeClient([]llm.Chunk{{Content: `[]`, Done: true}})
	ext := NewMemoryExtractor(client, store, MemoryExtractorConfig{Enabled: true})
	n, err := ext.Extract(context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: "hello"},
	})
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestMemoryExtractorNilSafe(t *testing.T) {
	var ext *MemoryExtractor
	n, err := ext.Extract(context.Background(), nil)
	if n != 0 || err != nil {
		t.Fatalf("nil extractor should be a no-op, got n=%d err=%v", n, err)
	}
}

func TestMemoryExtractorEmptyDialogueIsNoOp(t *testing.T) {
	store, err := memory.NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := llm.NewFakeClient([]llm.Chunk{{Content: `[{"type":"project","name":"x","description":"x","content":"x"}]`, Done: true}})
	ext := NewMemoryExtractor(client, store, MemoryExtractorConfig{Enabled: true})
	n, err := ext.Extract(context.Background(), nil)
	if err != nil || n != 0 {
		t.Fatalf("empty dialogue should no-op, n=%d err=%v", n, err)
	}
}
