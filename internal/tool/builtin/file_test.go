package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/tool"
)

func TestReadFileReadsSmallFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello file"), 0o600); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(ReadFileInput{Path: path})
	result, err := NewReadFileTool().Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	if !result.OK || !strings.Contains(result.Output, "hello file") {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestReadFileRejectsMissingPath(t *testing.T) {
	input, _ := json.Marshal(ReadFileInput{})
	_, err := NewReadFileTool().Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected missing path error")
	}
}

func TestWriteFileWritesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	input, _ := json.Marshal(WriteFileInput{Path: path, Content: "written"})

	result, err := NewWriteFileTool().Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || string(b) != "written" {
		t.Fatalf("unexpected write result: %+v content=%q", result, string(b))
	}
	if risk := NewWriteFileTool().Risk(input); risk != tool.RiskMedium {
		t.Fatalf("write_file risk = %s", risk)
	}
}

func TestListDirReturnsChildNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(ListDirInput{Path: dir, Depth: 1})
	result, err := NewListDirTool().Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Output, "a.txt") || !strings.Contains(result.Output, "nested") {
		t.Fatalf("expected child names in output, got %q", result.Output)
	}
}
