package collaboration

import (
	"context"
	"encoding/json"
	"testing"
)

func TestToolsCreateTeamAndSendMailboxMessage(t *testing.T) {
	s, _ := Open(t.TempDir() + "/collaboration.json")
	tools := Tools(s, "session-1")
	names := map[string]bool{}
	for _, candidate := range tools {
		names[candidate.Name()] = true
	}
	for _, name := range []string{"team_create", "team_list", "team_add_member", "team_assign", "team_shutdown", "team_delete", "mailbox_send", "mailbox_broadcast", "mailbox_inbox", "mailbox_read"} {
		if !names[name] {
			t.Fatalf("missing %s", name)
		}
	}
	result, err := tools[0].Execute(context.Background(), json.RawMessage(`{"request_id":"r1","name":"alpha"}`))
	if err != nil || !result.OK {
		t.Fatalf("create = %#v, %v", result, err)
	}
	if len(s.ListTeams("session-1")) != 1 {
		t.Fatal("team was not created")
	}
}

func TestToolSchemasDoNotEncodeNullRequired(t *testing.T) {
	s, _ := Open(t.TempDir() + "/collaboration.json")
	for _, candidate := range Tools(s, "session-1") {
		body, err := json.Marshal(candidate.Schema())
		if err != nil {
			t.Fatalf("marshal %s schema: %v", candidate.Name(), err)
		}
		var schema map[string]any
		if err := json.Unmarshal(body, &schema); err != nil {
			t.Fatalf("decode %s schema: %v", candidate.Name(), err)
		}
		if required, exists := schema["required"]; exists && required == nil {
			t.Fatalf("%s schema contains required:null: %s", candidate.Name(), body)
		}
	}
}
