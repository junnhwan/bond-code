package builtin

import (
	"context"
	"fmt"

	"github.com/junnhwan/bond-code/internal/command"
)

// NewSessionCommand starts a fresh empty session inside the running TUI.
func NewSessionCommand() command.Command {
	return newSessionCommand("new", "Start a fresh empty session")
}

// ClearSessionCommand is an OpenCode-style alias for starting a fresh session.
func ClearSessionCommand() command.Command {
	return newSessionCommand("clear", "Clear the current transcript and start fresh")
}

func newSessionCommand(name, description string) command.Command {
	return command.Command{
		Name:        name,
		Description: description,
		RemoteSafe:  true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			if env.NewSession == nil {
				return command.Result{Output: fmt.Sprintf("%s session is not available here", name)}, nil
			}
			id, err := env.NewSession()
			if err != nil {
				return command.Result{}, err
			}
			return command.Result{
				Output:          fmt.Sprintf("started new session %s", id),
				SessionSwitched: &id,
			}, nil
		},
	}
}
