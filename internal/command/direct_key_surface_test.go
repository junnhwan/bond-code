package command

import (
	"reflect"
	"testing"
)

func TestDirectKeyDescriptorsDefineCanonicalPolicy(t *testing.T) {
	want := []DirectKeyDescriptor{
		{ID: "key.submit", DisplayShortcut: "Enter", Description: "submit prompt", ExecutionTarget: "tui-local.submit"},
		{ID: "key.newline", DisplayShortcut: "Ctrl+J / Alt+Enter / Shift+Enter", Description: "insert newline (Ctrl+J is the reliable Windows path)", ExecutionTarget: "tui-local.composer.newline"},
		{ID: "key.escape", DisplayShortcut: "Esc", Description: "cancel run or dismiss active overlay", ExecutionTarget: "tui-local.cancel"},
		{ID: "key.interrupt", DisplayShortcut: "Ctrl+C", Description: "interrupt; repeated press exits", ExecutionTarget: "tui-local.interrupt"},
		{ID: "key.exit-empty", DisplayShortcut: "Ctrl+D", Description: "exit when input is empty", ExecutionTarget: "tui-local.exit.empty"},
		{ID: "key.mode-cycle", DisplayShortcut: "Shift+Tab / Alt+M", Description: "cycle plan/normal mode (Alt+M is the Windows fallback)", ExecutionTarget: "tui-local.mode.cycle"},
		{ID: "key.details", DisplayShortcut: "Ctrl+O", Description: "toggle expanded tool details", ExecutionTarget: "tui-local.view.verbose"},
		{ID: "key.thinking", DisplayShortcut: "Ctrl+T", Description: "toggle historical thinking blocks", ExecutionTarget: "tui-local.view.thinking"},
		{ID: "key.history-search", DisplayShortcut: "Ctrl+R", Description: "search prompt history in reverse", ExecutionTarget: "tui-local.history.reverse"},
		{ID: "key.agent-switcher", DisplayShortcut: "Ctrl+Up", Description: "open Agent switcher", ExecutionTarget: "tui-local.agent.switcher"},
		{ID: "key.external-editor", DisplayShortcut: "Ctrl+G", Description: "open draft in external editor", ExecutionTarget: "tui-local.prompt.editor"},
		{ID: "key.stash", DisplayShortcut: "Ctrl+S", Description: "stash or restore draft", ExecutionTarget: "tui-local.prompt.stash"},
		{ID: "key.redraw", DisplayShortcut: "Ctrl+L", Description: "redraw screen without clearing session state", ExecutionTarget: "tui-local.screen.redraw"},
	}

	if got := DirectKeyDescriptors(); !reflect.DeepEqual(got, want) {
		t.Fatalf("direct key descriptors = %#v, want %#v", got, want)
	}
}

func TestDirectKeyDescriptorsAreConsistentAndLookupable(t *testing.T) {
	descriptors := DirectKeyDescriptors()
	ids := make(map[string]struct{}, len(descriptors))
	shortcuts := make(map[string]struct{}, len(descriptors))
	targets := make(map[ExecutionTargetID]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		if descriptor.ID == "" || descriptor.DisplayShortcut == "" || descriptor.Description == "" || descriptor.ExecutionTarget == "" {
			t.Errorf("descriptor has an empty required field: %#v", descriptor)
		}
		if _, exists := ids[descriptor.ID]; exists {
			t.Errorf("duplicate direct key ID %q", descriptor.ID)
		}
		ids[descriptor.ID] = struct{}{}
		if _, exists := shortcuts[descriptor.DisplayShortcut]; exists {
			t.Errorf("duplicate direct key shortcut %q", descriptor.DisplayShortcut)
		}
		shortcuts[descriptor.DisplayShortcut] = struct{}{}
		if _, exists := targets[descriptor.ExecutionTarget]; exists {
			t.Errorf("duplicate direct key execution target %q", descriptor.ExecutionTarget)
		}
		targets[descriptor.ExecutionTarget] = struct{}{}
		if class := ClassifyExecutionTarget(descriptor.ExecutionTarget); class != ExecutionTargetTUILocal {
			t.Errorf("direct key %q target class = %q, want %q", descriptor.ID, class, ExecutionTargetTUILocal)
		}

		got, ok := LookupDirectKeyDescriptor(descriptor.ID)
		if !ok {
			t.Errorf("LookupDirectKeyDescriptor(%q) did not find a descriptor", descriptor.ID)
		} else if !reflect.DeepEqual(got, descriptor) {
			t.Errorf("LookupDirectKeyDescriptor(%q) = %#v, want %#v", descriptor.ID, got, descriptor)
		}
	}
	if _, ok := LookupDirectKeyDescriptor("key.unknown"); ok {
		t.Fatal("unknown direct key ID unexpectedly resolved")
	}
}

func TestDirectKeyDescriptorsReturnDefensiveCopies(t *testing.T) {
	first := DirectKeyDescriptors()
	first[0].ID = "mutated"
	first[1].DisplayShortcut = "mutated"

	second := DirectKeyDescriptors()
	if second[0].ID != "key.submit" || second[1].DisplayShortcut != "Ctrl+J / Alt+Enter / Shift+Enter" {
		t.Fatalf("descriptor mutation leaked into canonical policy: %#v", second[:2])
	}

	lookup, ok := LookupDirectKeyDescriptor("key.submit")
	if !ok {
		t.Fatal("key.submit lookup failed")
	}
	lookup.Description = "mutated"
	lookupAgain, _ := LookupDirectKeyDescriptor("key.submit")
	if lookupAgain.Description != "submit prompt" {
		t.Fatalf("lookup mutation leaked into canonical policy: %#v", lookupAgain)
	}
}
