package mcp

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/junnhwan/bond-code/internal/tool"
)

// Manager is the stdio MCP connection surface used by bootstrap and CLI.
type Manager interface {
	Connect(ctx context.Context, name string, command string, args []string) error
	Disconnect(ctx context.Context, name string) error
	ListTools(ctx context.Context) ([]tool.Tool, error)
	Status() []ServerStatus
	Close() error
}

// ProcessManager owns stdio MCP server processes (Claude Code-style connection map).
// Failed connects are recorded as ServerStateError and do not block other servers.
type ProcessManager struct {
	mu          sync.Mutex
	clients     map[string]*processClient
	status      map[string]ServerStatus
	callTimeout time.Duration
}

func NewProcessManager() *ProcessManager {
	return &ProcessManager{
		clients:     map[string]*processClient{},
		status:      map[string]ServerStatus{},
		callTimeout: 30 * time.Second,
	}
}

func (m *ProcessManager) Connect(ctx context.Context, name string, command string, args []string) error {
	if name == "" {
		return fmt.Errorf("mcp server name is required")
	}
	if command == "" {
		return fmt.Errorf("mcp server command is required")
	}
	m.mu.Lock()
	if _, exists := m.clients[name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("mcp server %q already connected", name)
	}
	timeout := m.callTimeout
	m.mu.Unlock()
	client, err := startProcessClient(ctx, name, command, args)
	if err != nil {
		m.recordError(name, command, args, err, "")
		return err
	}
	client.setCallTimeout(timeout)
	var ignored map[string]any
	if err := client.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "bondcode",
			"version": "0.1.0",
		},
	}, &ignored); err != nil {
		m.recordError(name, command, args, err, client.stderrString())
		_ = client.close()
		return err
	}
	// MCP requires notifications/initialized after initialize (best-effort).
	_ = client.notify(ctx, "notifications/initialized", map[string]any{})

	now := time.Now().UTC()
	m.mu.Lock()
	m.clients[name] = client
	m.status[name] = ServerStatus{
		Name:        name,
		Command:     command,
		Args:        append([]string(nil), args...),
		State:       ServerStateConnected,
		ConnectedAt: now,
		UpdatedAt:   now,
	}
	m.mu.Unlock()
	return nil
}

// ConnectAll connects every enabled server. Individual failures are recorded;
// other servers still connect.
func (m *ProcessManager) ConnectAll(ctx context.Context, servers []ServerConfig) (connected int, errs []error) {
	for _, s := range servers {
		if !s.Enabled {
			continue
		}
		if err := m.Connect(ctx, s.Name, s.Command, s.Args); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Name, err))
			continue
		}
		connected++
	}
	return connected, errs
}

// ServerConfig is the runtime view of a configured stdio MCP server.
type ServerConfig struct {
	Name    string
	Command string
	Args    []string
	Enabled bool
}

func (m *ProcessManager) Disconnect(ctx context.Context, name string) error {
	m.mu.Lock()
	client, ok := m.clients[name]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.clients, name)
	status := m.status[name]
	status.Name = name
	status.State = ServerStateDisconnected
	status.UpdatedAt = time.Now().UTC()
	m.status[name] = status
	m.mu.Unlock()
	return client.close()
}

// Close disconnects every server.
func (m *ProcessManager) Close() error {
	m.mu.Lock()
	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	m.mu.Unlock()
	var first error
	for _, name := range names {
		if err := m.Disconnect(context.Background(), name); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (m *ProcessManager) ListTools(ctx context.Context) ([]tool.Tool, error) {
	m.mu.Lock()
	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	m.mu.Unlock()
	sort.Strings(names)
	var out []tool.Tool
	for _, name := range names {
		tools, err := m.ListToolsForServer(ctx, name)
		if err != nil {
			return nil, err
		}
		out = append(out, tools...)
	}
	return out, nil
}

func (m *ProcessManager) ListToolsForServer(ctx context.Context, name string) ([]tool.Tool, error) {
	m.mu.Lock()
	client, ok := m.clients[name]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("mcp server %q is not connected", name)
	}
	var resp struct {
		Tools []toolInfo `json:"tools"`
	}
	if err := client.call(ctx, "tools/list", nil, &resp); err != nil {
		m.recordErrorForExisting(name, err, client.stderrString())
		return nil, err
	}
	out := make([]tool.Tool, 0, len(resp.Tools))
	for _, info := range resp.Tools {
		out = append(out, newAdapterTool(client, name, info))
	}
	m.recordToolCount(name, len(out))
	return out, nil
}

func (m *ProcessManager) Status() []ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.status))
	for name := range m.status {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ServerStatus, 0, len(names))
	for _, name := range names {
		status := m.status[name]
		status.Args = append([]string(nil), status.Args...)
		out = append(out, status)
	}
	return out
}

func (m *ProcessManager) SetCallTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callTimeout = timeout
	for _, client := range m.clients {
		client.setCallTimeout(timeout)
	}
}

func (m *ProcessManager) recordToolCount(name string, count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := m.status[name]
	status.Name = name
	status.State = ServerStateConnected
	status.ToolCount = count
	status.LastError = ""
	status.UpdatedAt = time.Now().UTC()
	m.status[name] = status
}

func (m *ProcessManager) recordErrorForExisting(name string, err error, stderr string) {
	m.mu.Lock()
	status := m.status[name]
	command := status.Command
	args := append([]string(nil), status.Args...)
	m.mu.Unlock()
	m.recordError(name, command, args, err, stderr)
}

func (m *ProcessManager) recordError(name, command string, args []string, err error, stderr string) {
	message := err.Error()
	if stderr != "" {
		message += "\nstderr: " + stderr
	}
	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	status := m.status[name]
	if status.ConnectedAt.IsZero() {
		status.ConnectedAt = now
	}
	status.Name = name
	status.Command = command
	status.Args = append([]string(nil), args...)
	status.State = ServerStateError
	status.LastError = message
	status.UpdatedAt = now
	m.status[name] = status
}
