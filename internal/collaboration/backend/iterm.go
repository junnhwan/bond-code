package backend

import (
	"context"
	"fmt"
	"strings"
)

const (
	itermMissingMarker = "__bondcode_missing__"

	itermDetectScript = `return id of application "iTerm"`

	itermLaunchScript = `on run argv
	set launchCommand to item 1 of argv
	set ownerID to item 2 of argv
	tell application "iTerm"
		set newWindow to (create window with default profile command launchCommand)
		tell current session of current tab of newWindow
			set variable named "user.bondcode_owner" to ownerID
			return unique ID
		end tell
	end tell
end run`

	itermInspectScript = `on run argv
	set wantedID to item 1 of argv
	tell application "iTerm"
		repeat with aWindow in windows
			repeat with aTab in tabs of aWindow
				repeat with aSession in sessions of aTab
					if unique ID of aSession is wantedID then
						return variable named "user.bondcode_owner" of aSession
					end if
				end repeat
			end repeat
		end repeat
	end tell
	return "__bondcode_missing__"
end run`

	itermInputScript = `on run argv
	set wantedID to item 1 of argv
	set inputText to item 2 of argv
	tell application "iTerm"
		repeat with aWindow in windows
			repeat with aTab in tabs of aWindow
				repeat with aSession in sessions of aTab
					if unique ID of aSession is wantedID then
						tell aSession to write text inputText
						return
					end if
				end repeat
			end repeat
		end repeat
	end tell
end run`

	itermShowScript = `on run argv
	set wantedID to item 1 of argv
	tell application "iTerm"
		activate
		repeat with aWindow in windows
			repeat with aTab in tabs of aWindow
				repeat with aSession in sessions of aTab
					if unique ID of aSession is wantedID then
						select aWindow
						select aTab
						select aSession
						return
					end if
				end repeat
			end repeat
		end repeat
	end tell
end run`

	itermInterruptScript = `on run argv
	set wantedID to item 1 of argv
	tell application "iTerm"
		repeat with aWindow in windows
			repeat with aTab in tabs of aWindow
				repeat with aSession in sessions of aTab
					if unique ID of aSession is wantedID then
						tell aSession to write text (ASCII character 3) newline NO
						return
					end if
				end repeat
			end repeat
		end repeat
	end tell
end run`

	// The close script repeats the ownership check immediately before closing,
	// fencing a resource that changed owner after the preceding inspection.
	itermCloseScript = `on run argv
	set wantedID to item 1 of argv
	set expectedOwner to item 2 of argv
	tell application "iTerm"
		repeat with aWindow in windows
			repeat with aTab in tabs of aWindow
				repeat with aSession in sessions of aTab
					if unique ID of aSession is wantedID then
						if variable named "user.bondcode_owner" of aSession is not expectedOwner then error "ownership mismatch"
						close aSession
						return
					end if
				end repeat
			end repeat
		end repeat
	end tell
end run`
)

// ITerm is a native macOS iTerm adapter implemented through a detected,
// static AppleScript integration. Dynamic values are passed in argv and are
// never interpolated into AppleScript source.
type ITerm struct {
	config externalConfig
}

func NewITerm(executor CommandExecutor, options ...ExternalOption) *ITerm {
	return &ITerm{config: newExternalConfig(executor, options)}
}

func (b *ITerm) Kind() Kind { return KindITerm }

func (b *ITerm) Detect(ctx context.Context) (Detection, error) {
	if b == nil {
		return Detection{}, fmt.Errorf("iTerm backend is not configured")
	}
	if b.config.goos != "darwin" {
		return Detection{}, &UnsupportedError{
			Backend:  KindITerm,
			Platform: b.config.goos,
			Reason:   "native iTerm integration is supported only on macOS",
			Action:   "select the in-process backend or run BondCode on macOS with iTerm installed",
		}
	}
	executable, err := b.config.executor.LookPath("osascript")
	if err != nil {
		return Detection{}, &UnsupportedError{
			Backend:  KindITerm,
			Platform: b.config.goos,
			Reason:   "the macOS osascript integration was not found",
			Action:   "repair the macOS scripting tools, or explicitly select the in-process backend",
		}
	}
	result, err := b.config.executor.Run(ctx, executable, "-e", itermDetectScript)
	if err != nil || result.ExitCode != 0 || !strings.Contains(strings.TrimSpace(result.Stdout), "com.googlecode.iterm2") {
		detail := commandFailureDetail(result, err)
		return Detection{}, &UnsupportedError{
			Backend:  KindITerm,
			Platform: b.config.goos,
			Reason:   "supported iTerm AppleScript integration was not detected: " + detail,
			Action:   "install iTerm and allow Apple Events access, or explicitly select the in-process backend",
		}
	}
	return Detection{
		Kind:       KindITerm,
		Available:  true,
		Executable: executable,
		Capabilities: Capabilities{
			External:     true,
			SendInput:    true,
			Attach:       true,
			Show:         true,
			GracefulStop: true,
			ForceStop:    true,
		},
	}, nil
}

func (b *ITerm) Launch(ctx context.Context, spec LaunchSpec) (Handle, error) {
	detection, err := b.Detect(ctx)
	if err != nil {
		return Handle{}, err
	}
	if err := validateExternalLaunchSpec(spec, b.config.goos); err != nil {
		return Handle{}, err
	}
	if strings.TrimSpace(b.config.clientExecutable) == "" || containsControl(b.config.clientExecutable) {
		return Handle{}, fmt.Errorf("%w: valid BondCode teammate client executable is required", ErrInvalidLaunchSpec)
	}
	command := shellJoin(append([]string{b.config.clientExecutable}, restrictedClientArgs(spec)...)...)
	result, err := b.config.executor.Run(ctx, detection.Executable, "-e", itermLaunchScript, "--", command, spec.OwnershipID)
	if err := requireCommandSuccess(result, err); err != nil {
		return Handle{}, fmt.Errorf("launch iTerm teammate session: %w", err)
	}
	resourceID := strings.TrimSpace(result.Stdout)
	if resourceID == "" || containsControl(resourceID) {
		return Handle{}, fmt.Errorf("launch iTerm teammate session: integration returned no valid session ID")
	}
	return Handle{
		Backend:     KindITerm,
		Namespace:   "iTerm",
		ResourceID:  resourceID,
		TaskID:      spec.TaskID,
		Generation:  spec.Generation,
		OwnershipID: spec.OwnershipID,
	}, nil
}

func (b *ITerm) SendInput(ctx context.Context, handle Handle, input string) error {
	executable, exists, err := b.validateOwnedResource(ctx, handle)
	if err != nil {
		return err
	}
	if !exists {
		return ErrResourceNotFound
	}
	if containsControl(input) {
		return fmt.Errorf("input contains control characters")
	}
	result, err := b.config.executor.Run(ctx, executable, "-e", itermInputScript, "--", handle.ResourceID, input)
	return requireCommandSuccess(result, err)
}

func (b *ITerm) Status(ctx context.Context, handle Handle) (Status, error) {
	_, exists, err := b.validateOwnedResource(ctx, handle)
	if err != nil {
		return Status{}, err
	}
	if !exists {
		return Status{State: StateStopped, Healthy: true, Generation: handle.Generation}, nil
	}
	return Status{State: StateRunning, Healthy: true, Generation: handle.Generation}, nil
}

func (b *ITerm) Attach(ctx context.Context, handle Handle) error { return b.Show(ctx, handle) }

func (b *ITerm) Show(ctx context.Context, handle Handle) error {
	executable, exists, err := b.validateOwnedResource(ctx, handle)
	if err != nil {
		return err
	}
	if !exists {
		return ErrResourceNotFound
	}
	result, err := b.config.executor.Run(ctx, executable, "-e", itermShowScript, "--", handle.ResourceID)
	return requireCommandSuccess(result, err)
}

func (b *ITerm) Hide(context.Context, Handle) error {
	return &UnsupportedOperationError{Backend: KindITerm, Operation: "hide", Action: "hiding iTerm would affect windows not owned by this backend"}
}

func (b *ITerm) Stop(ctx context.Context, handle Handle, mode StopMode) error {
	if mode == StopForce {
		return b.Cleanup(ctx, handle)
	}
	if mode != StopGraceful {
		return fmt.Errorf("unknown stop mode %q", mode)
	}
	executable, exists, err := b.validateOwnedResource(ctx, handle)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	result, err := b.config.executor.Run(ctx, executable, "-e", itermInterruptScript, "--", handle.ResourceID)
	return requireCommandSuccess(result, err)
}

func (b *ITerm) Cleanup(ctx context.Context, handle Handle) error {
	executable, exists, err := b.validateOwnedResource(ctx, handle)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	result, err := b.config.executor.Run(ctx, executable, "-e", itermCloseScript, "--", handle.ResourceID, handle.OwnershipID)
	if err := requireCommandSuccess(result, err); err != nil {
		return fmt.Errorf("clean up owned iTerm session: %w", err)
	}
	return nil
}

func (b *ITerm) validateOwnedResource(ctx context.Context, handle Handle) (string, bool, error) {
	if b == nil {
		return "", false, ErrResourceNotFound
	}
	if handle.Backend != KindITerm || strings.TrimSpace(handle.ResourceID) == "" || strings.TrimSpace(handle.OwnershipID) == "" || handle.Generation == 0 {
		return "", false, fmt.Errorf("%w: invalid iTerm handle", ErrOwnershipMismatch)
	}
	detection, err := b.Detect(ctx)
	if err != nil {
		return "", false, err
	}
	result, runErr := b.config.executor.Run(ctx, detection.Executable, "-e", itermInspectScript, "--", handle.ResourceID)
	if runErr != nil {
		return "", false, fmt.Errorf("query iTerm ownership: %w", runErr)
	}
	if result.ExitCode != 0 {
		return "", false, fmt.Errorf("query iTerm ownership: %s", commandFailureDetail(result, nil))
	}
	owner := strings.TrimSpace(result.Stdout)
	if owner == itermMissingMarker {
		return detection.Executable, false, nil
	}
	if owner != handle.OwnershipID {
		return "", false, fmt.Errorf("%w: refusing to operate on iTerm session %q", ErrOwnershipMismatch, handle.ResourceID)
	}
	return detection.Executable, true, nil
}

func shellJoin(args ...string) string {
	quoted := make([]string, len(args))
	for index, arg := range args {
		quoted[index] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
