package backend

import (
	"os"
	"runtime"
	"strings"
)

type externalConfig struct {
	executor         CommandExecutor
	goos             string
	wsl              bool
	clientExecutable string
}

type ExternalOption func(*externalConfig)

// WithPlatform overrides platform detection. It is primarily useful for
// deterministic platform-gated tests.
func WithPlatform(goos string) ExternalOption {
	return func(config *externalConfig) { config.goos = strings.ToLower(strings.TrimSpace(goos)) }
}

// WithWSL explicitly marks the environment as WSL. Native tmux support is
// deferred there until endpoint/path mapping has a dedicated design.
func WithWSL(wsl bool) ExternalOption {
	return func(config *externalConfig) { config.wsl = wsl }
}

// WithClientExecutable selects the BondCode executable that provides the
// fixed teammate-client subcommand. It does not permit arbitrary subcommands.
func WithClientExecutable(path string) ExternalOption {
	return func(config *externalConfig) { config.clientExecutable = path }
}

func newExternalConfig(executor CommandExecutor, options []ExternalOption) externalConfig {
	if executor == nil {
		executor = osCommandExecutor{}
	}
	clientExecutable, _ := os.Executable()
	config := externalConfig{
		executor:         executor,
		goos:             runtime.GOOS,
		wsl:              detectWSL(),
		clientExecutable: clientExecutable,
	}
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	return config
}

func detectWSL() bool {
	return strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME")) != "" || strings.TrimSpace(os.Getenv("WSL_INTEROP")) != ""
}
