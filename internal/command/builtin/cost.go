package builtin

import (
	"context"
	"fmt"

	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/command"
)

func CostCommand() command.Command {
	return command.Command{
		Name:        "cost",
		Description: "Show cumulative model token usage",
		RemoteSafe:  true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			var snap app.RuntimeStatus
			if env.StatusProvider != nil {
				snap = env.StatusProvider.StatusSnapshot()
			}
			panel := costPanel(snap)
			return command.Result{Output: renderPanelText(panel), Panel: panel}, nil
		},
	}
}

func costPanel(s app.RuntimeStatus) *command.Panel {
	usage := s.Usage
	if usage.ModelCalls == 0 {
		return &command.Panel{
			Title: "cost",
			Sections: []command.PanelSection{{
				Label: "MODEL USAGE",
				Rows: []command.PanelRow{
					{Key: "model calls", Value: "not measured yet"},
				},
			}},
		}
	}
	total := usage.TotalInputTokens + usage.TotalOutputTokens
	return &command.Panel{
		Title: "cost",
		Sections: []command.PanelSection{
			{Label: "MODEL USAGE", Rows: []command.PanelRow{
				{Key: "model", Value: orDash(s.Model)},
				{Key: "model calls", Value: fmt.Sprintf("%d", usage.ModelCalls)},
				{Key: "input", Value: fmt.Sprintf("%d tokens", usage.TotalInputTokens)},
				{Key: "output", Value: fmt.Sprintf("%d tokens", usage.TotalOutputTokens)},
				{Key: "total", Value: fmt.Sprintf("%d tokens", total)},
				{Key: "last call", Value: fmt.Sprintf("%d in / %d out", usage.LastInputTokens, usage.LastOutputTokens)},
			}},
		},
	}
}
