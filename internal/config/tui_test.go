package config

import "testing"

func TestTUIConfigMouseCaptureDefaultsOnForWheelScroll(t *testing.T) {
	// Wheel scroll requires mouse tracking on; default must enable it.
	if !(TUIConfig{}).Mouse() {
		t.Fatal("expected mouse_capture to default on so wheel scroll reaches the TUI")
	}
}

func TestTUIConfigMouseCaptureCanBeDisabled(t *testing.T) {
	disabled := false
	if (TUIConfig{MouseCapture: &disabled}).Mouse() {
		t.Fatal("expected explicit mouse_capture false to disable mouse tracking")
	}
}

func TestTUIConfigMouseCaptureCanBeEnabled(t *testing.T) {
	enabled := true
	if !(TUIConfig{MouseCapture: &enabled}).Mouse() {
		t.Fatal("expected explicit mouse_capture true to enable mouse tracking")
	}
}
