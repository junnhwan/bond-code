package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/memory"
)

func TestSelectRelevantMemoriesReturnsAllWhenFew(t *testing.T) {
	store, err := memory.NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		_ = store.Save(memory.MemoryFile{
			Type: memory.TypeProject, Name: fmt.Sprintf("F%d", i),
			Description: "fact " + string(rune('a'+i)), Body: "body",
		})
	}
	// Client should not be needed when count <= max.
	files, err := selectRelevantMemories(context.Background(), nil, store, "anything", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3, got %d", len(files))
	}
}

func TestSelectRelevantMemoriesUsesLLMFilenames(t *testing.T) {
	store, err := memory.NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		_ = store.Save(memory.MemoryFile{
			Type: memory.TypeProject, Name: fmt.Sprintf("F%d", i),
			Description: "fact " + string(rune('a'+i)), Body: "body " + string(rune('a'+i)),
		})
	}
	files, _ := store.List()
	pick := files[2].Filename
	client := llm.NewFakeClient([]llm.Chunk{
		{Content: fmt.Sprintf(`["%s"]`, pick), Done: true},
	})
	selected, err := selectRelevantMemories(context.Background(), client, store, "query", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].Filename != pick {
		t.Fatalf("selected=%#v want %s", selected, pick)
	}
}

func TestSelectRelevantMemoriesFallsBackOnBadLLM(t *testing.T) {
	store, err := memory.NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		_ = store.Save(memory.MemoryFile{
			Type: memory.TypeProject, Name: fmt.Sprintf("Go%d", i),
			Description: "go fact " + string(rune('a'+i)), Body: "go body",
		})
	}
	client := llm.NewFakeClient([]llm.Chunk{{Content: `nope`, Done: true}})
	selected, err := selectRelevantMemories(context.Background(), client, store, "go fact", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) == 0 {
		t.Fatal("expected keyword fallback hits")
	}
}
