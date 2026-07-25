package session

import "time"

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleSystem    Role = "system"
)

type Message struct {
	Role      Role      `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type ToolCall struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Input     string    `json:"input"`
	Risk      string    `json:"risk"`
	Approved  bool      `json:"approved"`
	Output    string    `json:"output"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type AgentEvent struct {
	Type       string    `json:"type"`
	Message    string    `json:"message,omitempty"`
	ToolName   string    `json:"tool_name,omitempty"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	Risk       string    `json:"risk,omitempty"`
	Input      string    `json:"input,omitempty"`
	Output     string    `json:"output,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type Event struct {
	EventID    string      `json:"event_id,omitempty"`  // 本事件 ID（session-tree 节点；空=旧线性事件，由 Store 自动补）
	ParentID   string      `json:"parent_id,omitempty"` // 父事件 ID（分叉点；空=根/线性）
	SessionID  string      `json:"session_id"`
	Type       string      `json:"type"`
	Message    *Message    `json:"message,omitempty"`
	ToolCall   *ToolCall   `json:"tool_call,omitempty"`
	AgentEvent *AgentEvent `json:"agent_event,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
}
