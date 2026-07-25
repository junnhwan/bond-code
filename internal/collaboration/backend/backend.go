// Package backend defines execution backends for collaboration members.
//
// External backends launch only BondCode's restricted teammate client. The
// parent runtime remains the sole owner of model execution, tools, policy, and
// confirmation. Launch tokens are represented only by protected file paths;
// raw token values are intentionally absent from this API.
package backend

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Kind string

type State string

type StopMode string

const (
	KindInProcess Kind = "in_process"
	KindTmux      Kind = "tmux"
	KindITerm     Kind = "iterm"

	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
	StateFailed   State = "failed"
	StateUnknown  State = "unknown"

	StopGraceful StopMode = "graceful"
	StopForce    StopMode = "force"
)

var (
	ErrUnsupported       = errors.New("execution backend unsupported")
	ErrInvalidLaunchSpec = errors.New("invalid backend launch specification")
	ErrOwnershipMismatch = errors.New("backend resource ownership mismatch")
	ErrResourceNotFound  = errors.New("backend resource not found")
	ErrAlreadyLaunched   = errors.New("backend task generation already launched")
)

// Capabilities describes optional backend operations. Callers must not infer
// support from the host platform or backend name.
type Capabilities struct {
	External     bool
	SendInput    bool
	Attach       bool
	Show         bool
	Hide         bool
	GracefulStop bool
	ForceStop    bool
}

// Detection is returned only when a backend is available. Unavailable
// backends return an actionable UnsupportedError instead of silently falling
// back to another backend.
type Detection struct {
	Kind         Kind
	Available    bool
	Executable   string
	Capabilities Capabilities
}

// LaunchSpec is copied into a backend at launch. External backends forward
// only identity, fencing, endpoint, and token-file fields to the restricted
// teammate client; Prompt and other work fields remain parent-owned.
type LaunchSpec struct {
	TaskID      string
	SessionID   string
	TeamID      string
	MemberID    string
	Generation  uint64
	OwnershipID string

	ParentEndpoint string
	TokenFile      string

	Description string
	Prompt      string
	Profile     string
}

// Handle is an ownership-bound reference to one launched backend resource.
// Namespace is backend-defined (for example, a tmux server name).
type Handle struct {
	Backend     Kind
	ResourceID  string
	Namespace   string
	TaskID      string
	Generation  uint64
	OwnershipID string
}

type Result struct {
	Summary     string
	ResultPath  string
	ErrorText   string
	LegacyAlias string
	Err         error
}

type Status struct {
	State      State
	Healthy    bool
	Generation uint64
	Detail     string
	Result     *Result
}

// Backend is intentionally lifecycle-only. It cannot execute model tools.
// External workers must request all model/tool/confirmation work from the
// parent endpoint after authenticating with the one-time token file.
type Backend interface {
	Kind() Kind
	Detect(context.Context) (Detection, error)
	Launch(context.Context, LaunchSpec) (Handle, error)
	SendInput(context.Context, Handle, string) error
	Status(context.Context, Handle) (Status, error)
	Attach(context.Context, Handle) error
	Show(context.Context, Handle) error
	Hide(context.Context, Handle) error
	Stop(context.Context, Handle, StopMode) error
	Cleanup(context.Context, Handle) error
}

type UnsupportedError struct {
	Backend  Kind
	Platform string
	Reason   string
	Action   string
}

func (e *UnsupportedError) Error() string {
	if e == nil {
		return ErrUnsupported.Error()
	}
	parts := []string{fmt.Sprintf("%s backend is unavailable on %s", e.Backend, e.Platform)}
	if strings.TrimSpace(e.Reason) != "" {
		parts = append(parts, strings.TrimSpace(e.Reason))
	}
	if strings.TrimSpace(e.Action) != "" {
		parts = append(parts, "action: "+strings.TrimSpace(e.Action))
	}
	return strings.Join(parts, ": ")
}

func (e *UnsupportedError) Unwrap() error { return ErrUnsupported }

type UnsupportedOperationError struct {
	Backend   Kind
	Operation string
	Action    string
}

func (e *UnsupportedOperationError) Error() string {
	message := fmt.Sprintf("%s backend does not support %s", e.Backend, e.Operation)
	if strings.TrimSpace(e.Action) != "" {
		message += "; action: " + strings.TrimSpace(e.Action)
	}
	return message
}

func (e *UnsupportedOperationError) Unwrap() error { return ErrUnsupported }

func validateCommonLaunchSpec(spec LaunchSpec) error {
	required := []struct {
		name  string
		value string
	}{
		{"task ID", spec.TaskID},
		{"session ID", spec.SessionID},
		{"ownership ID", spec.OwnershipID},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidLaunchSpec, field.name)
		}
		if containsControl(field.value) {
			return fmt.Errorf("%w: %s contains control characters", ErrInvalidLaunchSpec, field.name)
		}
	}
	if spec.Generation == 0 {
		return fmt.Errorf("%w: generation must be greater than zero", ErrInvalidLaunchSpec)
	}
	return nil
}

func validateExternalLaunchSpec(spec LaunchSpec, goos string) error {
	if err := validateCommonLaunchSpec(spec); err != nil {
		return err
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"team ID", spec.TeamID},
		{"member ID", spec.MemberID},
		{"parent endpoint", spec.ParentEndpoint},
		{"launch token file", spec.TokenFile},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: %s is required for an external backend", ErrInvalidLaunchSpec, field.name)
		}
		if containsControl(field.value) {
			return fmt.Errorf("%w: %s contains control characters", ErrInvalidLaunchSpec, field.name)
		}
	}
	if !isLocalEndpoint(spec.ParentEndpoint) {
		return fmt.Errorf("%w: parent endpoint must be local-only (unix socket or loopback TCP)", ErrInvalidLaunchSpec)
	}
	if !isAbsolutePath(goos, spec.TokenFile) {
		return fmt.Errorf("%w: launch token file must be an absolute path", ErrInvalidLaunchSpec)
	}
	return nil
}

func isLocalEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "unix":
		return u.Path != "" && strings.HasPrefix(u.Path, "/")
	case "tcp":
		host := u.Hostname()
		if host == "" {
			return false
		}
		if strings.EqualFold(host, "localhost") {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	case "npipe":
		return u.Host != "" || u.Path != ""
	default:
		return false
	}
}

func isAbsolutePath(goos, value string) bool {
	if goos == "windows" {
		if strings.HasPrefix(value, `\\`) {
			return true
		}
		return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/')
	}
	return strings.HasPrefix(value, "/")
}

func containsControl(value string) bool {
	for _, r := range value {
		if r < ' ' || r == 0x7f {
			return true
		}
	}
	return false
}

func generationString(generation uint64) string {
	return strconv.FormatUint(generation, 10)
}
