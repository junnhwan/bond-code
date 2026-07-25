package contextx

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestExpandPathMentionsAddsFileContextWithinRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello bond"), 0o600); err != nil {
		t.Fatal(err)
	}

	expanded := ExpandPathMentions("read @README.md", root)

	for _, want := range []string{"read @README.md", `<file path="README.md">`, "hello bond", "</file>"} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("expanded prompt missing %q:\n%s", want, expanded)
		}
	}
}

func TestExpandPathMentionsAddsFileLineRangeContext(t *testing.T) {
	root := t.TempDir()
	body := strings.Join([]string{"line one", "line two", "line three", "line four"}, "\n")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	expanded := ExpandPathMentions("read @README.md:2-3", root)

	for _, want := range []string{`read @README.md:2-3`, `<file path="README.md" lines="2-3">`, "line two", "line three", "</file>"} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("expanded line range missing %q:\n%s", want, expanded)
		}
	}
	for _, notWant := range []string{"line one", "line four"} {
		if strings.Contains(expanded, notWant) {
			t.Fatalf("line range should not include %q:\n%s", notWant, expanded)
		}
	}
}

func TestExpandPathMentionsAddsSingleFileLineContext(t *testing.T) {
	root := t.TempDir()
	body := strings.Join([]string{"first", "second", "third"}, "\n")
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	expanded := ExpandPathMentions("read @notes.txt:2", root)

	if !strings.Contains(expanded, `<file path="notes.txt" lines="2">`) || !strings.Contains(expanded, "second") {
		t.Fatalf("expected single line context, got:\n%s", expanded)
	}
	if strings.Contains(expanded, "first") || strings.Contains(expanded, "third") {
		t.Fatalf("single line context should not include adjacent lines:\n%s", expanded)
	}
}

func TestExpandPathMentionsSupportsAngledLineRangeContext(t *testing.T) {
	root := t.TempDir()
	body := strings.Join([]string{"alpha", "beta", "gamma"}, "\n")
	if err := os.WriteFile(filepath.Join(root, "My Notes.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	expanded := ExpandPathMentions("read @<My Notes.md:2-3>", root)

	if !strings.Contains(expanded, `<file path="My Notes.md" lines="2-3">`) || !strings.Contains(expanded, "beta") || !strings.Contains(expanded, "gamma") {
		t.Fatalf("expected angled line range context, got:\n%s", expanded)
	}
	if strings.Contains(expanded, "alpha") {
		t.Fatalf("angled line range should not include excluded line:\n%s", expanded)
	}
}

func TestExpandPathMentionsAfterPunctuationBoundary(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello punctuation"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, input := range []string{`read "@README.md"`, `inspect (@README.md)`, `see:@README.md`} {
		expanded := ExpandPathMentions(input, root)
		if !strings.Contains(expanded, `<file path="README.md">`) || !strings.Contains(expanded, "hello punctuation") {
			t.Fatalf("expected punctuation-adjacent mention to expand for %q, got:\n%s", input, expanded)
		}
	}
}

func TestExpandPathMentionsIgnoresEmbeddedAtWords(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello bond"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, input := range []string{`email me@example.com`, `literalfoo@README.md`} {
		expanded := ExpandPathMentions(input, root)
		if strings.Contains(expanded, `<file path=`) {
			t.Fatalf("embedded @ should not expand for %q, got:\n%s", input, expanded)
		}
	}
}

func TestExpandPathMentionsAddsDirectoryListingWithinRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}

	expanded := ExpandPathMentions("inspect @src", root)

	for _, want := range []string{`<directory path="src">`, "- main.go", "</directory>"} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("expanded prompt missing %q:\n%s", want, expanded)
		}
	}
}

func TestExpandPathMentionsDoesNotApplyLineRangeToDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}

	expanded := ExpandPathMentions("inspect @src:2", root)

	if expanded != "inspect @src:2" {
		t.Fatalf("directory line range should remain unexpanded, got:\n%s", expanded)
	}
}

func TestExpandPathMentionsRefusesEscapedPaths(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	expanded := ExpandPathMentions("read @"+secret, root)

	if strings.Contains(expanded, "<file") {
		t.Fatalf("expected outside path to remain unexpanded, got:\n%s", expanded)
	}
}

func TestExpandPathMentionsOmitsBinaryAndTruncatesLargeFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bin.dat"), []byte{0, 1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(strings.Repeat("a", MaxMentionFileBytes+10)), 0o600); err != nil {
		t.Fatal(err)
	}

	binary := ExpandPathMentions("read @bin.dat", root)
	if !strings.Contains(binary, `binary="true"`) || !strings.Contains(binary, "binary omitted") {
		t.Fatalf("expected binary marker, got:\n%s", binary)
	}
	large := ExpandPathMentions("read @large.txt", root)
	if !strings.Contains(large, "file truncated") || len(large) > MaxMentionFileBytes+1024 {
		t.Fatalf("expected large file to be truncated, len=%d", len(large))
	}
}

func TestExpandPathMentionsLimitsDirectoryEntries(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "many")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxMentionDirectoryEntries+5; i++ {
		name := filepath.Join(dir, "file"+strconv.Itoa(i)+".txt")
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	expanded := ExpandPathMentions("list @many", root)

	if strings.Count(expanded, "- file") != MaxMentionDirectoryEntries {
		t.Fatalf("expected %d listed entries, got:\n%s", MaxMentionDirectoryEntries, expanded)
	}
	if !strings.Contains(expanded, "directory truncated") {
		t.Fatalf("expected directory truncation marker, got:\n%s", expanded)
	}
}
