package observe

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDebugFileLoggerAppendsRecords(t *testing.T) {
	dir := t.TempDir()
	// Nested path proves the parent directory is auto-created.
	path := filepath.Join(dir, "sub", "session.debug.jsonl")
	logger, err := NewDebugFileLogger(path, VerboseDefault)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, rec := range []Record{
		{T: "llm_req", Step: 0, MsgCount: 4, TotalBytes: 100, Tools: 14},
		{T: "llm_resp", Step: 0, TextBytes: 340, Usage: &UsageRec{In: 12000, Out: 340, CacheRead: 9000}},
		{T: "tool", Step: 0, Name: "run_command", Risk: "medium", Decision: "allow", Approved: true, DurMs: 12, OutBytes: 500},
		{T: "decide", Step: 0, Kind: "context", Detail: "spilled tool result"},
	} {
		logger.Log(rec)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	recs := readRecords(t, path)
	if len(recs) != 4 {
		t.Fatalf("expected 4 records, got %d", len(recs))
	}
	if recs[0].T != "llm_req" || recs[0].MsgCount != 4 || recs[0].Tools != 14 {
		t.Fatalf("unexpected llm_req: %#v", recs[0])
	}
	if recs[1].Usage == nil || recs[1].Usage.CacheRead != 9000 {
		t.Fatalf("expected cache breakdown preserved on llm_resp: %#v", recs[1])
	}
	if recs[2].Name != "run_command" || recs[2].DurMs != 12 || !recs[2].Approved {
		t.Fatalf("unexpected tool record: %#v", recs[2])
	}
	if recs[3].Kind != "context" {
		t.Fatalf("unexpected decide record: %#v", recs[3])
	}
}

func TestDebugFileLoggerAppendsAcrossReopens(t *testing.T) {
	// O_APPEND means reopening the same trace file keeps prior lines intact and
	// adds new ones at the end, so a restart (or a separate debug inspection
	// mid-session) never clobbers history.
	path := filepath.Join(t.TempDir(), "s.debug.jsonl")
	logger1, err := NewDebugFileLogger(path, VerboseDefault)
	if err != nil {
		t.Fatal(err)
	}
	logger1.Log(Record{T: "llm_req", Step: 0})
	if err := logger1.Close(); err != nil {
		t.Fatal(err)
	}
	logger2, err := NewDebugFileLogger(path, VerboseDefault)
	if err != nil {
		t.Fatal(err)
	}
	logger2.Log(Record{T: "llm_resp", Step: 0})
	logger2.Close()

	recs := readRecords(t, path)
	if len(recs) != 2 {
		t.Fatalf("expected both records to survive across reopen, got %d", len(recs))
	}
}

func TestNopLoggerIsZeroOverhead(t *testing.T) {
	var l Logger = NopLogger{}
	l.Log(Record{T: "llm_req", Step: 0})
	if err := l.Close(); err != nil {
		t.Fatalf("nop close: %v", err)
	}
	if l.Verbose() != VerboseDefault {
		t.Fatalf("nop verbose default mismatch: %v", l.Verbose())
	}
}

func readRecords(t *testing.T, path string) []Record {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer f.Close()
	var out []Record
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unmarshal line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}
