package builtin

import (
	"context"

	"github.com/junnhwan/bond-code/internal/command"
)

// HelpCommand gives the TUI a first-class, searchable help surface without
// coupling command packages to TUI styling. The TUI renders Panel; CLI/once paths
// use Output.
func HelpCommand() command.Command {
	descriptor, _ := command.LookupSurfaceDescriptor("help")
	return command.Command{
		Name:        descriptor.Name,
		Description: descriptor.Description,
		RemoteSafe:  true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			panel := helpPanel()
			return command.Result{Output: renderPanelText(panel), Panel: panel}, nil
		},
	}
}

func helpPanel() *command.Panel {
	commandDescriptors := command.DiscoverableSurfaceDescriptors()
	commandRows := make([]command.PanelRow, len(commandDescriptors))
	for i, descriptor := range commandDescriptors {
		commandRows[i] = command.PanelRow{Key: descriptor.Shortcut, Value: descriptor.Description}
	}

	keyDescriptors := command.DirectKeyDescriptors()
	keyRows := make([]command.PanelRow, len(keyDescriptors))
	for i, descriptor := range keyDescriptors {
		keyRows[i] = command.PanelRow{Key: descriptor.DisplayShortcut, Value: descriptor.Description}
	}

	return &command.Panel{
		Title: "help",
		Sections: []command.PanelSection{
			{Label: "COMMANDS", Rows: commandRows},
			{Label: "KEYS", Rows: keyRows},
		},
	}
}
