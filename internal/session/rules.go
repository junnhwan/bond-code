package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/junnhwan/bond-code/internal/fsx"
	"github.com/junnhwan/bond-code/internal/safety"
)

// RuntimeRules are the per-session allow rules a user grants at a confirmation
// prompt via "Allow always" (Phase 5A). They are the runtime-writable sibling
// of the static Policy.Rules loaded from config yaml, and they live in
// <session-dir>/<id>.rules.json so a grant is O(1) and never touches the
// append-only audit log.
//
// Scoping is deliberate: rules are PER SESSION. A new session starts with
// none, so a grant made in one session never silently carries into another —
// trust does not creep across sessions. The TUI's "Allow always" choice is the
// only producer; Policy.Decide consumes them via the Policy.RuntimeRuleSource
// field (see safety/policy.go), evaluated with the same deny>ask>allow priority
// as configured rules.
//
// They are stored as safety.PermissionRule so the rule schema has a single
// source of truth — the exact type Policy.matchRule evaluates — and no lossy
// conversion is needed on the hot path.

// RulesPath returns the sidecar path for a session's runtime allow rules.
func (s *JSONLStore) RulesPath(sessionID string) string {
	return filepath.Join(s.dir, sessionID+".rules.json")
}

// LoadRuntimeRules reads a session's runtime allow rules. A missing file is not
// an error: it yields nil (no rules granted yet this session). A corrupt file
// is returned as an error so the bad data is left in place for inspection
// rather than silently overwritten.
func (s *JSONLStore) LoadRuntimeRules(sessionID string) ([]safety.PermissionRule, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.RulesPath(sessionID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rules []safety.PermissionRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, err
	}
	return rules, nil
}

// SaveRuntimeRules writes the full runtime rule set, creating the session dir
// if needed. An empty slice clears the sidecar so the session reverts to the
// "confirm everything" defaults.
func (s *JSONLStore) SaveRuntimeRules(sessionID string, rules []safety.PermissionRule) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	if len(rules) == 0 {
		if err := os.Remove(s.RulesPath(sessionID)); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(s.RulesPath(sessionID), append(data, '\n'), 0o600)
}

// AddRuntimeRule appends an allow rule to the session's runtime rules and
// returns the updated set. It is the persistence path used by RuleSource.Add;
// the TUI goes through RuleSource so the in-memory cache stays in sync. Dedup
// is by (tools, pattern, decision): re-granting the same rule is a no-op so
// the list stays bounded and idempotent under repeated grants.
func (s *JSONLStore) AddRuntimeRule(sessionID string, rule safety.PermissionRule) ([]safety.PermissionRule, error) {
	rules, err := s.LoadRuntimeRules(sessionID)
	if err != nil {
		return nil, err
	}
	if runtimeRuleExists(rules, rule) {
		return rules, nil
	}
	rules = append(rules, rule)
	if err := s.SaveRuntimeRules(sessionID, rules); err != nil {
		return nil, err
	}
	return rules, nil
}

// runtimeRuleExists reports whether an equivalent rule (same tools + pattern +
// decision) is already present, keeping the runtime rule set deduplicated.
func runtimeRuleExists(rules []safety.PermissionRule, target safety.PermissionRule) bool {
	for _, r := range rules {
		if runtimeRuleEqual(r, target) {
			return true
		}
	}
	return false
}

func runtimeRuleEqual(a, b safety.PermissionRule) bool {
	if len(a.Tools) != len(b.Tools) || a.Pattern != b.Pattern || a.Decision != b.Decision {
		return false
	}
	for i := range a.Tools {
		if a.Tools[i] != b.Tools[i] {
			return false
		}
	}
	return true
}

// RuleSource is the in-memory cache + persistence bridge for a session's
// runtime allow rules. It satisfies safety.RuntimeRuleSource (the read side
// Policy.Decide consults on every tool call) and offers Add (the write side
// the TUI's "Allow always" calls) and Reset (called on session switch so
// grants never leak across sessions).
//
// The cache is what makes the Decide hot path cheap — without it every tool
// call would re-read the sidecar. The sidecar remains the source of truth
// across restarts: a fresh RuleSource lazily loads it on first read.
type RuleSource struct {
	store     *JSONLStore
	sessionID string
	mu        sync.Mutex
	cached    []safety.PermissionRule
	loaded    bool
}

// NewRuleSource binds a cache to a session's rule sidecar. Rules are loaded
// lazily on first read, so constructing a source for a brand-new session is
// free.
func NewRuleSource(store *JSONLStore, sessionID string) *RuleSource {
	return &RuleSource{store: store, sessionID: sessionID}
}

// RuntimeAllowRules returns the cached allow rules, loading them from the
// session sidecar on first access. Policy.Decide calls this on every tool
// call, so the cache is what keeps it off the filesystem.
func (r *RuleSource) RuntimeAllowRules() []safety.PermissionRule {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.loaded {
		r.cached, _ = r.store.LoadRuntimeRules(r.sessionID)
		r.loaded = true
	}
	return r.cached
}

// Add persists a new allow rule to the session sidecar and refreshes the cache
// so the very next Decide sees it. Called by the TUI confirmer when the user
// picks "Allow always".
func (r *RuleSource) Add(rule safety.PermissionRule) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	updated, err := r.store.AddRuntimeRule(r.sessionID, rule)
	if err != nil {
		return err
	}
	r.cached = updated
	r.loaded = true
	return nil
}

// Reset rebinds the source to a different session and drops the cache so the
// next read loads the new session's rules. Called on session switch (/resume,
// /clear, switch-session) so an Allow-always grant from one session never
// carries into another.
func (r *RuleSource) Reset(sessionID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionID = sessionID
	r.cached = nil
	r.loaded = false
}

// SessionID returns the session this source is currently bound to.
func (r *RuleSource) SessionID() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessionID
}
