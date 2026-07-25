package backend

import (
	"context"
	"errors"
	"testing"
)

type registryBackend struct {
	kind      Kind
	detection Detection
	err       error
}

func (b registryBackend) Kind() Kind                                         { return b.kind }
func (b registryBackend) Detect(context.Context) (Detection, error)          { return b.detection, b.err }
func (b registryBackend) Launch(context.Context, LaunchSpec) (Handle, error) { return Handle{}, nil }
func (b registryBackend) SendInput(context.Context, Handle, string) error    { return nil }
func (b registryBackend) Status(context.Context, Handle) (Status, error)     { return Status{}, nil }
func (b registryBackend) Attach(context.Context, Handle) error               { return nil }
func (b registryBackend) Show(context.Context, Handle) error                 { return nil }
func (b registryBackend) Hide(context.Context, Handle) error                 { return nil }
func (b registryBackend) Stop(context.Context, Handle, StopMode) error       { return nil }
func (b registryBackend) Cleanup(context.Context, Handle) error              { return nil }

func TestRegistryResolveDefaultsToInProcessAndDoesNotFallback(t *testing.T) {
	r, err := NewRegistry(registryBackend{kind: KindInProcess, detection: Detection{Kind: KindInProcess, Available: true}}, registryBackend{kind: KindTmux, err: &UnsupportedError{Backend: KindTmux, Platform: "test", Reason: "missing"}})
	if err != nil {
		t.Fatal(err)
	}
	got, detection, err := r.Resolve(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind() != KindInProcess || detection.Kind != KindInProcess {
		t.Fatalf("default = %v/%v", got.Kind(), detection.Kind)
	}
	if _, _, err := r.Resolve(context.Background(), "tmux"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("tmux error = %v", err)
	}
}

func TestRegistryRejectsUnknownAndDuplicateBackends(t *testing.T) {
	if _, err := NewRegistry(registryBackend{kind: KindInProcess}, registryBackend{kind: KindInProcess}); !errors.Is(err, ErrDuplicateBackend) {
		t.Fatalf("duplicate error = %v", err)
	}
	r, err := NewRegistry(registryBackend{kind: KindInProcess, detection: Detection{Kind: KindInProcess, Available: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Resolve(context.Background(), "screen"); !errors.Is(err, ErrUnknownBackend) {
		t.Fatalf("unknown error = %v", err)
	}
}
