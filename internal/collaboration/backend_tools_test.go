package collaboration

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	execution "github.com/junnhwan/bond-code/internal/collaboration/backend"
	"github.com/junnhwan/bond-code/internal/safety"
)

type rejectingBackendSelector struct{ err error }

func (s rejectingBackendSelector) Resolve(context.Context, string) (execution.Backend, execution.Detection, error) {
	return nil, execution.Detection{}, s.err
}

func TestTeamAddMemberValidatesExplicitBackendSelection(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "collaboration.json"))
	if err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateTeam(CreateTeamInput{RequestID: "team", Name: "alpha", SessionID: "s", OwnerID: "session:s"})
	if err != nil {
		t.Fatal(err)
	}
	tools := ToolsWithBackends(store, "s", safety.ModeDefault, false, rejectingBackendSelector{err: execution.ErrUnknownBackend})
	var add interface {
		Execute(context.Context, json.RawMessage) (interface{}, error)
	}
	_ = add
	for _, candidate := range tools {
		if candidate.Name() != "team_add_member" {
			continue
		}
		result, executeErr := candidate.Execute(context.Background(), json.RawMessage(`{"request_id":"member","team_id":"`+team.ID+`","name":"builder","backend":"screen"}`))
		if executeErr != nil {
			t.Fatal(executeErr)
		}
		if result == nil || result.OK {
			t.Fatalf("result = %#v", result)
		}
		if got := store.ListMembers(team.ID); len(got) != 0 {
			t.Fatalf("members persisted after rejected backend: %#v", got)
		}
		return
	}
	t.Fatal("team_add_member tool not registered")
}

func TestTeamAddMemberDefaultsBackendToInProcess(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "collaboration.json"))
	if err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateTeam(CreateTeamInput{RequestID: "team-default", Name: "beta", SessionID: "s", OwnerID: "session:s"})
	if err != nil {
		t.Fatal(err)
	}
	selector := rejectingBackendSelector{err: errors.New("selector should not reject default")}
	_ = selector
	// The legacy constructor remains compatible and normalizes an omitted backend.
	tools := ToolsWithPolicy(store, "s", safety.ModeDefault, false)
	for _, candidate := range tools {
		if candidate.Name() == "team_add_member" {
			result, _ := candidate.Execute(context.Background(), json.RawMessage(`{"request_id":"member","team_id":"`+team.ID+`","name":"builder"}`))
			if !result.OK {
				t.Fatalf("result = %#v", result)
			}
			members := store.ListMembers(team.ID)
			if len(members) != 1 || members[0].Backend != string(execution.KindInProcess) {
				t.Fatalf("members = %#v", members)
			}
			return
		}
	}
	t.Fatal("team_add_member tool not registered")
}
