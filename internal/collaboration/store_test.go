package collaboration

import (
	"path/filepath"
	"testing"
)

func TestStoreCreatesTeamAndRejectsDuplicateMemberName(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "collaboration.json"))
	if err != nil {
		t.Fatal(err)
	}
	team, err := s.CreateTeam(CreateTeamInput{RequestID: "create-1", Name: "alpha", SessionID: "s1", OwnerID: "owner", Objective: "ship"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.AddMember(AddMemberInput{RequestID: "member-1", TeamID: team.ID, Name: "builder", Role: "worker", Profile: "general", Backend: "in_process", PermissionMode: "default"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.AddMember(AddMemberInput{RequestID: "member-1", TeamID: team.ID, Name: "ignored"})
	if err != nil || again.ID != first.ID {
		t.Fatalf("idempotent add = %#v, %v", again, err)
	}
	if _, err = s.AddMember(AddMemberInput{RequestID: "member-2", TeamID: team.ID, Name: "builder"}); err != ErrDuplicateMemberName {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestMailboxBroadcastSnapshotAndStableInboxSequence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collaboration.json")
	s, _ := Open(path)
	team, _ := s.CreateTeam(CreateTeamInput{RequestID: "t", Name: "alpha", SessionID: "s", OwnerID: "owner"})
	a, _ := s.AddMember(AddMemberInput{RequestID: "a", TeamID: team.ID, Name: "a"})
	b, _ := s.AddMember(AddMemberInput{RequestID: "b", TeamID: team.ID, Name: "b"})
	msg, err := s.Send(SendInput{RequestID: "msg-1", TeamID: team.ID, Sender: Principal{Kind: PrincipalOwner, ID: "owner"}, Broadcast: true, Kind: MessageBroadcast, Body: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Recipients) != 2 {
		t.Fatalf("recipients = %v", msg.Recipients)
	}
	if _, err = s.AddMember(AddMemberInput{RequestID: "c", TeamID: team.ID, Name: "c"}); err != nil {
		t.Fatal(err)
	}
	inbox, err := s.Inbox(team.ID, a.ID, 0, false)
	if err != nil || len(inbox) != 1 || inbox[0].Sequence != 1 || inbox[0].Message.ID != msg.ID {
		t.Fatalf("inbox = %#v, %v", inbox, err)
	}
	binbox, _ := s.Inbox(team.ID, b.ID, 0, false)
	if len(binbox) != 1 || binbox[0].Sequence != 1 {
		t.Fatalf("b inbox = %#v", binbox)
	}
	if err = s.MarkRead(team.ID, a.ID, msg.ID); err != nil {
		t.Fatal(err)
	}
	unread, _ := s.Inbox(team.ID, a.ID, 0, true)
	if len(unread) != 0 {
		t.Fatalf("unread = %#v", unread)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	persisted, _ := reopened.Inbox(team.ID, b.ID, 0, true)
	if len(persisted) != 1 || persisted[0].Message.Body != "hello" {
		t.Fatalf("persisted = %#v", persisted)
	}
}

func TestSenderAuthorizationAndImmutableRecipientScope(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "collaboration.json"))
	team, _ := s.CreateTeam(CreateTeamInput{RequestID: "t", Name: "alpha", SessionID: "s", OwnerID: "owner"})
	a, _ := s.AddMember(AddMemberInput{RequestID: "a", TeamID: team.ID, Name: "a"})
	if _, err := s.Send(SendInput{RequestID: "x", TeamID: team.ID, Sender: Principal{Kind: PrincipalMember, ID: "outsider"}, Recipients: []string{a.ID}, Body: "bad"}); err != ErrUnauthorized {
		t.Fatalf("error = %v", err)
	}
	if _, err := s.Send(SendInput{RequestID: "y", TeamID: team.ID, Sender: Principal{Kind: PrincipalMember, ID: a.ID}, Recipients: []string{"missing"}, Body: "bad"}); err != ErrMemberNotFound {
		t.Fatalf("error = %v", err)
	}
}

func TestAssignmentFencesGenerationAndOneActivePrimaryTask(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "collaboration.json"))
	team, _ := s.CreateTeam(CreateTeamInput{RequestID: "t", Name: "alpha", SessionID: "s", OwnerID: "owner"})
	member, _ := s.AddMember(AddMemberInput{RequestID: "m", TeamID: team.ID, Name: "builder"})
	assignment, err := s.Assign(AssignInput{RequestID: "assign-1", TeamID: team.ID, MemberID: member.ID, TaskID: "task-1", Generation: 1, Issuer: Principal{Kind: PrincipalOwner, ID: "owner"}})
	if err != nil {
		t.Fatal(err)
	}
	if assignment.TaskID != "task-1" || assignment.Generation != 1 {
		t.Fatalf("assignment = %#v", assignment)
	}
	if _, err = s.Assign(AssignInput{RequestID: "assign-2", TeamID: team.ID, MemberID: member.ID, TaskID: "task-2", Generation: 1, Issuer: Principal{Kind: PrincipalOwner, ID: "owner"}}); err != ErrMemberBusy {
		t.Fatalf("busy error = %v", err)
	}
	if err = s.CompleteAssignment(team.ID, member.ID, "task-1", 2); err != ErrStaleTaskGeneration {
		t.Fatalf("stale error = %v", err)
	}
	if err = s.CompleteAssignment(team.ID, member.ID, "task-1", 1); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Assign(AssignInput{RequestID: "assign-2", TeamID: team.ID, MemberID: member.ID, TaskID: "task-2", Generation: 1, Issuer: Principal{Kind: PrincipalOwner, ID: "owner"}}); err != nil {
		t.Fatal(err)
	}
}

func TestTeamDeletionRequiresStoppedMembers(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "collaboration.json"))
	team, _ := s.CreateTeam(CreateTeamInput{RequestID: "t", Name: "alpha", SessionID: "s", OwnerID: "owner"})
	member, _ := s.AddMember(AddMemberInput{RequestID: "m", TeamID: team.ID, Name: "builder"})
	if _, err := s.RequestShutdown(ShutdownInput{RequestID: "stop", TeamID: team.ID, MemberID: member.ID, Issuer: Principal{Kind: PrincipalOwner, ID: "owner"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTeam(team.ID, Principal{Kind: PrincipalOwner, ID: "owner"}); err != ErrLiveMembers {
		t.Fatalf("delete error = %v", err)
	}
	if err := s.AcknowledgeShutdown(team.ID, member.ID, Principal{Kind: PrincipalMember, ID: member.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTeam(team.ID, Principal{Kind: PrincipalOwner, ID: "owner"}); err != nil {
		t.Fatal(err)
	}
	got, ok := s.GetTeam(team.ID)
	if !ok || got.State != TeamDeleted {
		t.Fatalf("team = %#v, %v", got, ok)
	}
}
