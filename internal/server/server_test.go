package server

import (
	"testing"

	"github.com/lunch/monixtend/internal/config"
)

func TestClampSize(t *testing.T) {
	s := &Server{cfg: config.Config{MaxWidth: 1920, MaxHeight: 1200}}
	cases := []struct {
		inW, inH     int
		wantW, wantH int
	}{
		{1280, 720, 1280, 720},
		{4000, 3000, 1920, 1200},
		{0, 0, 1280, 720},
		{100, 100, 320, 240},
		{1921, 1201, 1920, 1200},
		{1279, 719, 1278, 718}, // rounded down to even
	}
	for _, tc := range cases {
		gotW, gotH := s.clampSize(tc.inW, tc.inH)
		if gotW != tc.wantW || gotH != tc.wantH {
			t.Errorf("clampSize(%d,%d) = %d,%d want %d,%d",
				tc.inW, tc.inH, gotW, gotH, tc.wantW, tc.wantH)
		}
	}
}
