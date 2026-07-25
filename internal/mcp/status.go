package mcp

import "time"

type ServerState string

const (
	ServerStateConnected    ServerState = "connected"
	ServerStateDisconnected ServerState = "disconnected"
	ServerStateError        ServerState = "error"
)

type ServerStatus struct {
	Name        string      `json:"name"`
	Command     string      `json:"command,omitempty"`
	Args        []string    `json:"args,omitempty"`
	State       ServerState `json:"state"`
	ToolCount   int         `json:"tool_count"`
	LastError   string      `json:"last_error,omitempty"`
	ConnectedAt time.Time   `json:"connected_at,omitempty"`
	UpdatedAt   time.Time   `json:"updated_at,omitempty"`
}
