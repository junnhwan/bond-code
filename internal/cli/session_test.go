package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/session"
)

func TestSessionListShowAndDeleteCommands(t *testing.T) {
	dir := t.TempDir()
	sessionID := "demo"
	sessionPath := filepath.Join(dir, sessionID+".jsonl")
	writeSessionFixture(t, sessionPath)

	list := NewRootCommand()
	var listOut bytes.Buffer
	list.SetOut(&listOut)
	list.SetErr(&listOut)
	list.SetArgs([]string{"session", "--dir", dir, "list"})
	if err := list.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listOut.String(), sessionID) {
		t.Fatalf("expected session id in list output, got %q", listOut.String())
	}

	show := NewRootCommand()
	var showOut bytes.Buffer
	show.SetOut(&showOut)
	show.SetErr(&showOut)
	show.SetArgs([]string{"session", "--dir", dir, "show", sessionID})
	if err := show.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(showOut.String(), "hello") {
		t.Fatalf("expected event content in show output, got %q", showOut.String())
	}

	del := NewRootCommand()
	del.SetArgs([]string{"session", "--dir", dir, "delete", sessionID})
	if err := del.Execute(); err != nil {
		t.Fatal(err)
	}

	listAgain := NewRootCommand()
	var after bytes.Buffer
	listAgain.SetOut(&after)
	listAgain.SetErr(&after)
	listAgain.SetArgs([]string{"session", "--dir", dir, "list"})
	if err := listAgain.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(after.String(), sessionID) {
		t.Fatalf("expected deleted session to be absent, got %q", after.String())
	}
}

func TestSessionForkCommand(t *testing.T) {
	dir := t.TempDir()
	store := session.NewJSONLStore(dir)
	if err := store.Append(session.Event{SessionID: "base", Type: "message", Message: &session.Message{Role: session.RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("append: %v", err)
	}

	cmd := newSessionCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--dir", dir, "fork", "base", "forked"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute fork: %v", err)
	}
	if !strings.Contains(out.String(), "forked base -> forked") {
		t.Fatalf("unexpected output %q", out.String())
	}
}

func writeSessionFixture(t *testing.T, path string) {
	t.Helper()
	content := `{"session_id":"demo","type":"message","message":{"role":"user","content":"hello","created_at":"2026-06-01T00:00:00Z"},"created_at":"2026-06-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
