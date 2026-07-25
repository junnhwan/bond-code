package backendipc

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/junnhwan/bond-code/internal/teammate"
)

func TestSupervisorAuthenticatesClientPublishesOutputAndRenewsLease(t *testing.T) {
	supervisor, err := Start(t.TempDir(), 80*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.Close()
	inputs := make(chan string, 1)
	supervisor.SetInputHandler(func(_ context.Context, _ Identity, input string) error { inputs <- input; return nil })
	identity := Identity{TaskID: "task", SessionID: "session", TeamID: "team", MemberID: "member", Generation: 2, OwnershipID: "owner"}
	launch, lease, err := supervisor.Prepare(identity)
	if err != nil {
		t.Fatal(err)
	}
	clientDone := make(chan error, 1)
	var output bytes.Buffer
	go func() {
		clientDone <- teammate.Run(context.Background(), teammate.Config{ParentEndpoint: launch.Endpoint, LaunchTokenFile: launch.TokenFile, TaskID: identity.TaskID, SessionID: identity.SessionID, TeamID: identity.TeamID, MemberID: identity.MemberID, Generation: identity.Generation, OwnershipID: identity.OwnershipID, PollInterval: 5 * time.Millisecond}, strings.NewReader("focus\n"), &output)
	}()
	deadline := time.Now().Add(time.Second)
	for !lease.Authenticated() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !lease.Authenticated() {
		t.Fatal("client did not authenticate")
	}
	select {
	case input := <-inputs:
		if input != "focus" {
			t.Fatalf("input = %q", input)
		}
	case <-time.After(time.Second):
		t.Fatal("client input was not forwarded")
	}
	supervisor.Publish(identity, "running", "working", "")
	time.Sleep(20 * time.Millisecond)
	select {
	case <-lease.Done():
		t.Fatal("lease expired while client was polling")
	default:
	}
	supervisor.Publish(identity, "completed", "done", "")
	if err := <-clientDone; err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "working") || !strings.Contains(got, "done") {
		t.Fatalf("output = %q", got)
	}
	if _, err := os.Stat(launch.TokenFile); !os.IsNotExist(err) {
		t.Fatalf("launch token remains: %v", err)
	}
}

func TestSupervisorConsumesLaunchTokenOnceAndFencesGeneration(t *testing.T) {
	supervisor, err := Start(t.TempDir(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.Close()
	identity := Identity{TaskID: "task", SessionID: "session", Generation: 3, OwnershipID: "owner"}
	launch, _, err := supervisor.Prepare(identity)
	if err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(launch.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	post := func(generation string) int {
		body := `{"task_id":"task","session_id":"session","generation":` + generation + `,"ownership_id":"owner"}`
		req, _ := http.NewRequest(http.MethodPost, launch.Endpoint+"/v1/handshake", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	if got := post("2"); got != http.StatusUnauthorized {
		t.Fatalf("stale generation status = %d", got)
	}
	if got := post("3"); got != http.StatusOK {
		t.Fatalf("first exchange status = %d", got)
	}
	if got := post("3"); got != http.StatusUnauthorized {
		t.Fatalf("reused token status = %d", got)
	}
}

func TestSupervisorExpiresUnrenewedLease(t *testing.T) {
	supervisor, err := Start(t.TempDir(), 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.Close()
	_, lease, err := supervisor.Prepare(Identity{TaskID: "task", SessionID: "s", Generation: 1, OwnershipID: "o"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-lease.Done():
	case <-time.After(time.Second):
		t.Fatal("lease did not expire")
	}
}

func TestSupervisorResumeGenerationGetsFreshLaunchCredential(t *testing.T) {
	supervisor, err := Start(t.TempDir(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.Close()
	firstID := Identity{TaskID: "task", SessionID: "session", Generation: 1, OwnershipID: "owner-1"}
	first, _, err := supervisor.Prepare(firstID)
	if err != nil {
		t.Fatal(err)
	}
	firstToken, err := os.ReadFile(first.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	supervisor.Release(firstID)
	secondID := Identity{TaskID: "task", SessionID: "session", Generation: 2, OwnershipID: "owner-2"}
	second, _, err := supervisor.Prepare(secondID)
	if err != nil {
		t.Fatal(err)
	}
	secondToken, err := os.ReadFile(second.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if first.TokenFile == second.TokenFile {
		t.Fatalf("token path reused: %s", first.TokenFile)
	}
	if bytes.Equal(firstToken, secondToken) {
		t.Fatal("launch token reused across generations")
	}
}
