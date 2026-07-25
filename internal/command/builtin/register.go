package builtin

import "github.com/junnhwan/bond-code/internal/command"

func RegisterAll(registry *command.Registry) error {
	for _, cmd := range []command.Command{
		ClearSessionCommand(),
		CompactCommand(),
		ContextCommand(),
		CostCommand(),
		ExportCommand(),
		HelpCommand(),
		MemoryCommand(),
		ModelCommand(),
		NewSessionCommand(),
		PermissionsCommand(),
		ResumeCommand(),
		SessionCommand(),
		SessionsCommand(),
		SkillsCommand(),
		StatusCommand(),
		UndoCommand(),
	} {
		if err := registry.Register(cmd); err != nil {
			return err
		}
	}
	return nil
}
