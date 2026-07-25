package observe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeLoggerAppendsRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.log")
	l, err := NewRuntimeLogger(path)
	if err != nil {
		t.Fatalf("NewRuntimeLogger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	l.Write(RuntimeRecord{Time: time.Now(), Level: "error", Name: "cli", Detail: "bad config"})
	l.Write(RuntimeRecord{Time: time.Now(), Level: "panic", Name: "agent", Detail: "boom", Stack: "goroutine 1 [running]:\nloop.go:42"})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "[error] name=cli") || !strings.Contains(s, "bad config") {
		t.Errorf("missing error record:\n%s", s)
	}
	if !strings.Contains(s, "[panic] name=agent") || !strings.Contains(s, "boom") || !strings.Contains(s, "loop.go:42") {
		t.Errorf("missing panic record / stack:\n%s", s)
	}
	if got := strings.Count(s, "[panic]"); got != 1 {
		t.Errorf("want 1 panic header, got %d", got)
	}
}

func TestSetRuntimeLoggerAndLogPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.log")
	l, err := NewRuntimeLogger(path)
	if err != nil {
		t.Fatalf("NewRuntimeLogger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	prev := getRuntimeLogger()
	SetRuntimeLogger(l)
	t.Cleanup(func() { SetRuntimeLogger(prev) })

	LogPanic("test", "exploded", []byte("stacktrace"))

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "[panic] name=test") || !strings.Contains(string(data), "exploded") {
		t.Fatalf("LogPanic not written:\n%s", data)
	}
}

func TestSafeGoSwallowsPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.log")
	l, err := NewRuntimeLogger(path)
	if err != nil {
		t.Fatalf("NewRuntimeLogger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	prev := getRuntimeLogger()
	SetRuntimeLogger(l)
	t.Cleanup(func() { SetRuntimeLogger(prev) })

	SafeGo("bg", func() {
		panic("kaboom")
	})
	// SafeGo logs from its deferred recover, which runs AFTER fn unwinds — so a
	// one-shot read races the write. Poll until the panic lands (or time out).
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), "kaboom") {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("SafeGo panic not logged:\n%s", data)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
