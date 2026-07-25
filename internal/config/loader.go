package config

import (
	"fmt"
	"os"

	"github.com/junnhwan/bond-code/internal/safety"
	"gopkg.in/yaml.v3"
)

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	mode, err := safety.ParsePermissionMode(string(cfg.Safety.PermissionMode))
	if err != nil {
		return nil, fmt.Errorf("safety.permission_mode: %w", err)
	}
	cfg.Safety.PermissionMode = mode
	if mode == safety.ModeBypass && !cfg.Safety.EnableBypass {
		return nil, fmt.Errorf("safety.enable_bypass must be true when safety.permission_mode is bypass")
	}
	return &cfg, nil
}
