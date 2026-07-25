package contextx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolResultStoreSavesOutputUnderSessionPath(t *testing.T) {
	dataDir := t.TempDir()
	store := NewToolResultStore(dataDir, "session-test")

	displayPath, err := store.Save("call-1", "full output")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(filepath.ToSlash(displayPath), "tool-results/session-test/call-1.txt") {
		t.Fatalf("expected display path to include tool result components, got %q", displayPath)
	}
	content, err := os.ReadFile(filepath.Join(dataDir, "tool-results", "session-test", "call-1.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "full output" {
		t.Fatalf("expected stored output, got %q", string(content))
	}
}
