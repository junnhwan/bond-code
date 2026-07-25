package collaboration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/junnhwan/bond-code/internal/agenttask"
)

func TestTaskInputJournalPersistsControlMessage(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "collaboration.json"))
	team, _ := s.CreateTeam(CreateTeamInput{RequestID: "t", Name: "alpha", SessionID: "s", OwnerID: "owner"})
	member, _ := s.AddMember(AddMemberInput{RequestID: "m", TeamID: team.ID, Name: "worker"})
	journal := NewTaskInputJournal(s)
	err := journal.RecordInput(context.Background(), agenttask.Task{ID: "task", Generation: 2, TeamID: team.ID, MemberID: member.ID, OwnerID: "owner"}, "focus")
	if err != nil {
		t.Fatal(err)
	}
	inbox, _ := s.Inbox(team.ID, member.ID, 0, true)
	if len(inbox) != 1 || inbox[0].Message.Body != "focus" || inbox[0].Message.TaskID != "task" || inbox[0].Message.Generation != 2 {
		t.Fatalf("inbox = %#v", inbox)
	}
}
