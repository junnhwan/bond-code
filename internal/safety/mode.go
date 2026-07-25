package safety

import (
	"fmt"
	"strings"
	"sync"

	"github.com/junnhwan/bond-code/internal/tool"
)

type PermissionMode string

const (
	ModeDefault     PermissionMode = "default"
	ModeAcceptEdits PermissionMode = "accept-edits"
	ModePlan        PermissionMode = "plan"
	ModeBypass      PermissionMode = "bypass"
)

func ParsePermissionMode(raw string) (PermissionMode, error) {
	mode := PermissionMode(strings.ToLower(strings.TrimSpace(raw)))
	if mode == "" {
		mode = ModeDefault
	}
	switch mode {
	case ModeDefault, ModeAcceptEdits, ModePlan, ModeBypass:
		return mode, nil
	default:
		return "", fmt.Errorf("unknown permission mode %q (want default, accept-edits, plan, or bypass)", raw)
	}
}

type Capability string

const (
	CapabilityRead              Capability = "read"
	CapabilityMutate            Capability = "mutate"
	CapabilityExecute           Capability = "execute"
	CapabilityAutoApproveEdits  Capability = "auto_approve_edits"
	CapabilityAutoApproveMedium Capability = "auto_approve_medium"
)

type CapabilitySet map[Capability]struct{}

func (s CapabilitySet) Has(c Capability) bool { _, ok := s[c]; return ok }

func CapabilitiesForMode(mode PermissionMode, bypassEnabled bool) CapabilitySet {
	out := CapabilitySet{CapabilityRead: {}}
	if mode != ModePlan {
		out[CapabilityMutate] = struct{}{}
		out[CapabilityExecute] = struct{}{}
	}
	if mode == ModeAcceptEdits {
		out[CapabilityAutoApproveEdits] = struct{}{}
	}
	if mode == ModeBypass && bypassEnabled {
		out[CapabilityAutoApproveEdits] = struct{}{}
		out[CapabilityAutoApproveMedium] = struct{}{}
	}
	return out
}

func EffectiveCapabilities(sets ...CapabilitySet) CapabilitySet {
	if len(sets) == 0 {
		return CapabilitySet{}
	}
	out := CapabilitySet{}
	for c := range sets[0] {
		keep := true
		for _, set := range sets[1:] {
			if !set.Has(c) {
				keep = false
				break
			}
		}
		if keep {
			out[c] = struct{}{}
		}
	}
	return out
}

func isPlanMutation(toolName string) bool {
	switch toolName {
	case tool.WriteFile, tool.EditFile, tool.RunCommand:
		return true
	default:
		return false
	}
}

// PermissionModeSource provides a concurrency-safe runtime mode shared by
// parent and child policy copies. Trusted bypass enablement is immutable.
type PermissionModeSource struct {
	mu            sync.RWMutex
	mode          PermissionMode
	bypassEnabled bool
}

func NewPermissionModeSource(mode PermissionMode, bypassEnabled bool) (*PermissionModeSource, error) {
	parsed, err := ParsePermissionMode(string(mode))
	if err != nil {
		return nil, err
	}
	if parsed == ModeBypass && !bypassEnabled {
		return nil, fmt.Errorf("bypass mode requires trusted enablement")
	}
	return &PermissionModeSource{mode: parsed, bypassEnabled: bypassEnabled}, nil
}
func (s *PermissionModeSource) Mode() PermissionMode {
	if s == nil {
		return ModeDefault
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode
}
func (s *PermissionModeSource) BypassEnabled() bool { return s != nil && s.bypassEnabled }
func (s *PermissionModeSource) Set(mode PermissionMode) error {
	parsed, err := ParsePermissionMode(string(mode))
	if err != nil {
		return err
	}
	if parsed == ModeBypass && !s.bypassEnabled {
		return fmt.Errorf("bypass mode requires trusted enablement")
	}
	s.mu.Lock()
	s.mode = parsed
	s.mu.Unlock()
	return nil
}
