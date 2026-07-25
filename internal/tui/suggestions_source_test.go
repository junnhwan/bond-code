package tui

import (
	"context"
	"testing"

	"github.com/junnhwan/bond-code/internal/command"
)

func TestSuggestionsSourceUsesCanonicalBuiltinsAndCustomTemplates(t *testing.T) {
	reg := command.NewRegistry()
	if err := reg.Register(command.Command{
		Name:        "status",
		Description: "registry status description",
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(command.Command{
		Name:        "legacy",
		Description: "non-surface registry command",
		Run: func(ctx context.Context, env command.Env, args []string) (command.Result, error) {
			return command.Result{}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(command.Command{Name: "mycmd", Description: "custom cmd", PromptTemplate: "do $ARGUMENTS"}); err != nil {
		t.Fatal(err)
	}

	sl := NewSuggestionList(reg)
	items := map[string]Suggestion{}
	for _, suggestion := range sl.commandItems {
		items[suggestion.Name] = suggestion
	}

	tests := []struct {
		name        string
		wantSource  string
		wantDesc    string
		wantPresent bool
	}{
		{name: "status", wantSource: "builtin", wantDesc: "Show current runtime status", wantPresent: true},
		{name: "retry", wantSource: "builtin", wantDesc: "Rerun latest failed turn", wantPresent: true},
		{name: "mycmd", wantSource: "custom", wantDesc: "custom cmd", wantPresent: true},
		{name: "legacy", wantPresent: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item, ok := items[tt.name]
			if ok != tt.wantPresent {
				t.Fatalf("presence = %v, want %v", ok, tt.wantPresent)
			}
			if !ok {
				return
			}
			if item.Source != tt.wantSource {
				t.Errorf("Source = %q, want %q", item.Source, tt.wantSource)
			}
			if item.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", item.Description, tt.wantDesc)
			}
		})
	}
}
