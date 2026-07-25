package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrUnknownBackend   = errors.New("unknown execution backend")
	ErrDuplicateBackend = errors.New("duplicate execution backend")
)

// Registry owns the selectable collaboration execution backends. An empty
// selection means in-process; explicit unavailable selections fail rather than
// silently changing the requested execution environment.
type Registry struct{ backends map[Kind]Backend }

func NewRegistry(backends ...Backend) (*Registry, error) {
	r := &Registry{backends: make(map[Kind]Backend, len(backends))}
	for _, candidate := range backends {
		if candidate == nil || strings.TrimSpace(string(candidate.Kind())) == "" {
			return nil, fmt.Errorf("%w: backend kind is required", ErrUnknownBackend)
		}
		if _, exists := r.backends[candidate.Kind()]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateBackend, candidate.Kind())
		}
		r.backends[candidate.Kind()] = candidate
	}
	if _, exists := r.backends[KindInProcess]; !exists {
		return nil, fmt.Errorf("%w: %s must be registered", ErrUnknownBackend, KindInProcess)
	}
	return r, nil
}

func (r *Registry) Resolve(ctx context.Context, selection string) (Backend, Detection, error) {
	kind := Kind(strings.ToLower(strings.TrimSpace(selection)))
	if kind == "" {
		kind = KindInProcess
	}
	if r == nil {
		return nil, Detection{}, fmt.Errorf("%w: %s", ErrUnknownBackend, kind)
	}
	selected, exists := r.backends[kind]
	if !exists {
		return nil, Detection{}, fmt.Errorf("%w: %s", ErrUnknownBackend, kind)
	}
	detection, err := selected.Detect(ctx)
	if err != nil {
		return nil, Detection{}, err
	}
	if !detection.Available {
		return nil, Detection{}, &UnsupportedError{Backend: kind, Platform: "current host", Reason: "capability detection reported unavailable", Action: "select the in-process backend"}
	}
	return selected, detection, nil
}

func (r *Registry) Kinds() []Kind {
	ordered := []Kind{KindInProcess, KindTmux, KindITerm}
	result := make([]Kind, 0, len(r.backends))
	for _, kind := range ordered {
		if _, ok := r.backends[kind]; ok {
			result = append(result, kind)
		}
	}
	return result
}
