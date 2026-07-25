package builtin

import (
	"context"
	"testing"

	"github.com/junnhwan/bond-code/internal/command"
)

func TestPermissionsCommandShowsAndSwitchesMode(t *testing.T) {
	cmd := PermissionsCommand()
	env := command.Env{PermissionMode: "default"}
	result, err := cmd.Run(context.Background(), env, nil)
	if err != nil || result.Output != "permission mode: default" {
		t.Fatalf("show=%#v err=%v", result, err)
	}
	var changed string
	env.SetPermissionMode = func(mode string) error { changed = mode; return nil }
	result, err = cmd.Run(context.Background(), env, []string{"plan"})
	if err != nil || changed != "plan" || result.PermissionModeChanged == nil || *result.PermissionModeChanged != "plan" {
		t.Fatalf("switch=%#v err=%v changed=%q", result, err, changed)
	}
}

func TestPermissionsCommandRequiresRuntimeSwitcher(t *testing.T) {
	_, err := PermissionsCommand().Run(context.Background(), command.Env{PermissionMode: "default"}, []string{"plan"})
	if err == nil {
		t.Fatal("expected unavailable error")
	}
}
