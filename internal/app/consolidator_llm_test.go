package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/memory"
)

func TestConsolidateMemoriesSkipsBelowThreshold(t *testing.T) {
	store, err := memory.NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Save(memory.MemoryFile{Type: memory.TypeProject, Name: "fact", Description: "fact", Body: "fact"})
	client := llm.NewFakeClient([]llm.Chunk{{Content: `["should_not_matter.md"]`, Done: true}})
	n, err := ConsolidateMemories(context.Background(), client, store, 10)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 deletions below threshold, got %d", n)
	}
	count, _ := store.Count()
	if count != 1 {
		t.Fatalf("file should remain, count=%d", count)
	}
}

func TestConsolidateMemoriesDeletesSelected(t *testing.T) {
	store, err := memory.NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("Note %d", i)
		_ = store.Save(memory.MemoryFile{
			Type: memory.TypeProject, Name: name, Description: "fact " + name, Body: "fact",
		})
	}
	files, _ := store.List()
	if len(files) != 5 {
		t.Fatalf("setup failed: %d", len(files))
	}
	// Delete first two filenames.
	payload := fmt.Sprintf(`["%s","%s"]`, files[0].Filename, files[1].Filename)
	client := llm.NewFakeClient([]llm.Chunk{{Content: payload, Done: true}})
	n, err := ConsolidateMemories(context.Background(), client, store, 5)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 deletions, got %d", n)
	}
	count, _ := store.Count()
	if count != 3 {
		t.Fatalf("expected 3 remaining, got %d", count)
	}
}

func TestConsolidateMemoriesBadLLMDeletesNothing(t *testing.T) {
	store, err := memory.NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_ = store.Save(memory.MemoryFile{
			Type: memory.TypeProject, Name: fmt.Sprintf("N%d", i), Description: "f", Body: "f",
		})
	}
	client := llm.NewFakeClient([]llm.Chunk{{Content: `not json`, Done: true}})
	n, err := ConsolidateMemories(context.Background(), client, store, 5)
	if n != 0 {
		t.Fatalf("expected 0 deletions, got %d (err=%v)", n, err)
	}
	count, _ := store.Count()
	if count != 5 {
		t.Fatalf("count=%d", count)
	}
}

func TestConsolidateMemoriesNilSafe(t *testing.T) {
	n, err := ConsolidateMemories(context.Background(), nil, nil, 5)
	if n != 0 || err != nil {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
