package cli

import (
	"path/filepath"
	"testing"

	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/collaboration"
	"github.com/junnhwan/bond-code/internal/tui"
)

func TestWithCollaborationStatusProjectsMembersAndUnread(t *testing.T) {
	store, err := collaboration.Open(filepath.Join(t.TempDir(), "collaboration.json"))
	if err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateTeam(collaboration.CreateTeamInput{RequestID: "team-1", Name: "alpha", SessionID: "session-1", OwnerID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := store.AddMember(collaboration.AddMemberInput{RequestID: "member-1", TeamID: team.ID, Name: "builder", Role: "implementation", Backend: "tmux", PermissionMode: "accept-edits"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Send(collaboration.SendInput{RequestID: "message-1", TeamID: team.ID, Sender: collaboration.Principal{Kind: collaboration.PrincipalOwner, ID: "owner"}, Recipients: []string{member.ID}, Kind: collaboration.MessageDirect, Body: "status?"})
	if err != nil {
		t.Fatal(err)
	}

	got := withCollaborationStatus(tui.Status{}, &app.App{SessionID: "session-1", Collaboration: store})
	if len(got.Teams) != 1 || got.Teams[0].Name != "alpha" || got.Teams[0].Unread != 1 {
		t.Fatalf("unexpected teams: %#v", got.Teams)
	}
	if len(got.Teams[0].Members) != 1 {
		t.Fatalf("unexpected members: %#v", got.Teams[0].Members)
	}
	m := got.Teams[0].Members[0]
	if m.Name != "builder" || m.Backend != "tmux" || m.PermissionMode != "accept-edits" || m.Unread != 1 {
		t.Fatalf("unexpected member: %#v", m)
	}
}
