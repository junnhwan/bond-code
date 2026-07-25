// Package backendipc provides the loopback-only, task-bound control plane used
// by external terminal clients. Model and tool execution remain in the parent.
package backendipc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Identity struct {
	TaskID      string `json:"task_id"`
	SessionID   string `json:"session_id"`
	TeamID      string `json:"team_id,omitempty"`
	MemberID    string `json:"member_id,omitempty"`
	Generation  uint64 `json:"generation"`
	OwnershipID string `json:"ownership_id"`
}
type Launch struct{ Endpoint, TokenFile string }
type Lease struct {
	done          chan struct{}
	terminalSeen  chan struct{}
	authenticated atomic.Bool
}

func (l *Lease) Done() <-chan struct{}         { return l.done }
func (l *Lease) TerminalSeen() <-chan struct{} { return l.terminalSeen }
func (l *Lease) Authenticated() bool           { return l != nil && l.authenticated.Load() }

type outputLine struct {
	sequence uint64
	text     string
}
type record struct {
	identity       Identity
	launchHash     [32]byte
	launchConsumed bool
	sessionHash    [32]byte
	hasSession     bool
	expires        time.Time
	lease          *Lease
	state, detail  string
	sequence       uint64
	output         []outputLine
	tokenFile      string
}
type Supervisor struct {
	mu                 sync.Mutex
	records            map[string]*record
	ttl                time.Duration
	tokenDir, endpoint string
	server             *http.Server
	listener           net.Listener
	closed             chan struct{}
	closeOnce          sync.Once
	inputHandler       func(context.Context, Identity, string) error
}

func Start(tokenDir string, ttl time.Duration) (*Supervisor, error) {
	if ttl <= 0 {
		return nil, errors.New("lease TTL must be positive")
	}
	if err := os.MkdirAll(tokenDir, 0o700); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Supervisor{records: map[string]*record{}, ttl: ttl, tokenDir: tokenDir, endpoint: "http://" + listener.Addr().String(), listener: listener, closed: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/handshake", s.handleHandshake)
	mux.HandleFunc("/v1/task", s.handlePoll)
	mux.HandleFunc("/v1/input", s.handleInput)
	s.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = s.server.Serve(listener) }()
	go s.expireLoop()
	return s, nil
}
func (s *Supervisor) SetInputHandler(handler func(context.Context, Identity, string) error) {
	s.mu.Lock()
	s.inputHandler = handler
	s.mu.Unlock()
}

func (s *Supervisor) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.closed)
		err = s.server.Close()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, r := range s.records {
			s.expireLocked(r)
		}
	})
	return err
}
func (s *Supervisor) Prepare(identity Identity) (Launch, *Lease, error) {
	if err := validateIdentity(identity); err != nil {
		return Launch{}, nil, err
	}
	key := identityKey(identity)
	token, err := randomToken()
	if err != nil {
		return Launch{}, nil, err
	}
	tokenFile := filepath.Join(s.tokenDir, "launch-"+digest(key)+".token")
	if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o600); err != nil {
		return Launch{}, nil, err
	}
	lease := &Lease{done: make(chan struct{}), terminalSeen: make(chan struct{})}
	r := &record{identity: identity, launchHash: sha256.Sum256([]byte(token)), expires: time.Now().Add(s.ttl), lease: lease, state: "queued", tokenFile: tokenFile}
	s.mu.Lock()
	if _, exists := s.records[key]; exists {
		s.mu.Unlock()
		_ = os.Remove(tokenFile)
		return Launch{}, nil, fmt.Errorf("task generation already prepared")
	}
	s.records[key] = r
	s.mu.Unlock()
	return Launch{Endpoint: s.endpoint, TokenFile: tokenFile}, lease, nil
}
func (s *Supervisor) Release(identity Identity) {
	s.mu.Lock()
	record := s.records[identityKey(identity)]
	if record != nil && record.identity == identity {
		delete(s.records, identityKey(identity))
		s.expireLocked(record)
	}
	s.mu.Unlock()
	if record != nil {
		_ = os.Remove(record.tokenFile)
	}
}

func (s *Supervisor) Publish(identity Identity, state, output, detail string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.records[identityKey(identity)]
	if r == nil || r.identity != identity {
		return errors.New("task generation is not prepared")
	}
	if output != "" {
		r.sequence++
		r.output = append(r.output, outputLine{r.sequence, output})
		if len(r.output) > 256 {
			r.output = append([]outputLine(nil), r.output[len(r.output)-256:]...)
		}
	}
	r.state = state
	r.detail = detail
	return nil
}
func (s *Supervisor) handleHandshake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !isLoopback(r.RemoteAddr) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	token, ok := bearer(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var identity Identity
	if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&identity) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	rec := s.records[identityKey(identity)]
	if rec == nil || rec.identity != identity || rec.launchConsumed || rec.launchHash != sha256.Sum256([]byte(token)) || time.Now().After(rec.expires) {
		s.mu.Unlock()
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	session, err := randomToken()
	if err != nil {
		s.mu.Unlock()
		http.Error(w, "internal error", 500)
		return
	}
	rec.launchConsumed = true
	rec.sessionHash = sha256.Sum256([]byte(session))
	rec.hasSession = true
	rec.expires = time.Now().Add(s.ttl)
	rec.lease.authenticated.Store(true)
	expires := rec.expires
	s.mu.Unlock()
	writeJSON(w, map[string]any{"session_token": session, "lease_expires_at": expires, "poll_after_ms": max(1, int((s.ttl/3)/time.Millisecond))})
}
func (s *Supervisor) handlePoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || !isLoopback(r.RemoteAddr) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	token, ok := bearer(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	hash := sha256.Sum256([]byte(token))
	cursor, _ := strconv.ParseUint(r.URL.Query().Get("cursor"), 10, 64)
	s.mu.Lock()
	var rec *record
	for _, candidate := range s.records {
		if candidate.hasSession && candidate.sessionHash == hash {
			rec = candidate
			break
		}
	}
	if rec == nil || time.Now().After(rec.expires) {
		s.mu.Unlock()
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rec.expires = time.Now().Add(s.ttl)
	out := []string{}
	for _, line := range rec.output {
		if line.sequence > cursor {
			out = append(out, line.text)
		}
	}
	terminal := rec.state == "completed" || rec.state == "failed" || rec.state == "canceled" || rec.state == "interrupted"
	if terminal {
		select {
		case <-rec.lease.terminalSeen:
		default:
			close(rec.lease.terminalSeen)
		}
	}
	response := map[string]any{"state": rec.state, "detail": rec.detail, "cursor": rec.sequence, "output": out, "lease_expires_at": rec.expires}
	s.mu.Unlock()
	writeJSON(w, response)
}
func (s *Supervisor) handleInput(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || !isLoopback(request.RemoteAddr) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	token, ok := bearer(request)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var payload struct {
		Input string `json:"input"`
	}
	if json.NewDecoder(io.LimitReader(request.Body, 64<<10)).Decode(&payload) != nil || strings.TrimSpace(payload.Input) == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	hash := sha256.Sum256([]byte(token))
	s.mu.Lock()
	var rec *record
	for _, candidate := range s.records {
		if candidate.hasSession && candidate.sessionHash == hash {
			rec = candidate
			break
		}
	}
	if rec == nil || time.Now().After(rec.expires) {
		s.mu.Unlock()
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rec.expires = time.Now().Add(s.ttl)
	identity := rec.identity
	handler := s.inputHandler
	s.mu.Unlock()
	if handler == nil {
		http.Error(w, "input unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := handler(request.Context(), identity, payload.Input); err != nil {
		http.Error(w, "input rejected", http.StatusConflict)
		return
	}
	writeJSON(w, map[string]bool{"accepted": true})
}

func (s *Supervisor) expireLoop() {
	ticker := time.NewTicker(maxDuration(5*time.Millisecond, s.ttl/4))
	defer ticker.Stop()
	for {
		select {
		case <-s.closed:
			return
		case now := <-ticker.C:
			s.mu.Lock()
			for _, r := range s.records {
				if now.After(r.expires) {
					s.expireLocked(r)
				}
			}
			s.mu.Unlock()
		}
	}
}
func (s *Supervisor) expireLocked(r *record) {
	select {
	case <-r.lease.done:
	default:
		close(r.lease.done)
	}
}
func validateIdentity(i Identity) error {
	if strings.TrimSpace(i.TaskID) == "" || strings.TrimSpace(i.SessionID) == "" || i.Generation == 0 || strings.TrimSpace(i.OwnershipID) == "" {
		return errors.New("complete task identity and ownership are required")
	}
	return nil
}
func identityKey(i Identity) string { return i.TaskID + "\x00" + strconv.FormatUint(i.Generation, 10) }
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func digest(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:8]) }
func bearer(r *http.Request) (string, bool) {
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	return token, token != ""
}
func isLoopback(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
