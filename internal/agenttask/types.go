package agenttask

import "time"

type Kind string
type Mode string
type State string

const (
	KindAgent        Kind  = "agent"
	ModeForeground   Mode  = "foreground"
	ModeBackground   Mode  = "background"
	StateQueued      State = "queued"
	StateRunning     State = "running"
	StateWaiting     State = "waiting"
	StateCompleted   State = "completed"
	StateFailed      State = "failed"
	StateCanceled    State = "canceled"
	StateInterrupted State = "interrupted"
)

type ResultRef struct {
	Path    string `json:"path,omitempty"`
	Summary string `json:"summary,omitempty"`
	Error   string `json:"error,omitempty"`
}
type Task struct {
	ID                 string     `json:"id"`
	Kind               Kind       `json:"kind"`
	Mode               Mode       `json:"mode"`
	State              State      `json:"state"`
	SessionID          string     `json:"session_id,omitempty"`
	OwnerID            string     `json:"owner_id,omitempty"`
	TeamID             string     `json:"team_id,omitempty"`
	MemberID           string     `json:"member_id,omitempty"`
	Description        string     `json:"description,omitempty"`
	Prompt             string     `json:"prompt,omitempty"`
	Profile            string     `json:"profile,omitempty"`
	Backend            string     `json:"backend,omitempty"`
	BackendOwnershipID string     `json:"backend_ownership_id,omitempty"`
	LegacyAliases      []string   `json:"legacy_aliases,omitempty"`
	Generation         uint64     `json:"generation"`
	EventSequence      uint64     `json:"event_sequence"`
	RuntimeLease       string     `json:"runtime_lease"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	EndedAt            *time.Time `json:"ended_at,omitempty"`
	Result             ResultRef  `json:"result,omitempty"`
}
type Event struct {
	ID           string    `json:"id"`
	Sequence     uint64    `json:"sequence"`
	TaskID       string    `json:"task_id"`
	Generation   uint64    `json:"generation"`
	RuntimeLease string    `json:"runtime_lease"`
	State        State     `json:"state"`
	At           time.Time `json:"at"`
}
type CreateInput struct {
	IdempotencyKey     string
	Kind               Kind
	Mode               Mode
	SessionID          string
	OwnerID            string
	TeamID             string
	MemberID           string
	Description        string
	Prompt             string
	Profile            string
	Backend            string
	BackendOwnershipID string
}

func (s State) terminal() bool { return s == StateCompleted || s == StateFailed || s == StateCanceled }
