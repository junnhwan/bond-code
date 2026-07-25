package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/command"
	"github.com/junnhwan/bond-code/internal/skill"
)

func SkillsCommand() command.Command {
	return command.Command{
		Name:        "skills",
		Description: "List or show local SKILL.md skills (BondCode dirs only)",
		RemoteSafe:  true,
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			if env.SkillLoader == nil {
				return command.Result{}, fmt.Errorf("skill loader is not configured")
			}
			if len(args) == 0 {
				index, err := env.SkillLoader.IndexAll()
				if err != nil {
					return command.Result{}, err
				}
				var b strings.Builder
				if roots := env.SkillLoader.Roots(); len(roots) > 0 {
					b.WriteString("Roots:\n")
					for _, r := range roots {
						b.WriteString("  ")
						b.WriteString(r)
						b.WriteByte('\n')
					}
					b.WriteByte('\n')
				}
				b.WriteString(skill.FormatIndex(index))
				return command.Result{Output: b.String()}, nil
			}
			switch strings.TrimSpace(args[0]) {
			case "show":
				if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
					return command.Result{}, fmt.Errorf("usage: /skills show <name>")
				}
				item, err := env.SkillLoader.Load(args[1])
				if err != nil {
					return command.Result{}, err
				}
				content, _, err := env.SkillLoader.Expand(item.Name, "")
				if err != nil {
					// Still show body for user-only skills via Load.
					content = item.Body
					if item.Dir != "" {
						content = "Base directory for this skill: " + item.Dir + "\n\n" + content
					}
				}
				if env.SkillMaxChars > 0 && len(content) > env.SkillMaxChars {
					content = content[:env.SkillMaxChars] + "\n[skill content truncated]"
				}
				return command.Result{Output: content}, nil
			default:
				return command.Result{}, fmt.Errorf("unknown skills command: %s (try /skills or /skills show <name>)", args[0])
			}
		},
	}
}
