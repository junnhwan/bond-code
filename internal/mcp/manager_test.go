package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestManagerConnectsAndListsToolsFromFakeServer(t *testing.T) {
	server := buildFakeMCPServer(t)
	manager := NewProcessManager()

	if err := manager.Connect(context.Background(), "fake", server, nil); err != nil {
		t.Fatal(err)
	}
	defer manager.Disconnect(context.Background(), "fake")

	tools, err := manager.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name() != "mcp__fake__fake_echo" {
		t.Fatalf("unexpected tools %#v (name=%q)", tools, tools[0].Name())
	}
}

func TestManagerReturnsInitializeError(t *testing.T) {
	server := buildFakeMCPServer(t, "error")
	manager := NewProcessManager()

	err := manager.Connect(context.Background(), "bad", server, nil)
	if err == nil {
		t.Fatal("expected initialize error")
	}
	if _, exists := manager.clients["bad"]; exists {
		t.Fatal("expected failed client not to remain connected")
	}
}

func TestManagerConnectHonorsContextWhileWaitingForInitialize(t *testing.T) {
	server := buildFakeMCPServer(t, "sleep")
	manager := NewProcessManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := manager.Connect(ctx, "slow", server, nil)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if _, exists := manager.clients["slow"]; exists {
		t.Fatal("expected cancelled client not to remain connected")
	}
}

func TestManagerRecordsInvalidJSONResponse(t *testing.T) {
	server := buildFakeMCPServer(t, "invalid-json")
	manager := NewProcessManager()

	err := manager.Connect(context.Background(), "bad-json", server, nil)
	if err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestListToolsHonorsCallTimeout(t *testing.T) {
	server := buildFakeMCPServer(t, "list-sleep")
	manager := NewProcessManager()

	if err := manager.Connect(context.Background(), "slow", server, nil); err != nil {
		t.Fatalf("connect fake server: %v", err)
	}
	defer manager.Disconnect(context.Background(), "slow")
	manager.SetCallTimeout(10 * time.Millisecond)

	_, err := manager.ListToolsForServer(context.Background(), "slow")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestListToolsAfterTimeoutDoesNotReadStaleResponse(t *testing.T) {
	server := buildFakeMCPServer(t, "list-delay-once")
	manager := NewProcessManager()

	if err := manager.Connect(context.Background(), "slow-once", server, nil); err != nil {
		t.Fatalf("connect fake server: %v", err)
	}
	defer manager.Disconnect(context.Background(), "slow-once")
	manager.SetCallTimeout(20 * time.Millisecond)

	_, err := manager.ListToolsForServer(context.Background(), "slow-once")
	if err == nil {
		t.Fatal("expected first list to time out")
	}

	manager.SetCallTimeout(time.Second)
	tools, err := manager.ListToolsForServer(context.Background(), "slow-once")
	if err != nil {
		t.Fatalf("expected second list to ignore stale response: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "mcp__slow-once__fresh_echo" {
		t.Fatalf("expected fresh tool response, got %#v", tools)
	}
}

func buildFakeMCPServer(t *testing.T, initializeBehavior ...string) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	initBehavior := `nil`
	if len(initializeBehavior) > 0 {
		initBehavior = initializeBehavior[0]
	}
	initBehavior = fmt.Sprintf("%q", initBehavior)
	code := `package main
import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)
func main() {
	listDelayOnceUsed := false
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &req)
		id := req["id"]
		method, _ := req["method"].(string)
		var result any = map[string]any{}
		var rpcError any
		if method == "initialize" {
			initBehavior := ` + initBehavior + `
			if initBehavior == "sleep" {
				select {}
			}
			if initBehavior == "invalid-json" {
				fmt.Println("{not-json")
				continue
			}
			if initBehavior == "error" {
				rpcError = map[string]any{"code":-32000,"message":"init failed"}
			}
		}
		if method == "tools/list" {
			initBehavior := ` + initBehavior + `
			if initBehavior == "list-sleep" {
				select {}
			}
			if initBehavior == "list-delay-once" && !listDelayOnceUsed {
				listDelayOnceUsed = true
				time.Sleep(80 * time.Millisecond)
				result = map[string]any{"tools":[]any{map[string]any{"name":"stale_echo","description":"stale","inputSchema":map[string]any{"type":"object"}}}}
			} else if initBehavior == "list-delay-once" {
				result = map[string]any{"tools":[]any{map[string]any{"name":"fresh_echo","description":"fresh","inputSchema":map[string]any{"type":"object"}}}}
			} else {
				result = map[string]any{"tools":[]any{map[string]any{"name":"fake_echo","description":"echo","inputSchema":map[string]any{"type":"object"}}}}
			}
			if initBehavior == "list-error" {
				rpcError = map[string]any{"code":-32001,"message":"list failed"}
			}
		}
		respMap := map[string]any{"jsonrpc":"2.0","id":id,"result":result}
		if rpcError != nil {
			respMap["error"] = rpcError
			delete(respMap, "result")
		}
		resp, _ := json.Marshal(respMap)
		fmt.Println(string(resp))
	}
}
`
	if err := os.WriteFile(source, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fake-mcp")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, source)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fake mcp: %v\n%s", err, string(out))
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatal(fmt.Errorf("fake mcp binary missing: %w", err))
	}
	return bin
}
