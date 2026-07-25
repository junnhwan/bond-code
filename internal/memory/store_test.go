package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndReadTopicWithIndex(t *testing.T) {
	store, err := NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = store.Save(MemoryFile{
		Type:        TypeFeedback,
		Name:        "Testing policy",
		Description: "Integration tests must hit a real database",
		Body:        "Do not mock the database in these tests.\n\n**Why:** prior mock/prod divergence.\n\n**How to apply:** use real DB in integration tests.",
	})
	if err != nil {
		t.Fatal(err)
	}

	files, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 topic file, got %d", len(files))
	}
	if files[0].Type != TypeFeedback {
		t.Fatalf("type = %s", files[0].Type)
	}
	if !strings.Contains(files[0].Body, "Do not mock") {
		t.Fatalf("body = %q", files[0].Body)
	}

	index, err := store.GetMemoryContext()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(index, "Testing policy") || !strings.Contains(index, ".md") {
		t.Fatalf("index missing entry:\n%s", index)
	}

	// Upsert same topic via explicit filename.
	name := files[0].Filename
	err = store.Save(MemoryFile{
		Filename:    name,
		Type:        TypeFeedback,
		Name:        "Testing policy",
		Description: "Integration tests must hit a real database (updated)",
		Body:        "Updated body.",
	})
	if err != nil {
		t.Fatal(err)
	}
	files, _ = store.List()
	if len(files) != 1 {
		t.Fatalf("expected still 1 file after update, got %d", len(files))
	}
	if files[0].Body != "Updated body." {
		t.Fatalf("body not updated: %q", files[0].Body)
	}
}

func TestSearchRanksDescription(t *testing.T) {
	store, err := NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Save(MemoryFile{Type: TypeUser, Name: "Role", Description: "data scientist focused on logging", Body: "User is a data scientist."})
	_ = store.Save(MemoryFile{Type: TypeProject, Name: "Freeze", Description: "mobile release merge freeze", Body: "Merge freeze next week."})

	hits, err := store.Search(SearchOptions{Query: "logging scientist", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Type != TypeUser {
		t.Fatalf("expected user memory first, got %#v", hits)
	}
}

func TestDeleteRemovesFileAndIndex(t *testing.T) {
	store, err := NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Save(MemoryFile{Type: TypeReference, Name: "Linear", Description: "bugs in INGEST", Body: "Pipeline bugs in Linear INGEST."})
	files, _ := store.List()
	if len(files) != 1 {
		t.Fatal(files)
	}
	filename := files[0].Filename
	if err := store.Delete(filename); err != nil {
		t.Fatal(err)
	}
	files, _ = store.List()
	if len(files) != 0 {
		t.Fatalf("expected empty, got %d", len(files))
	}
	index, _ := store.GetMemoryContext()
	if strings.Contains(index, filename) {
		t.Fatalf("index still has file: %s", index)
	}
}

func TestRebuildIndex(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMemoryStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Save(MemoryFile{Type: TypeUser, Name: "A", Description: "alpha", Body: "a"})
	_ = store.Save(MemoryFile{Type: TypeProject, Name: "B", Description: "beta", Body: "b"})
	// Corrupt index.
	if err := os.WriteFile(filepath.Join(store.Dir(), EntrypointName), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := store.RebuildIndex()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Fatalf("rebuild missing entries:\n%s", out)
	}
}

func TestModelWriteFlag(t *testing.T) {
	store, err := NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if store.ConsumeModelWrite() {
		t.Fatal("expected false initially")
	}
	store.MarkModelWrite()
	if !store.ConsumeModelWrite() {
		t.Fatal("expected true after mark")
	}
	if store.ConsumeModelWrite() {
		t.Fatal("expected cleared")
	}
}

func TestGuidancePromptMentionsTaxonomy(t *testing.T) {
	g := GuidancePrompt("/tmp/memory")
	for _, want := range []string{"user", "feedback", "project", "reference", "What NOT to save", "memory_save"} {
		if !strings.Contains(g, want) {
			t.Fatalf("guidance missing %q", want)
		}
	}
}

func TestComposeInjection(t *testing.T) {
	out := ComposeInjection("- [A](a.md) — hook", []MemoryFile{{
		Filename: "a.md", Type: TypeUser, Description: "hook", Body: "body", MtimeMs: 0,
	}}, 4000)
	if !strings.Contains(out, "MEMORY.md") || !strings.Contains(out, "Relevant memories") || !strings.Contains(out, "body") {
		t.Fatalf("injection:\n%s", out)
	}
}

func TestMemorySaveTool(t *testing.T) {
	store, err := NewMemoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tool := NewMemorySaveTool(store)
	res, err := tool.Execute(t.Context(), []byte(`{"type":"user","name":"Role","description":"Go expert new to React","content":"Deep Go, new to React frontend."}`))
	if err != nil || res == nil || !res.OK {
		t.Fatalf("save failed: err=%v res=%#v", err, res)
	}
	if !store.ConsumeModelWrite() {
		t.Fatal("expected model write flag")
	}
	n, _ := store.Count()
	if n != 1 {
		t.Fatalf("count=%d", n)
	}
}
