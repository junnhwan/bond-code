package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/junnhwan/bond-code/internal/safety"
)

func TestLoadPermissionMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bondcode.yaml")
	if err := os.WriteFile(path, []byte("safety:\n  permission_mode: accept-edits\n  enable_bypass: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Safety.PermissionMode != safety.ModeAcceptEdits {
		t.Fatalf("mode=%q", cfg.Safety.PermissionMode)
	}
}

func TestLoadRejectsUnknownPermissionMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bondcode.yaml")
	if err := os.WriteFile(path, []byte("safety:\n  permission_mode: unrestricted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid permission mode error")
	}
}

func TestLoadBypassRequiresEnableBypass(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bondcode.yaml")
	if err := os.WriteFile(path, []byte("safety:\n  permission_mode: bypass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected bypass acknowledgement error")
	}
}
