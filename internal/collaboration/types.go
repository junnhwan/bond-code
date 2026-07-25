package collaboration

import "time"

type TeamState string
type MemberState string
type PrincipalKind string
type MessageKind string

const (
	TeamActive                  TeamState     = "active"
	TeamShuttingDown            TeamState     = "shutting_down"
	TeamDeleted                 TeamState     = "deleted"
	MemberActive                MemberState   = "active"
	MemberStopping              MemberState   = "stopping"
	MemberStopped               MemberState   = "stopped"
	PrincipalUser               PrincipalKind = "user"
	PrincipalOwner              PrincipalKind = "owner"
	PrincipalMember             PrincipalKind = "member"
	PrincipalBackendWorker      PrincipalKind = "backend_worker"
	MessageDirect               MessageKind   = "direct"
	MessageBroadcast            MessageKind   = "broadcast"
	MessageTaskAssignment       MessageKind   = "task_assignment"
	MessageProgress             MessageKind   = "progress"
	MessageResult               MessageKind   = "result"
	MessageShutdownRequest      MessageKind   = "shutdown_request"
	MessageShutdownResponse     MessageKind   = "shutdown_response"
	MessagePlanApprovalRequest  MessageKind   = "plan_approval_request"
	MessagePlanApprovalResponse MessageKind   = "plan_approval_response"
)

type Team struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SessionID string    `json:"session_id"`
	OwnerID   string    `json:"owner_id"`
	Objective string    `json:"objective,omitempty"`
	State     TeamState `json:"state"`
	MemberIDs []string  `json:"member_ids"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Member struct {
	ID             string      `json:"id"`
	TeamID         string      `json:"team_id"`
	Name           string      `json:"name"`
	Role           string      `json:"role,omitempty"`
	Profile        string      `json:"profile,omitempty"`
	Backend        string      `json:"backend,omitempty"`
	PermissionMode string      `json:"permission_mode,omitempty"`
	PrimaryTaskID  string      `json:"primary_task_id,omitempty"`
	State          MemberState `json:"state"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}
type Principal struct {
	Kind PrincipalKind `json:"kind"`
	ID   string        `json:"id"`
}
type Message struct {
	ID            string      `json:"id"`
	RequestID     string      `json:"request_id"`
	TeamID        string      `json:"team_id"`
	Sender        Principal   `json:"sender"`
	Recipients    []string    `json:"recipients"`
	Kind          MessageKind `json:"kind"`
	Body          string      `json:"body"`
	CorrelationID string      `json:"correlation_id,omitempty"`
	TaskID        string      `json:"task_id,omitempty"`
	Generation    uint64      `json:"generation,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
}
type Delivery struct {
	MessageID   string     `json:"message_id"`
	TeamID      string     `json:"team_id"`
	RecipientID string     `json:"recipient_id"`
	Sequence    uint64     `json:"sequence"`
	ReadAt      *time.Time `json:"read_at,omitempty"`
}
type InboxItem struct {
	Sequence uint64
	ReadAt   *time.Time
	Message  Message
}
type CreateTeamInput struct{ RequestID, Name, SessionID, OwnerID, Objective string }
type AddMemberInput struct {
	RequestID      string `json:"request_id"`
	TeamID         string `json:"team_id"`
	Name           string `json:"name"`
	Role           string `json:"role"`
	Profile        string `json:"profile"`
	Backend        string `json:"backend"`
	PermissionMode string `json:"permission_mode"`
}
type SendInput struct {
	RequestID, TeamID           string
	Sender                      Principal
	Recipients                  []string
	Broadcast                   bool
	Kind                        MessageKind
	Body, CorrelationID, TaskID string
	Generation                  uint64
}
type AssignmentState string

const (
	AssignmentActive    AssignmentState = "active"
	AssignmentCompleted AssignmentState = "completed"
)

type Assignment struct {
	ID         string          `json:"id"`
	TeamID     string          `json:"team_id"`
	MemberID   string          `json:"member_id"`
	TaskID     string          `json:"task_id"`
	Generation uint64          `json:"generation"`
	State      AssignmentState `json:"state"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}
type AssignInput struct {
	RequestID  string    `json:"request_id"`
	TeamID     string    `json:"team_id"`
	MemberID   string    `json:"member_id"`
	TaskID     string    `json:"task_id"`
	Generation uint64    `json:"generation"`
	Issuer     Principal `json:"-"`
}
type ShutdownInput struct {
	RequestID string    `json:"request_id"`
	TeamID    string    `json:"team_id"`
	MemberID  string    `json:"member_id"`
	Issuer    Principal `json:"-"`
}
