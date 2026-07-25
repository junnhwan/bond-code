package backend

import (
	"errors"
	"strings"
	"testing"
)

func TestUnsupportedErrorIsActionable(t *testing.T) {
	err := &UnsupportedError{
		Backend:  KindTmux,
		Platform: "windows",
		Reason:   "native tmux is not supported on Windows",
		Action:   "select the in-process backend",
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("errors.Is(%v, ErrUnsupported) = false", err)
	}
	for _, want := range []string{"tmux", "windows", "select the in-process backend"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func TestExternalLaunchSpecRejectsNonLocalParentEndpoint(t *testing.T) {
	spec := validExternalSpec()
	spec.ParentEndpoint = "tcp://192.0.2.10:7777"
	if err := validateExternalLaunchSpec(spec, "linux"); !errors.Is(err, ErrInvalidLaunchSpec) {
		t.Fatalf("validateExternalLaunchSpec error = %v, want ErrInvalidLaunchSpec", err)
	}
}

func TestExternalLaunchSpecCarriesTokenByFileOnly(t *testing.T) {
	spec := validExternalSpec()
	if err := validateExternalLaunchSpec(spec, "linux"); err != nil {
		t.Fatalf("validateExternalLaunchSpec: %v", err)
	}
	if strings.Contains(strings.ToLower(spec.TokenFile), "secret-value") {
		t.Fatal("test fixture accidentally embeds a raw token")
	}
}

func validExternalSpec() LaunchSpec {
	return LaunchSpec{
		TaskID:         "task-1",
		SessionID:      "session-1",
		TeamID:         "team-1",
		MemberID:       "member-1",
		Generation:     2,
		OwnershipID:    "owner-unguessable-1",
		ParentEndpoint: "unix:///tmp/bondcode-parent.sock",
		TokenFile:      "/tmp/bondcode-launch-token",
	}
}
