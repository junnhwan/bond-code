package collaboration

import (
	"context"
	"fmt"

	"github.com/junnhwan/bond-code/internal/agenttask"
)

type TaskInputJournal struct{ store *Store }

func NewTaskInputJournal(store *Store) *TaskInputJournal { return &TaskInputJournal{store: store} }
func (j *TaskInputJournal) RecordInput(_ context.Context, task agenttask.Task, input string) error {
	if task.TeamID == "" && task.MemberID == "" {
		return nil
	}
	if j == nil || j.store == nil {
		return fmt.Errorf("collaboration store is required")
	}
	if task.TeamID == "" || task.MemberID == "" || task.OwnerID == "" {
		return fmt.Errorf("team task input requires team, member, and owner identity")
	}
	_, err := j.store.Send(SendInput{RequestID: newID("task-input"), TeamID: task.TeamID, Sender: Principal{Kind: PrincipalOwner, ID: task.OwnerID}, Recipients: []string{task.MemberID}, Kind: MessageDirect, Body: input, TaskID: task.ID, Generation: task.Generation})
	return err
}
