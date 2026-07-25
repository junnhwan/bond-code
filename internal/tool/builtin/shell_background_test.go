package builtin

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A model-facing tool call must return the command's real terminal result.
// Fire-and-forget loses completion/output forever, so the parent agent cannot
// verify a long build or react to its failure.
func TestRunCommandBackgroundCompatibilityStillWaitsForResult(t *testing.T) {
	cmd := "sleep 0.25; echo finished"
	if runtime.GOOS == "windows" {
		cmd = "Start-Sleep -Milliseconds 250; Write-Output finished"
	}
	rt := runCommandTool{}
	start := time.Now()
	result, err := rt.Execute(context.Background(), json.RawMessage(`{"command":`+quotedJSON(cmd)+`,"background":true,"timeout_seconds":10}`))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if elapsed < 200*time.Millisecond {
		t.Fatalf("legacy background flag returned before command completion: %v", elapsed)
	}
	if result == nil || !result.OK || !strings.Contains(result.Output, "finished") {
		t.Fatalf("expected observable command result, got %+v", result)
	}
}

func TestRunCommandSchemaDoesNotAdvertiseUnobservableBackgroundMode(t *testing.T) {
	encoded, err := json.Marshal(NewRunCommandTool().Schema())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "background") {
		t.Fatalf("schema must not encourage fire-and-forget commands: %s", encoded)
	}
}

func quotedJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
