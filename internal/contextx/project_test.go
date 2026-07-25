package contextx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectProjectDetectsGoModule(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd", "app"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "app", "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}

	summary, err := InspectProject(root)
	if err != nil {
		t.Fatal(err)
	}

	if summary.Language != "go" {
		t.Fatalf("expected go language, got %q", summary.Language)
	}
	if summary.GoModule != "example.com/demo" {
		t.Fatalf("expected module example.com/demo, got %q", summary.GoModule)
	}
	if !summary.HasGit {
		t.Fatal("expected git project")
	}
	if !contains(summary.KeyFiles, "go.mod") {
		t.Fatalf("expected go.mod key file, got %#v", summary.KeyFiles)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
