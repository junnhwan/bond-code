// Package observe provides an opt-in, Ctrl+C-safe per-session debug trace that
// complements the protocol-audit session.jsonl. Where the audit layer records
// every agent event (chunk-level, shareable transcript), the debug layer
// records model-decision facts one line per call: the governed request actually
// sent to the model, the response with prompt-cache breakdown, each tool
// decision and timing, and safety/context governance decisions. It is opt-in
// (--debug) and writes newline-delimited JSON via O_APPEND so a SIGINT between
// records cannot lose already-written lines.
package observe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Verbose controls how much the debug trace records.
type Verbose int

const (
	// VerboseDefault records governed-request payloads (truncated to PayloadCap)
	// and response text (truncated), plus tool/decide records.
	VerboseDefault Verbose = 1
	// VerboseFull records untruncated payloads and response text (larger files).
	VerboseFull Verbose = 2
)

// Record is one newline-delimited JSON line in the debug trace. Only fields
// relevant to the record type T are populated; the rest are omitted so each line
// stays compact and type-readable.
type Record struct {
	T           string        `json:"t"`                      // llm_req | llm_resp | tool | decide
	Step        int           `json:"step"`                   // loop step (0-based)
	Model       string        `json:"model,omitempty"`        // llm_req
	MsgCount    int           `json:"msg_count,omitempty"`    // llm_req
	SystemBytes int           `json:"system_bytes,omitempty"` // llm_req
	TotalBytes  int           `json:"total_bytes,omitempty"`  // llm_req
	Tools       int           `json:"tools,omitempty"`        // llm_req
	Payload     string        `json:"payload,omitempty"`      // llm_req governed messages (truncated unless full)
	TextBytes   int           `json:"text_bytes,omitempty"`   // llm_resp
	ToolCalls   []ToolCallRec `json:"tool_calls,omitempty"`   // llm_resp
	Usage       *UsageRec     `json:"usage,omitempty"`        // llm_resp
	StopReason  string        `json:"stop_reason,omitempty"`  // llm_resp: end_turn|tool_use|max_tokens|stop_sequence|length
	Name        string        `json:"name,omitempty"`         // tool
	ArgsBytes   int           `json:"args_bytes,omitempty"`   // tool
	Risk        string        `json:"risk,omitempty"`         // tool | decide(safety)
	Decision    string        `json:"decision,omitempty"`     // tool: allow|confirm|blocked|rejected|disabled|hook-blocked
	Approved    bool          `json:"approved,omitempty"`     // tool
	DurMs       int64         `json:"dur_ms,omitempty"`       // tool
	OutBytes    int           `json:"out_bytes,omitempty"`    // tool
	Error       string        `json:"error,omitempty"`        // tool | llm_resp
	Kind        string        `json:"kind,omitempty"`         // decide: safety | context
	Detail      string        `json:"detail,omitempty"`       // decide
}

// ToolCallRec is one requested tool call summarized in an llm_resp record.
type ToolCallRec struct {
	Name      string `json:"name"`
	ArgsBytes int    `json:"args_bytes"`
}

// UsageRec is the prompt-cache-aware token breakdown for an llm_resp record.
type UsageRec struct {
	In          int `json:"in"`
	Out         int `json:"out"`
	CacheRead   int `json:"cache_read"`
	CacheCreate int `json:"cache_create"`
}

// Logger writes debug records. The agent loop holds one of these and calls Log
// on the hot path, so the no-op implementation must stay cheap.
type Logger interface {
	Log(record Record)
	Close() error
	Verbose() Verbose
}

// NopLogger is a zero-overhead Logger used when debug tracing is off, so the
// loop's `if l.debugLogger != nil` guard is the only hot-path cost.
type NopLogger struct{}

func (NopLogger) Log(Record)       {}
func (NopLogger) Close() error     { return nil }
func (NopLogger) Verbose() Verbose { return VerboseDefault }

// DebugFileLogger appends newline-delimited JSON records to a per-session file
// (<dir>/<id>.debug.jsonl). Each Log call json-encodes one record (the encoder
// appends a trailing newline) under a mutex; the file is opened O_APPEND so a
// SIGINT between records cannot corrupt already-written lines, mirroring
// session.jsonl's Ctrl+C-safety.
type DebugFileLogger struct {
	mu      sync.Mutex
	f       *os.File
	enc     *json.Encoder
	verbose Verbose
}

// NewDebugFileLogger opens (creating if needed) the debug trace file. The parent
// directory is created with 0o700 and the file with 0o600, matching the session
// audit store permissions.
func NewDebugFileLogger(path string, verbose Verbose) (*DebugFileLogger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &DebugFileLogger{f: f, enc: json.NewEncoder(f), verbose: verbose}, nil
}

func (l *DebugFileLogger) Log(record Record) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.enc.Encode(record)
}

func (l *DebugFileLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

func (l *DebugFileLogger) Verbose() Verbose { return l.verbose }

// PayloadCap is the default character cap on a governed-request payload
// inlined into an llm_req record at VerboseDefault; VerboseFull leaves it
// unbounded. A giant system prompt would otherwise dominate the trace file.
const PayloadCap = 16 * 1024
