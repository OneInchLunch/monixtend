package main

import "testing"

func TestParseResolution(t *testing.T) {
	cases := []struct {
		in      string
		w, h, r int
		wantErr bool
	}{
		{"1920x1080@60", 1920, 1080, 60, false},
		{"1280x720", 1280, 720, 0, false},
		{"800x600@144", 800, 600, 144, false},
		{"garbage", 0, 0, 0, true},
		{"1920@60", 0, 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			w, h, r, err := parseResolution(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseResolution(%q): %v", tc.in, err)
			}
			if w != tc.w || h != tc.h || r != tc.r {
				t.Errorf("got %d,%d,@%d want %d,%d,@%d", w, h, r, tc.w, tc.h, tc.r)
			}
		})
	}
}

func TestParsePosition(t *testing.T) {
	x, y, err := parsePosition("1920x0")
	if err != nil || x != 1920 || y != 0 {
		t.Fatalf("got %d,%d,%v", x, y, err)
	}
	if _, _, err := parsePosition("nope"); err == nil {
		t.Error("expected error")
	}
}

func TestParseSpec(t *testing.T) {
	spec, err := parseSpec([]string{"1280x720@60", "100x200", "2"})
	if err != nil {
		t.Fatalf("parseSpec: %v", err)
	}
	if spec.Width != 1280 || spec.Height != 720 || spec.Refresh != 60 {
		t.Errorf("resolution = %dx%d@%d", spec.Width, spec.Height, spec.Refresh)
	}
	if spec.X != 100 || spec.Y != 200 {
		t.Errorf("position = %d,%d", spec.X, spec.Y)
	}
	if spec.Scale != 2 {
		t.Errorf("scale = %v", spec.Scale)
	}
}
