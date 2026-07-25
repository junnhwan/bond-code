package teammate

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientExchangesOneTimeTokenAndPollsToCompletion(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "launch-token")
	if err := os.WriteFile(tokenFile, []byte("one-time-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/handshake":
			if r.Header.Get("Authorization") != "Bearer one-time-secret" {
				t.Fatalf("launch auth = %q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"session_token":"session-secret","lease_expires_at":"2099-01-01T00:00:00Z","poll_after_ms":1}`))
		case "/v1/task":
			if r.Header.Get("Authorization") != "Bearer session-secret" {
				t.Fatalf("session auth = %q", r.Header.Get("Authorization"))
			}
			if polls.Add(1) == 1 {
				_, _ = w.Write([]byte(`{"state":"running","output":["working"],"cursor":1,"lease_expires_at":"2099-01-01T00:00:00Z"}`))
				return
			}
			_, _ = w.Write([]byte(`{"state":"completed","output":["done"],"cursor":2,"lease_expires_at":"2099-01-01T00:00:00Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var out bytes.Buffer
	err := Run(context.Background(), Config{ParentEndpoint: server.URL, LaunchTokenFile: tokenFile, TaskID: "task", SessionID: "s", TeamID: "team", MemberID: "member", Generation: 2, OwnershipID: "owner", PollInterval: time.Millisecond}, strings.NewReader(""), &out)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tokenFile); !os.IsNotExist(err) {
		t.Fatalf("token file still exists: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "working") || !strings.Contains(got, "done") {
		t.Fatalf("output = %q", got)
	}
}

func TestClientRejectsNonLoopbackEndpointBeforeReadingToken(t *testing.T) {
	err := Run(context.Background(), Config{ParentEndpoint: "http://192.0.2.1:7777", LaunchTokenFile: "missing", TaskID: "t", SessionID: "s", Generation: 1, OwnershipID: "o"}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("error = %v", err)
	}
}

func TestClientRejectsInsecureLaunchTokenPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows permissions require ACL validation")
	}
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), Config{ParentEndpoint: "http://127.0.0.1:1", LaunchTokenFile: path, TaskID: "t", SessionID: "s", Generation: 1, OwnershipID: "o"}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("error = %v", err)
	}
}
