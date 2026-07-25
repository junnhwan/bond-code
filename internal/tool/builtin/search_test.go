package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchTextFindsKeywordWithLineNumber(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("first\nneedle here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "ignored.txt"), []byte("needle ignored"), 0o600); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(SearchTextInput{Path: dir, Pattern: "needle"})
	result, err := NewSearchTextTool().Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Output, "a.txt:2: needle here") {
		t.Fatalf("expected match with file and line, got %q", result.Output)
	}
	if strings.Contains(result.Output, "ignored.txt") {
		t.Fatalf("expected .git directory to be skipped, got %q", result.Output)
	}
}
