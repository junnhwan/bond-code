package command

import (
	"context"
	"testing"
)

func TestRegistryRegisterGetAndList(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(Command{
		Name:        "status",
		Description: "show status",
		Run: func(ctx context.Context, env Env, args []string) (Result, error) {
			return Result{Output: "ok"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	cmd, ok := reg.Get("status")
	if !ok {
		t.Fatal("expected command")
	}
	if cmd.Name != "status" {
		t.Fatalf("unexpected command %s", cmd.Name)
	}
	names := reg.Names()
	if len(names) != 1 || names[0] != "status" {
		t.Fatalf("unexpected names %#v", names)
	}
}

func TestRegistryRejectsDuplicateCommands(t *testing.T) {
	reg := NewRegistry()
	cmd := Command{Name: "status", Run: func(context.Context, Env, []string) (Result, error) { return Result{}, nil }}
	if err := reg.Register(cmd); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(cmd); err == nil {
		t.Fatal("expected duplicate command error")
	}
}
