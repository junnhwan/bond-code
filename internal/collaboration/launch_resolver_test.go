package collaboration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/junnhwan/bond-code/internal/agenttask"
	execution "github.com/junnhwan/bond-code/internal/collaboration/backend"
)

type recordingSelector struct{ selection string }

func (s *recordingSelector) Resolve(_ context.Context, selection string) (execution.Backend, execution.Detection, error) {
	s.selection = selection
	return nil, execution.Detection{Kind: execution.Kind(selection), Available: true}, nil
}

func TestLaunchResolverInheritsMemberBackendAndProfile(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "collaboration.json"))
	if err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateTeam(CreateTeamInput{RequestID: "t", Name: "alpha", SessionID: "s", OwnerID: "session:s"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := store.AddMember(AddMemberInput{RequestID: "m", TeamID: team.ID, Name: "builder", Backend: "tmux", Profile: "coder"})
	if err != nil {
		t.Fatal(err)
	}
	selector := &recordingSelector{}
	resolver := NewLaunchResolver(store, selector)
	got, err := resolver.ResolveLaunch(context.Background(), agenttask.StartInput{SessionID: "s", OwnerID: "session:s", TeamID: team.ID, MemberID: member.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Backend != "tmux" || got.Profile != "coder" || got.OwnershipID == "" || selector.selection != "tmux" {
		t.Fatalf("resolved = %#v, selection = %q", got, selector.selection)
	}
}

func TestLaunchResolverRejectsCrossSessionTeam(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "collaboration.json"))
	team, _ := store.CreateTeam(CreateTeamInput{RequestID: "t", Name: "alpha", SessionID: "other", OwnerID: "session:other"})
	member, _ := store.AddMember(AddMemberInput{RequestID: "m", TeamID: team.ID, Name: "builder", Backend: "in_process"})
	resolver := NewLaunchResolver(store, &recordingSelector{})
	if _, err := resolver.ResolveLaunch(context.Background(), agenttask.StartInput{SessionID: "s", OwnerID: "session:s", TeamID: team.ID, MemberID: member.ID}); err == nil {
		t.Fatal("expected cross-session rejection")
	}
}
