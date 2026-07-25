package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/command"
)

// ModelCommand switches the active model without restarting the TUI
// (`/model <name>`). With no argument it prints the current model plus any
// configured fallback models as suggestions. The switch itself is delegated to
// env.SwitchModel (wired to app.SwitchModel in the TUI; nil in headless/--once,
// where the command reports it is unavailable). RemoteSafe: it only reconfigures
// the local client, no network call.
func ModelCommand() command.Command {
	return command.Command{
		Name:        "model",
		Description: "Switch the active model without restarting (/model <name>; no arg shows current)",
		RemoteSafe:  true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			current := strings.TrimSpace(env.Model)
			if len(args) == 0 {
				out := fmt.Sprintf("current model: %s", nonEmptyOrDefault(current, "(unset)"))
				if suggestions := nonEmptyValues(env.ModelSuggestions); len(suggestions) > 0 {
					out += "\nfallback models: " + strings.Join(suggestions, ", ")
				}
				out += "\nusage: /model <name>"
				return command.Result{Output: out}, nil
			}
			if env.SwitchModel == nil {
				return command.Result{Output: "model switch is not available here"}, nil
			}
			model := strings.TrimSpace(strings.Join(args, " "))
			if model == "" {
				return command.Result{}, fmt.Errorf("model name is empty")
			}
			if err := env.SwitchModel(model); err != nil {
				return command.Result{}, err
			}
			switched := model
			return command.Result{
				Output:        fmt.Sprintf("switched model to %s", model),
				ModelSwitched: &switched,
			}, nil
		},
	}
}

func nonEmptyOrDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func nonEmptyValues(values []string) []string {
	var out []string
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}
