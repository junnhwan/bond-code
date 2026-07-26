package builtin

import (
	"context"
	"reflect"
	"testing"

	"github.com/junnhwan/bond-code/internal/command"
)

func TestHelpSurfaceUsesDiscoverableCommandDescriptors(t *testing.T) {
	result, err := HelpCommand().Run(context.Background(), command.Env{}, nil)
	if err != nil {
		t.Fatalf("help command: %v", err)
	}
	if result.Panel == nil {
		t.Fatal("expected structured help panel")
	}
	if len(result.Panel.Sections) != 2 {
		t.Fatalf("help sections = %d, want only COMMANDS and KEYS", len(result.Panel.Sections))
	}
	if result.Panel.Sections[0].Label != "COMMANDS" || result.Panel.Sections[1].Label != "KEYS" {
		t.Fatalf("help section labels = %#v, want COMMANDS then KEYS", result.Panel.Sections)
	}

	descriptors := command.DiscoverableSurfaceDescriptors()
	wantRows := make([]command.PanelRow, len(descriptors))
	for i, descriptor := range descriptors {
		wantRows[i] = command.PanelRow{Key: descriptor.Shortcut, Value: descriptor.Description}
	}
	if got := result.Panel.Sections[0].Rows; !reflect.DeepEqual(got, wantRows) {
		t.Fatalf("help command rows = %#v, want descriptor rows %#v", got, wantRows)
	}

	keyDescriptors := command.DirectKeyDescriptors()
	wantKeyRows := make([]command.PanelRow, len(keyDescriptors))
	for i, descriptor := range keyDescriptors {
		wantKeyRows[i] = command.PanelRow{Key: descriptor.DisplayShortcut, Value: descriptor.Description}
	}
	if got := result.Panel.Sections[1].Rows; !reflect.DeepEqual(got, wantKeyRows) {
		t.Fatalf("help key rows = %#v, want direct-key descriptor rows %#v", got, wantKeyRows)
	}

	hidden := map[string]bool{
		"/sessions": true, "/session": true, "/new": true, "/cost": true, "/theme": true,
		"/quit": true, "/q": true, "/context summary": true,
		"/model": true, "/permissions": true, "/diff": true, "/history": true,
	}
	for _, row := range result.Panel.Sections[0].Rows {
		if hidden[row.Key] {
			t.Errorf("hidden compatibility entry %q appeared in help command rows", row.Key)
		}
	}
}
