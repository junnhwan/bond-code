package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeepInResumeList(t *testing.T) {
	cases := []struct {
		name   string
		active bool
		pinned bool
		title  string
		count  int
		want   bool
	}{
		{name: "empty abandoned", want: false},
		{name: "has user messages", count: 1, want: true},
		{name: "active empty", active: true, want: true},
		{name: "pinned empty", pinned: true, want: true},
		{name: "renamed empty", title: "my branch", want: true},
		{name: "whitespace title only", title: "   ", want: false},
	}
	for _, tc := range cases {
		if got := KeepInResumeList(tc.active, tc.pinned, tc.title, tc.count); got != tc.want {
			t.Fatalf("%s: KeepInResumeList = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestMetaSidecarRoundTrip covers Load/Save semantics: a missing file is the
// zero Meta (not an error), a write round-trips, and an empty write clears the
// sidecar so the session reverts to derived defaults.
func TestMetaSidecarRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewJSONLStore(dir)
	id := "test-session-20260702"

	if m, err := store.LoadMeta(id); err != nil || m.Pinned || m.Title != "" {
		t.Fatalf("expected zero meta for new session, got %+v err=%v", m, err)
	}
	if err := store.SaveMeta(id, Meta{Title: "my title", Pinned: true}); err != nil {
		t.Fatalf("save: %v", err)
	}
	m, err := store.LoadMeta(id)
	if err != nil {
		t.Fatalf("load after save: %v", err)
	}
	if m.Title != "my title" || !m.Pinned {
		t.Fatalf("round-trip mismatch: %+v", m)
	}
	// Empty meta clears the sidecar.
	if err := store.SaveMeta(id, Meta{}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if m, _ := store.LoadMeta(id); m.Title != "" || m.Pinned {
		t.Fatalf("expected cleared meta, got %+v", m)
	}
}

// TestMetaSidecarSurvivesUnrelatedSession ensures sidecars do not bleed across
// sessions — each id owns its own file.
func TestMetaSidecarSurvivesUnrelatedSession(t *testing.T) {
	dir := t.TempDir()
	store := NewJSONLStore(dir)
	_ = store.SaveMeta("session-alpha", Meta{Title: "alpha", Pinned: true})
	_ = store.SaveMeta("session-beta", Meta{Title: "beta"})
	a, _ := store.LoadMeta("session-alpha")
	b, _ := store.LoadMeta("session-beta")
	if a.Title != "alpha" || !a.Pinned {
		t.Fatalf("alpha meta wrong: %+v", a)
	}
	if b.Title != "beta" || b.Pinned {
		t.Fatalf("beta meta wrong: %+v", b)
	}
}

// TestDeleteClearsMetaSidecar verifies Delete removes the sidecar too, so a
// reused id (or just a clean slate) does not inherit stale metadata.
func TestDeleteClearsMetaSidecar(t *testing.T) {
	dir := t.TempDir()
	store := NewJSONLStore(dir)
	id := "session-gone"
	_ = store.SaveMeta(id, Meta{Title: "stale", Pinned: true})
	// The audit log must exist for Delete to mean something; create it.
	if err := store.Append(Event{SessionID: id, Type: "message"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.LoadMeta(id); err != nil {
		t.Fatalf("LoadMeta should tolerate a deleted sidecar, got %v", err)
	}
	// Sidecar file is gone.
	if _, err := os.ReadFile(filepath.Join(dir, id+".meta.json")); err == nil {
		t.Fatal("expected meta sidecar to be removed by Delete")
	}
}
