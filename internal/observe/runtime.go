package observe

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// RuntimeLogger is the process-wide sink for unrecoverable runtime facts:
// panics (with stack) and startup/CLI errors. It complements the per-session
// DebugFileLogger: where the debug trace records model-decision facts for one
// session, the runtime log records crash-level facts across all sessions in a
// project, always-on, so a bootstrap failure or a goroutine panic still leaves
// a body on disk at <projects-dir>/runtime.log.
type RuntimeLogger interface {
	Write(rec RuntimeRecord)
	Close() error
}

// RuntimeRecord is one entry in runtime.log.
type RuntimeRecord struct {
	Time   time.Time
	Level  string // "panic" | "error"
	Name   string // logical source, e.g. "agent", "llm-stream", "main", "cli"
	Detail string // human-readable summary
	Stack  string // optional; multi-line stack for panics
}

// String renders one record as a human-readable block. Stack lines are
// indented so a multi-line trace stays readable in the append-only log.
func (r RuntimeRecord) String() string {
	var b bytes.Buffer
	if r.Time.IsZero() {
		r.Time = time.Now()
	}
	fmt.Fprintf(&b, "%s [%s] name=%s", r.Time.UTC().Format("2006-01-02T15:04:05.000Z"), r.Level, r.Name)
	if r.Detail != "" {
		b.WriteString("\n  detail: ")
		b.WriteString(r.Detail)
	}
	if r.Stack != "" {
		b.WriteString("\n  stack:\n")
		for _, line := range strings.Split(strings.TrimRight(r.Stack, "\n"), "\n") {
			b.WriteString("    ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
		return b.String()
	}
	b.WriteByte('\n')
	return b.String()
}

// NopRuntimeLogger is the zero-overhead default used until main installs a real
// sink, so tests and any pre-main code pay nothing.
type NopRuntimeLogger struct{}

func (NopRuntimeLogger) Write(RuntimeRecord) {}
func (NopRuntimeLogger) Close() error        { return nil }

type runtimeFileLogger struct {
	mu sync.Mutex
	f  *os.File
}

// NewRuntimeLogger opens (creating if needed) path for appending. Mirrors
// NewDebugFileLogger's Ctrl+C-safe O_APPEND semantics so a SIGINT between
// writes cannot corrupt already-written lines. Returns an error if the file
// cannot be opened; callers fall back to NopRuntimeLogger so a logging failure
// never blocks startup.
func NewRuntimeLogger(path string) (RuntimeLogger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &runtimeFileLogger{f: f}, nil
}

func (l *runtimeFileLogger) Write(rec RuntimeRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.f.WriteString(rec.String())
}

func (l *runtimeFileLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// Package-level default. Recover points are scattered across deep goroutines;
// they reach for this singleton rather than threading a handle everywhere. main
// installs a real sink once at startup; tests leave it Nop.
var (
	runtimeMu     sync.Mutex
	runtimeLogger RuntimeLogger = NopRuntimeLogger{}
)

// SetRuntimeLogger installs the process-wide runtime sink. Call once from main.
func SetRuntimeLogger(l RuntimeLogger) {
	runtimeMu.Lock()
	runtimeLogger = l
	runtimeMu.Unlock()
}

func getRuntimeLogger() RuntimeLogger {
	runtimeMu.Lock()
	l := runtimeLogger
	runtimeMu.Unlock()
	return l
}

// LogPanic records a recovered panic (value + stack) under the given source
// name. Safe to call from any goroutine, including a defer/recover block.
func LogPanic(name string, r any, stack []byte) {
	getRuntimeLogger().Write(RuntimeRecord{
		Time:   time.Now(),
		Level:  "panic",
		Name:   name,
		Detail: fmt.Sprintf("%v", r),
		Stack:  string(stack),
	})
}

// LogError records a non-panic error under the given source name. No-op if err
// is nil.
func LogError(src string, err error) {
	if err == nil {
		return
	}
	getRuntimeLogger().Write(RuntimeRecord{
		Time:   time.Now(),
		Level:  "error",
		Name:   src,
		Detail: err.Error(),
	})
}

// SafeGo runs fn on a new goroutine with a recover guard that logs and swallows
// any panic. Use for background goroutines whose panic must not crash the
// process (detached memory jobs, MCP read loops).
func SafeGo(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				LogPanic(name, r, debug.Stack())
			}
		}()
		fn()
	}()
}
