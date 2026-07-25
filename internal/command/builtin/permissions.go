package builtin

import (
	"context"
	"fmt"

	"github.com/junnhwan/bond-code/internal/command"
)

func PermissionsCommand() command.Command {
	return command.Command{
		Name:        "permissions",
		Description: "Show permission mode",
		RemoteSafe:  true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			if len(args) == 0 {
				return command.Result{Output: fmt.Sprintf("permission mode: %s", env.PermissionMode)}, nil
			}
			if len(args) != 1 {
				return command.Result{}, fmt.Errorf("usage: /permissions [default|accept-edits|plan|bypass]")
			}
			if env.SetPermissionMode == nil {
				return command.Result{}, fmt.Errorf("runtime permission switching is unavailable")
			}
			if err := env.SetPermissionMode(args[0]); err != nil {
				return command.Result{}, err
			}
			mode := args[0]
			return command.Result{Output: fmt.Sprintf("permission mode: %s", mode), PermissionModeChanged: &mode}, nil
		},
	}
}
