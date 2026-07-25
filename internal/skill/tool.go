package skill

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/junnhwan/bond-code/internal/tool"
)

// Tool is the Claude Code-style Skill tool: one call expands a skill into the
// conversation (inline). Discovery is via the dynamic skill listing reminder,
// not a separate skill_list tool.
type Tool struct {
	loader *Loader
}

func NewTool(loader *Loader) *Tool {
	return &Tool{loader: loader}
}

func (t *Tool) Name() string { return tool.Skill }

func (t *Tool) Description() string {
	return `Execute a skill within the main conversation.

When users ask you to perform tasks, check if any of the available skills match. Skills provide specialized capabilities and domain knowledge.

When users reference a "slash command" or "/<something>" (e.g., "/commit"), they are referring to a skill — invoke this tool with that skill name.

How to invoke:
- skill: the skill name (e.g. "commit", "review-pr")
- args: optional arguments string passed into the skill body

Important:
- Available skills are listed in system-reminder messages (## Available Skills)
- When a skill matches the user's request, invoke this tool BEFORE generating other work about that task
- Do not invoke a skill that is already running in this turn
- Do not use this tool for built-in CLI slash commands like /help or /clear`
}

func (t *Tool) Schema() any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"skill": map[string]any{
				"type":        "string",
				"description": "The skill name. E.g. \"commit\", \"review-pr\", or \"pdf\".",
			},
			"args": map[string]any{
				"type":        "string",
				"description": "Optional arguments for the skill (substituted for $ARGUMENTS).",
			},
		},
		"required": []string{"skill"},
	}
}

func (t *Tool) Risk(json.RawMessage) tool.RiskLevel { return tool.RiskLow }

func (t *Tool) Execute(ctx context.Context, args json.RawMessage) (*tool.Result, error) {
	if t.loader == nil {
		return tool.ErrorResult(t.Name(), "skill loader unavailable", "skill loader is nil"), nil
	}
	var parsed struct {
		Skill string `json:"skill"`
		Args  string `json:"args"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &parsed); err != nil {
			return tool.ErrorResult(t.Name(), "invalid JSON arguments", err.Error()), nil
		}
	}
	name := strings.TrimSpace(parsed.Skill)
	name = strings.TrimPrefix(name, "/")
	if name == "" {
		return tool.ErrorResult(t.Name(), "missing skill name", "skill is required"), nil
	}
	content, s, err := t.loader.Expand(name, parsed.Args)
	if err != nil {
		return tool.ErrorResult(t.Name(), "skill invoke failed", err.Error()), nil
	}
	result := tool.Success(t.Name(), "loaded skill "+s.Name, content)
	result.Metadata = map[string]any{
		"skill":         s.Name,
		"description":   s.Description,
		"path":          s.Path,
		"dir":           s.Dir,
		"source":        string(s.Source),
		"status":        "inline",
		"allowed_tools": s.AllowedTools,
	}
	if parsed.Args != "" {
		result.Metadata["args"] = parsed.Args
	}
	return result, nil
}
