package agenttask

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/junnhwan/bond-code/internal/fsx"
)

var (
	ErrTaskNotFound      = errors.New("agent task not found")
	ErrInvalidTransition = errors.New("invalid agent task transition")
	ErrStaleGeneration   = errors.New("stale agent task generation")
	ErrStaleLease        = errors.New("stale agent task runtime lease")
	ErrCorruptLedger     = errors.New("corrupt agent task ledger")
)

const ledgerSchemaVersion = 1

type diskState struct {
	SchemaVersion int               `json:"schema_version"`
	Lease         string            `json:"lease"`
	Sequence      uint64            `json:"sequence"`
	Tasks         map[string]Task   `json:"tasks"`
	Idempotency   map[string]string `json:"idempotency"`
	Events        []Event           `json:"events"`
}
type envelope struct {
	Payload  json.RawMessage `json:"payload"`
	Checksum string          `json:"checksum"`
}
type Ledger struct {
	mu    sync.Mutex
	path  string
	state diskState
}

func Open(path, lease string) (*Ledger, error) {
	if path == "" || lease == "" {
		return nil, fmt.Errorf("ledger path and runtime lease are required")
	}
	l := &Ledger{path: path, state: diskState{SchemaVersion: ledgerSchemaVersion, Lease: lease, Tasks: map[string]Task{}, Idempotency: map[string]string{}}}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err = l.decode(data); err != nil {
			_ = quarantine(path)
			return nil, err
		}
		if l.state.Lease != lease {
			l.state.Lease = lease
			now := time.Now().UTC()
			for id, task := range l.state.Tasks {
				if !task.State.terminal() && task.State != StateInterrupted {
					task.State = StateInterrupted
					task.RuntimeLease = lease
					task.UpdatedAt = now
					task.EndedAt = &now
					l.appendEventLocked(&task, now)
					l.state.Tasks[id] = task
				}
			}
			if err = l.persistLocked(); err != nil {
				return nil, err
			}
		}
	}
	return l, nil
}
func (l *Ledger) Create(in CreateInput) (Task, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if in.IdempotencyKey == "" {
		return Task{}, false, fmt.Errorf("idempotency key is required")
	}
	if id := l.state.Idempotency["create:"+in.IdempotencyKey]; id != "" {
		return l.state.Tasks[id], false, nil
	}
	if in.Kind == "" {
		in.Kind = KindAgent
	}
	if in.Mode == "" {
		in.Mode = ModeForeground
	}
	before := cloneDiskState(l.state)
	now := time.Now().UTC()
	task := Task{ID: newID("task"), Kind: in.Kind, Mode: in.Mode, State: StateQueued, SessionID: in.SessionID, OwnerID: in.OwnerID, TeamID: in.TeamID, MemberID: in.MemberID, Description: in.Description, Prompt: in.Prompt, Profile: in.Profile, Backend: in.Backend, BackendOwnershipID: in.BackendOwnershipID, Generation: 1, RuntimeLease: l.state.Lease, CreatedAt: now, UpdatedAt: now}
	l.appendEventLocked(&task, now)
	l.state.Tasks[task.ID] = task
	l.state.Idempotency["create:"+in.IdempotencyKey] = task.ID
	if err := l.persistLocked(); err != nil {
		l.state = before
		return Task{}, false, err
	}
	return task, true, nil
}
func (l *Ledger) Get(id string) (Task, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	task, ok := l.state.Tasks[id]
	return cloneTask(task), ok
}
func (l *Ledger) List(sessionID string) []Task {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Task, 0, len(l.state.Tasks))
	for _, task := range l.state.Tasks {
		if sessionID == "" || task.SessionID == sessionID {
			out = append(out, cloneTask(task))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}
func (l *Ledger) Transition(id string, generation uint64, lease string, next State, result ResultRef) (Task, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	task, ok := l.state.Tasks[id]
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	if generation != task.Generation {
		return Task{}, ErrStaleGeneration
	}
	if lease != task.RuntimeLease || lease != l.state.Lease {
		return Task{}, ErrStaleLease
	}
	if !validTransition(task.State, next) {
		return Task{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, task.State, next)
	}
	before := cloneDiskState(l.state)
	now := time.Now().UTC()
	task.State = next
	task.Result = result
	task.UpdatedAt = now
	if next == StateRunning && task.StartedAt == nil {
		task.StartedAt = &now
	}
	if next.terminal() || next == StateInterrupted {
		task.EndedAt = &now
	}
	l.appendEventLocked(&task, now)
	l.state.Tasks[id] = task
	if err := l.persistLocked(); err != nil {
		l.state = before
		return Task{}, err
	}
	return cloneTask(task), nil
}
func (l *Ledger) Resume(id, key string, prompts ...string) (Task, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	task, ok := l.state.Tasks[id]
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	command := "resume:" + id + ":" + key
	if existing := l.state.Idempotency[command]; existing != "" {
		return cloneTask(l.state.Tasks[existing]), nil
	}
	if !(task.State.terminal() || task.State == StateInterrupted) {
		return Task{}, fmt.Errorf("%w: cannot resume %s", ErrInvalidTransition, task.State)
	}
	before := cloneDiskState(l.state)
	now := time.Now().UTC()
	task.Generation++
	task.State = StateQueued
	task.RuntimeLease = l.state.Lease
	task.UpdatedAt = now
	task.StartedAt = nil
	task.EndedAt = nil
	task.Result = ResultRef{}
	if len(prompts) > 0 {
		task.Prompt = prompts[0]
	}
	l.appendEventLocked(&task, now)
	l.state.Tasks[id] = task
	l.state.Idempotency[command] = id
	if err := l.persistLocked(); err != nil {
		l.state = before
		return Task{}, err
	}
	return cloneTask(task), nil
}
func (l *Ledger) Events(after uint64) []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Event
	for _, e := range l.state.Events {
		if e.Sequence > after {
			out = append(out, e)
		}
	}
	return out
}
func (l *Ledger) appendEventLocked(task *Task, at time.Time) {
	l.state.Sequence++
	task.EventSequence = l.state.Sequence
	l.state.Events = append(l.state.Events, Event{ID: fmt.Sprintf("event-%d", l.state.Sequence), Sequence: l.state.Sequence, TaskID: task.ID, Generation: task.Generation, RuntimeLease: task.RuntimeLease, State: task.State, At: at})
}
func (l *Ledger) persistLocked() error {
	payload, err := json.Marshal(l.state)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(payload)
	data, err := json.Marshal(envelope{Payload: payload, Checksum: hex.EncodeToString(sum[:])})
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	return fsx.WriteFileAtomic(l.path, data, 0o600)
}
func (l *Ledger) decode(data []byte) error {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptLedger, err)
	}
	sum := sha256.Sum256(env.Payload)
	if env.Checksum != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("%w: checksum mismatch", ErrCorruptLedger)
	}
	if err := json.Unmarshal(env.Payload, &l.state); err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptLedger, err)
	}
	if l.state.SchemaVersion != ledgerSchemaVersion {
		return fmt.Errorf("%w: schema %d", ErrCorruptLedger, l.state.SchemaVersion)
	}
	if l.state.Tasks == nil {
		l.state.Tasks = map[string]Task{}
	}
	if l.state.Idempotency == nil {
		l.state.Idempotency = map[string]string{}
	}
	return nil
}
func validTransition(from, to State) bool {
	switch from {
	case StateQueued:
		return to == StateRunning || to == StateCanceled || to == StateInterrupted
	case StateRunning:
		return to == StateWaiting || to == StateCompleted || to == StateFailed || to == StateCanceled || to == StateInterrupted
	case StateWaiting:
		return to == StateRunning || to == StateCompleted || to == StateFailed || to == StateCanceled || to == StateInterrupted
	}
	return false
}
func quarantine(path string) error {
	return os.Rename(path, path+".corrupt-"+time.Now().UTC().Format("20060102T150405.000000000"))
}
func newID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}
func cloneTask(t Task) Task { t.LegacyAliases = append([]string(nil), t.LegacyAliases...); return t }

// RuntimeLease returns the lease fencing mutations in this process.
func (l *Ledger) RuntimeLease() string { l.mu.Lock(); defer l.mu.Unlock(); return l.state.Lease }

func (l *Ledger) AddLegacyAlias(id string, generation uint64, lease, alias string) error {
	if alias == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	task, ok := l.state.Tasks[id]
	if !ok {
		return ErrTaskNotFound
	}
	if task.Generation != generation {
		return ErrStaleGeneration
	}
	if task.RuntimeLease != lease || l.state.Lease != lease {
		return ErrStaleLease
	}
	for _, existing := range task.LegacyAliases {
		if existing == alias {
			return nil
		}
	}
	before := cloneDiskState(l.state)
	task.LegacyAliases = append(task.LegacyAliases, alias)
	l.state.Tasks[id] = task
	if err := l.persistLocked(); err != nil {
		l.state = before
		return err
	}
	return nil
}

func cloneDiskState(in diskState) diskState {
	out := in
	out.Tasks = make(map[string]Task, len(in.Tasks))
	for id, task := range in.Tasks {
		out.Tasks[id] = cloneTask(task)
	}
	out.Idempotency = make(map[string]string, len(in.Idempotency))
	for key, id := range in.Idempotency {
		out.Idempotency[key] = id
	}
	out.Events = append([]Event(nil), in.Events...)
	return out
}

func NewRuntimeLease() string { return newID("runtime") }
