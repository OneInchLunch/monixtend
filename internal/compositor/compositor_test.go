package compositor

import "testing"

func TestMonitorLogicalSize(t *testing.T) {
	cases := []struct {
		name         string
		m            Monitor
		wantW, wantH int
	}{
		{"fractional scale", Monitor{Width: 1920, Height: 1080, Scale: 1.5}, 1280, 720},
		{"integer scale", Monitor{Width: 2560, Height: 1440, Scale: 2}, 1280, 720},
		{"unit scale", Monitor{Width: 1280, Height: 720, Scale: 1}, 1280, 720},
		{"zero scale falls back", Monitor{Width: 800, Height: 600, Scale: 0}, 800, 600},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.m.LogicalWidth(); got != tc.wantW {
				t.Errorf("LogicalWidth() = %d, want %d", got, tc.wantW)
			}
			if got := tc.m.LogicalHeight(); got != tc.wantH {
				t.Errorf("LogicalHeight() = %d, want %d", got, tc.wantH)
			}
		})
	}
}
