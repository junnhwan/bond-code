package builtin

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/junnhwan/bond-code/internal/command"
)

// ExportCommand renders the current session as a human-readable text file. With
// no argument it writes to <project>/bondcode-session-<id>.txt; passing a path
// overrides the destination (relative paths resolve against the project root).
func ExportCommand() command.Command {
	return command.Command{
		Name:        "export",
		Description: "Export the current session to a text file",
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			if env.Sessions == nil || strings.TrimSpace(env.SessionID) == "" {
				return command.Result{Output: "no active session to export"}, nil
			}
			target := exportTargetPath(env, args)
			summary, err := env.Sessions.ExportText(env.SessionID, target)
			if err != nil {
				return command.Result{}, err
			}
			return command.Result{Output: fmt.Sprintf(
				"exported %d messages (%d user, %d assistant) and %d tool calls to %s",
				summary.TotalMessages(), summary.UserMessages, summary.AssistantMessages, summary.ToolCalls, summary.Path,
			)}, nil
		},
	}
}

func exportTargetPath(env command.Env, args []string) string {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		arg := strings.TrimSpace(args[0])
		if filepath.IsAbs(arg) {
			return arg
		}
		if dir := strings.TrimSpace(env.ProjectRoot); dir != "" {
			return filepath.Join(dir, arg)
		}
		return arg
	}
	name := fmt.Sprintf("bondcode-%s.txt", env.SessionID)
	if dir := strings.TrimSpace(env.ProjectRoot); dir != "" {
		return filepath.Join(dir, name)
	}
	return name
}
