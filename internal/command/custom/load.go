// Package custom loads user-defined slash commands from .bondcode/commands/*.md
// files (project-level ./ and user-level ~/), registering each as a
// prompt-injecting command.Command. A custom command is just a prompt template:
// the TUI substitutes $ARGUMENTS / $1 / $2 ... and submits the result as a user
// turn, so custom commands never bypass the agent's safety or context layers —
// they are equivalent to the user typing the expanded prompt.
package custom

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/junnhwan/bond-code/internal/command"
)

// Load scans project-level then user-level command directories and registers
// each *.md file as a prompt-injecting command. Project-level entries override
// user-level on name collision; collisions with builtins or earlier custom
// commands are skipped (not fatal) so a bad file never breaks startup.
func Load(registry *command.Registry) error {
	seen := map[string]bool{}
	for _, dir := range commandDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), ".md")
			if name == "" || seen[name] {
				continue
			}
			cmd, err := parseFile(filepath.Join(dir, entry.Name()), name)
			if err != nil {
				continue
			}
			if err := registry.Register(cmd); err != nil {
				continue
			}
			seen[name] = true
		}
	}
	return nil
}

// commandDirs returns the custom-command search paths in priority order:
// project-level (.bondcode/commands) before user-level (~/.bondcode/commands).
func commandDirs() []string {
	var dirs []string
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, filepath.Join(wd, ".bondcode", "commands"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, ".bondcode", "commands"))
	}
	return dirs
}

func parseFile(path, fallbackName string) (command.Command, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return command.Command{}, err
	}
	name, description, body := splitFrontmatter(string(data))
	if strings.TrimSpace(name) == "" {
		name = fallbackName
	}
	if strings.TrimSpace(body) == "" {
		return command.Command{}, fmt.Errorf("custom command %q has empty body", fallbackName)
	}
	return command.Command{
		Name:           name,
		Description:    description,
		PromptTemplate: body,
	}, nil
}

// splitFrontmatter separates an optional leading "---\nkey: value\n---\n" block
// (simple YAML, name/description only) from the prompt-template body.
func splitFrontmatter(content string) (name, description, body string) {
	body = strings.TrimSpace(content)
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---") {
		return
	}
	rest := strings.TrimPrefix(trimmed, "---")
	// The closing fence is a line that is exactly "---".
	closeIdx := strings.Index(rest, "\n---")
	if closeIdx < 0 {
		return
	}
	frontmatter := rest[:closeIdx]
	body = strings.TrimSpace(rest[closeIdx+len("\n---"):])
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(strings.Trim(val, `"`))
		switch key {
		case "name":
			name = val
		case "description":
			description = val
		}
	}
	return
}

// SubstituteArgs replaces $ARGUMENTS (all args joined by spaces) and positional
// $1, $2, ... placeholders in a prompt template with the supplied args. Missing
// positional args expand to empty.
func SubstituteArgs(template string, args []string) string {
	out := strings.ReplaceAll(template, "$ARGUMENTS", strings.Join(args, " "))
	for i, arg := range args {
		out = strings.ReplaceAll(out, "$"+strconv.Itoa(i+1), arg)
	}
	return out
}
