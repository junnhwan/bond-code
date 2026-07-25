package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicCreatesAndOverwritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := WriteFileAtomic(path, []byte("first"), 0o600); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := WriteFileAtomic(path, []byte("replacement"), 0o600); err != nil {
		t.Fatalf("overwrite file: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != "replacement" {
		t.Fatalf("unexpected content %q", got)
	}
	assertNoTemporaryFiles(t, dir)
}

func TestWriteFileAtomicRemovesTemporaryFileWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "existing-directory")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := WriteFileAtomic(target, []byte("content"), 0o600); err == nil {
		t.Fatal("expected replacing a directory to fail")
	}

	assertNoTemporaryFiles(t, dir)
}

func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".*.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files leaked: %v", matches)
	}
}
