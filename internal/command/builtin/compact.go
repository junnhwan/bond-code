package builtin

import (
	"context"

	"github.com/junnhwan/bond-code/internal/command"
)

func CompactCommand() command.Command {
	return command.Command{
		Name:        "compact",
		Description: "Compact prompt context",
		RemoteSafe:  true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{Output: "context compaction requested"}, nil
		},
	}
}
