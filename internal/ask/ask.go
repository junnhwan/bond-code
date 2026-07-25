// Package ask provides a structured way for the agent to ask the user a
// clarifying question with a fixed set of choices, used during planning or
// whenever the right next step depends on user intent.
//
// The agent invokes it through the ask_user tool. The tool's Execute blocks on
// a Questioner until the UI returns the user's selection, then hands that
// selection back as the tool result so the normal agent loop continues without
// any special-casing.
package ask

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/junnhwan/bond-code/internal/tool"
)

// Question is a clarifying question the agent wants to put to the user.
type Question struct {
	Prompt  string   `json:"prompt"`
	Options []Option `json:"options"`
	// Multi allows more than one option to be selected.
	Multi bool `json:"multi,omitempty"`
}

// Option is one selectable choice. Label is the short pick; Description is the
// optional rationale shown beneath it.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Answer is the set of selected option indices (0-based). It has length 1
// unless the question was Multi.
type Answer []int

// Questioner asks the user a question and blocks until they answer (or the
// context is cancelled). The UI implements this; the agent/tools depend only
// on the interface so there is no import cycle with the TUI package.
type Questioner interface {
	Ask(ctx context.Context, q Question) (Answer, error)
}

// AskUserTool is the agent-facing tool that surfaces a Question to the user via
// the configured Questioner.
type AskUserTool struct {
	questioner Questioner
}

// NewAskUserTool builds an ask_user tool backed by the given Questioner. A nil
// Questioner is allowed (the tool reports an error if invoked) so callers that
// run without a UI can still construct the tool.
func NewAskUserTool(q Questioner) *AskUserTool {
	return &AskUserTool{questioner: q}
}

func (t *AskUserTool) Name() string { return tool.AskUser }

func (t *AskUserTool) Description() string {
	return "Ask the user a clarifying question with a small set of choices. " +
		"Use it when you must decide between approaches and the right choice " +
		"depends on user intent; the selected option(s) come back as the result."
}

func (t *AskUserTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "The question to ask the user.",
			},
			"options": map[string]any{
				"type":     "array",
				"minItems": 1,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"label":       map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
					},
					"required": []string{"label"},
				},
			},
			"multi": map[string]any{
				"type":        "boolean",
				"description": "Allow more than one option to be selected.",
			},
		},
		"required": []string{"prompt", "options"},
	}
}

func (t *AskUserTool) Risk(json.RawMessage) tool.RiskLevel { return tool.RiskLow }

func (t *AskUserTool) Execute(ctx context.Context, raw json.RawMessage) (*tool.Result, error) {
	if t.questioner == nil {
		return tool.ErrorResult("ask_user", "questioner unavailable", "no questioner is configured"), nil
	}
	var args struct {
		Prompt  string   `json:"prompt"`
		Options []Option `json:"options"`
		Multi   bool     `json:"multi"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tool.ErrorResult("ask_user", "invalid arguments", err.Error()), nil
	}
	args.Prompt = strings.TrimSpace(args.Prompt)
	if args.Prompt == "" {
		return tool.ErrorResult("ask_user", "missing prompt", "prompt is required"), nil
	}
	if len(args.Options) == 0 {
		return tool.ErrorResult("ask_user", "missing options", "at least one option is required"), nil
	}

	answer, err := t.questioner.Ask(ctx, Question{Prompt: args.Prompt, Options: args.Options, Multi: args.Multi})
	if err != nil {
		return tool.ErrorResult("ask_user", "ask failed", err.Error()), nil
	}
	labels := make([]string, 0, len(answer))
	for _, idx := range answer {
		if idx >= 0 && idx < len(args.Options) {
			labels = append(labels, args.Options[idx].Label)
		}
	}
	if len(labels) == 0 {
		return tool.ErrorResult("ask_user", "no selection", "user selected nothing"), nil
	}
	summary := fmt.Sprintf("user selected: %s", strings.Join(labels, ", "))
	return tool.Success("ask_user", summary, strings.Join(labels, "\n")), nil
}
