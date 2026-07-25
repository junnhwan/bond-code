package session

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJSONLStoreListExcludesDebugAndSidecarJSONL(t *testing.T) {
	dir := t.TempDir()
	store := NewJSONLStore(dir)

	mustWrite := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Real sessions (with and without messages).
	mustWrite("session-real.jsonl", `{"session_id":"session-real","type":"message"}`+"\n")
	mustWrite("session-empty.jsonl", "")
	// Sidecars / noise that previously polluted /resume as "(no messages)".
	mustWrite("session-real.debug.jsonl", `{"type":"llm_req"}`+"\n")
	mustWrite("session-orphan.debug.jsonl", `{"type":"llm_req"}`+"\n")
	mustWrite("session-real.history.json", `[]`)
	mustWrite("session-real.meta.json", `{"title":"x"}`)
	mustWrite("not-a-session.txt", "x")
	// Timestamp-shaped production id (fractional seconds include a dot).
	mustWrite("session-20260725-010203.000000001.jsonl", "{}\n")

	ids, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"session-real":                      true,
		"session-empty":                     true,
		"session-20260725-010203.000000001": true,
	}
	if len(ids) != len(want) {
		t.Fatalf("List() = %v, want only real session ids %v", ids, keys(want))
	}
	for _, id := range ids {
		if !want[id] {
			t.Fatalf("List() unexpectedly included %q: %v", id, ids)
		}
	}
}

func TestJSONLStoreDeleteRemovesSidecars(t *testing.T) {
	dir := t.TempDir()
	store := NewJSONLStore(dir)
	id := "session-cleanup"
	if err := store.Append(Event{SessionID: id, Type: "message", Message: &Message{Role: RoleUser, Content: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteHistory(id, []byte(`[]`)); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMeta(id, Meta{Title: "t", Pinned: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.DebugPath(id), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.Delete(id); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{store.path(id), store.MetaPath(id), store.HistoryPath(id), store.DebugPath(id)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed, stat err=%v", p, err)
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestSessionIDFromJSONLName(t *testing.T) {
	cases := []struct {
		name string
		id   string
		ok   bool
	}{
		{"session-20260725-010203.000000001.jsonl", "session-20260725-010203.000000001", true},
		{"session-test.jsonl", "session-test", true},
		{"session-test.debug.jsonl", "", false},
		{"notes.txt", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		id, ok := sessionIDFromJSONLName(tc.name)
		if ok != tc.ok || id != tc.id {
			t.Fatalf("sessionIDFromJSONLName(%q) = (%q, %v), want (%q, %v)", tc.name, id, ok, tc.id, tc.ok)
		}
	}
}

func TestJSONLStoreAppendAndLoadEventsBySession(t *testing.T) {
	store := NewJSONLStore(t.TempDir())
	sessionID := "session-test"
	first := Event{
		SessionID: sessionID,
		Type:      "message",
		Message:   &Message{Role: RoleUser, Content: "hello", CreatedAt: time.Unix(1, 0)},
		CreatedAt: time.Unix(1, 0),
	}
	second := Event{
		SessionID: sessionID,
		Type:      "message",
		Message:   &Message{Role: RoleAssistant, Content: "hi", CreatedAt: time.Unix(2, 0)},
		CreatedAt: time.Unix(2, 0),
	}

	if err := store.Append(first); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(second); err != nil {
		t.Fatal(err)
	}

	events, err := store.Load(sessionID)
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Message.Content != "hello" || events[1].Message.Content != "hi" {
		t.Fatalf("events not loaded in append order: %+v", events)
	}
}

func TestJSONLStoreHistoryRoundTrip(t *testing.T) {
	store := NewJSONLStore(t.TempDir())
	id := "session-resume"

	// Missing snapshot is not an error and yields nil.
	data, err := store.ReadHistory(id)
	if err != nil {
		t.Fatalf("ReadHistory on missing snapshot should not error: %v", err)
	}
	if data != nil {
		t.Fatalf("expected nil data for missing snapshot, got %q", data)
	}

	payload := []byte(`[{"role":"user","content":"hi"}]`)
	if err := store.WriteHistory(id, payload); err != nil {
		t.Fatalf("WriteHistory: %v", err)
	}
	got, err := store.ReadHistory(id)
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("history round-trip mismatch: got %q want %q", got, payload)
	}
}

func TestNewEventIDConcurrentUnique(t *testing.T) {
	const (
		workers      = 128
		idsPerWorker = 1000
	)

	ids := make(chan string, workers*idsPerWorker)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for range idsPerWorker {
				ids <- newEventID()
			}
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[string]struct{}, workers*idsPerWorker)
	for id := range ids {
		if !strings.HasPrefix(id, "evt_") {
			t.Fatalf("event ID %q lost the evt_ compatibility prefix", id)
		}
		if _, err := strconv.ParseInt(strings.TrimPrefix(id, "evt_"), 10, 64); err != nil {
			t.Fatalf("event ID %q lost the numeric compatibility format: %v", id, err)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate event ID generated concurrently: %s", id)
		}
		seen[id] = struct{}{}
	}
	if got, want := len(seen), workers*idsPerWorker; got != want {
		t.Fatalf("generated %d unique event IDs, want %d", got, want)
	}
}
