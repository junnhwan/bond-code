package builtin

import (
	"context"
	"fmt"
	"os"

	"github.com/junnhwan/bond-code/internal/command"
	"github.com/junnhwan/bond-code/internal/undo"
)

// UndoCommand reverts the most recent file write/edit by restoring the
// pre-write content the file tools stashed (/undo). It is a safety net for when
// the agent overwrote something unexpected and git is not an option. The
// restore is a direct write — a user-initiated revert like /copy, not an agent
// tool call — so it intentionally does not flow through Policy/Confirmer.
//
// Limitation: the store is in-memory and per-process, so /undo cannot reach
// writes from a previous session or before the process started. File creation
// (no prior content) is not recorded, so /undo cannot undo a brand-new file.
func UndoCommand() command.Command {
	return newUndoCommand(undo.Default, os.WriteFile)
}

func newUndoCommand(store *undo.Store, writeFile func(string, []byte, os.FileMode) error) command.Command {
	return command.Command{
		Name:        "undo",
		Description: "Revert the most recent file write/edit (restore pre-write content)",
		RemoteSafe:  true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			snapshot, err := store.Restore(func(snapshot undo.Snapshot) error {
				if err := writeFile(snapshot.Path, snapshot.Old, 0o600); err != nil {
					return fmt.Errorf("restore %s: %w", snapshot.Path, err)
				}
				return nil
			})
			if err != nil {
				return command.Result{}, err
			}
			if snapshot == nil {
				return command.Result{Output: "nothing to undo (no prior write recorded this session)"}, nil
			}
			return command.Result{Output: fmt.Sprintf("reverted %s to its pre-write state (%d bytes)", snapshot.Path, len(snapshot.Old))}, nil
		},
	}
}
