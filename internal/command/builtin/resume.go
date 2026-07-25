package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/command"
)

// ResumeCommand implements /resume, the in-app seamless session switch:
//   - /resume (no args): list previous sessions reusing the /sessions rendering
//     (preview + relative age + count), but with an in-app footer pointing at
//     /resume <id> instead of the CLI flag.
//   - /resume <id>: hot-switch the running app onto that session so the
//     conversation continues there without exiting and restarting. The switch
//     itself runs in app.SwitchSession (via env.SwitchSession); this command
//     only validates input, invokes it, and signals the TUI to reset its view
//     via Result.SessionSwitched (the seed is re-fetched by the TUI, never
//     carried here, so command stays free of tui imports).
//
// This is the in-app counterpart to `bondcode --resume <id>` (which starts
// a new process) and to ctrl+h fork-resume (which branches first). /resume
// continues the target session in place.
func ResumeCommand() command.Command {
	return command.Command{
		Name:        "resume",
		Description: "List sessions or switch to a session by id",
		RemoteSafe:  true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			if len(args) == 0 {
				out, err := listSessionsOutput(env, "resume with: /resume <id>")
				if err != nil {
					return command.Result{}, err
				}
				// TUI opens the interactive session-manager overlay from this
				// signal; headless falls back to the text list in Output.
				return command.Result{Output: out, OpenSessionManager: true}, nil
			}
			id := strings.TrimSpace(args[0])
			if id == "" {
				return command.Result{Output: "usage: /resume <session-id>"}, nil
			}
			// Headless / --once builds have no running app to switch, so the env
			// carries no SwitchSession callback; the TUI always injects one.
			if env.SwitchSession == nil {
				return command.Result{Output: "session switching is not available here"}, nil
			}
			if err := env.SwitchSession(id); err != nil {
				return command.Result{}, err
			}
			switched := id
			return command.Result{
				Output:          fmt.Sprintf("switched to %s", id),
				SessionSwitched: &switched,
			}, nil
		},
	}
}
