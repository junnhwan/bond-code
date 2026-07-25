package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/command"
	"github.com/junnhwan/bond-code/internal/memory"
)

func MemoryCommand() command.Command {
	return command.Command{
		Name:        "memory",
		Description: "Show file-based long-term memory (memdir)",
		RemoteSafe:  true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			if env.MemoryStore == nil {
				return command.Result{}, fmt.Errorf("memory store is not configured")
			}
			maxChars := env.MemoryMaxChars
			if maxChars <= 0 {
				maxChars = 4000
			}
			if len(args) == 0 {
				content, err := env.MemoryStore.GetMemoryContext(maxChars)
				if err != nil {
					return command.Result{}, err
				}
				if content == "" {
					content = "No memory saved. Topic files will appear under the project memory/ directory; MEMORY.md is the index."
				}
				return command.Result{Output: content}, nil
			}
			switch args[0] {
			case "compact":
				out, err := env.MemoryStore.RebuildIndex()
				if err != nil {
					return command.Result{}, err
				}
				if out == "" {
					out = "Memory index rebuilt (empty)."
				} else {
					out = "Memory index rebuilt:\n" + out
				}
				return command.Result{Output: out}, nil
			case "list":
				files, err := env.MemoryStore.List()
				if err != nil {
					return command.Result{}, err
				}
				return command.Result{Output: renderMemoryFiles(files, "No topic memories.")}, nil
			case "search":
				query := strings.TrimSpace(strings.Join(args[1:], " "))
				files, err := env.MemoryStore.Search(memory.SearchOptions{Query: query, Limit: 20, MaxChars: maxChars})
				if err != nil {
					return command.Result{}, err
				}
				return command.Result{Output: renderMemoryFiles(files, "No matching memories.")}, nil
			case "show":
				if len(args) < 2 {
					return command.Result{}, fmt.Errorf("usage: /memory show <filename>")
				}
				f, err := env.MemoryStore.Read(args[1])
				if err != nil {
					return command.Result{}, err
				}
				return command.Result{Output: renderMemoryFiles([]memory.MemoryFile{*f}, "")}, nil
			case "delete":
				if len(args) < 2 {
					return command.Result{}, fmt.Errorf("usage: /memory delete <filename>")
				}
				if err := env.MemoryStore.Delete(args[1]); err != nil {
					return command.Result{}, err
				}
				return command.Result{Output: "deleted " + args[1]}, nil
			case "dir":
				return command.Result{Output: env.MemoryStore.Dir()}, nil
			default:
				return command.Result{}, fmt.Errorf("unknown memory command: %s (try list|search|show|compact|delete|dir)", args[0])
			}
		},
	}
}

func renderMemoryFiles(files []memory.MemoryFile, empty string) string {
	if len(files) == 0 {
		return empty
	}
	var b strings.Builder
	for i, f := range files {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(f.Filename)
		b.WriteString(" [")
		b.WriteString(string(f.Type))
		b.WriteString("] · ")
		b.WriteString(memory.AgeText(f.MtimeMs))
		b.WriteString("\n")
		if f.Description != "" {
			b.WriteString(f.Description)
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimSpace(f.Body))
	}
	return b.String()
}
