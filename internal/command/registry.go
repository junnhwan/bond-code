package command

import (
	"fmt"
	"sort"
)

type Registry struct {
	commands map[string]Command
}

func NewRegistry() *Registry {
	return &Registry{commands: map[string]Command{}}
}

func (r *Registry) Register(cmd Command) error {
	if cmd.Name == "" {
		return fmt.Errorf("command name is required")
	}
	if cmd.Run == nil && cmd.PromptTemplate == "" {
		return fmt.Errorf("command %q has no runner or prompt template", cmd.Name)
	}
	if _, ok := r.commands[cmd.Name]; ok {
		return fmt.Errorf("command %q already registered", cmd.Name)
	}
	r.commands[cmd.Name] = cmd
	return nil
}

func (r *Registry) Get(name string) (Command, bool) {
	cmd, ok := r.commands[name]
	return cmd, ok
}

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.commands))
	for name := range r.commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// List returns all commands sorted by name
func (r *Registry) List() []Command {
	cmds := make([]Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		cmds = append(cmds, cmd)
	}
	sort.Slice(cmds, func(i, j int) bool {
		return cmds[i].Name < cmds[j].Name
	})
	return cmds
}
