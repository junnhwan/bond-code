package tui

import "testing"

func TestCalculateLayoutFillsWidth(t *testing.T) {
	cases := []struct {
		name   string
		width  int
		height int
	}{
		{name: "wide", width: 120, height: 40},
		{name: "narrow", width: 40, height: 20},
		{name: "tiny", width: 20, height: 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			layout := CalculateLayout(tc.width, tc.height, 3)
			if layout.TimelineW != tc.width {
				t.Fatalf("TimelineW = %d, want %d", layout.TimelineW, tc.width)
			}
			if layout.TimelineH < 1 {
				t.Fatalf("TimelineH must be positive, got %d", layout.TimelineH)
			}
			if layout.Width != tc.width || layout.Height != tc.height {
				t.Fatalf("layout size = %dx%d, want %dx%d", layout.Width, layout.Height, tc.width, tc.height)
			}
		})
	}
}
