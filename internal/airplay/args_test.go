package airplay

import (
	"slices"
	"testing"
)

func TestParseResolution(t *testing.T) {
	cases := []struct {
		in      string
		w, h, r int
		wantErr bool
	}{
		{"1920x1080", 1920, 1080, 0, false},
		{"1920x1080@60", 1920, 1080, 60, false},
		{"1280x720@144", 1280, 720, 144, false},
		{"", 0, 0, 0, true},
		{"1920", 0, 0, 0, true},
		{"1920x", 0, 0, 0, true},
		{"x1080", 0, 0, 0, true},
		{"1920x1080@0", 0, 0, 0, true},
		{"a@b", 0, 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			w, h, r, err := ParseResolution(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseResolution(%q): %v", tc.in, err)
			}
			if w != tc.w || h != tc.h || r != tc.r {
				t.Errorf("got %dx%d@%d want %dx%d@%d", w, h, r, tc.w, tc.h, tc.r)
			}
		})
	}
}

func TestBuildArgs(t *testing.T) {
	args := BuildArgs(Options{
		Name:        "martop",
		Resolution:  "1920x1080@60",
		Pin:         "1234",
		SmoothingMS: 100,
		Leaky:       true,
		ExtraArgs:   []string{"-vd", "openh264dec"},
	})
	want := []string{"-n", "martop", "-s", "1920x1080@60", "-as", "0", "-vsync", "no", "-latency", "100", "-leaky", "on", "-pin", "1234", "-vs", "waylandsink", "-vd", "openh264dec"}
	if !slices.Equal(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestBuildArgsSmoothing(t *testing.T) {
	args := BuildArgs(Options{Name: "x", Resolution: "1280x720", SmoothingMS: 150, Leaky: false})
	if !hasPair(args, "-latency", "150") {
		t.Errorf("expected -latency 150; got %v", args)
	}
	if !hasPair(args, "-leaky", "off") {
		t.Errorf("expected -leaky off; got %v", args)
	}
	args = BuildArgs(Options{Name: "x", Resolution: "1280x720"}) // SmoothingMS 0
	if hasPair(args, "-latency", "100") || hasPair(args, "-leaky", "on") {
		t.Errorf("smoothing 0 must omit -latency/-leaky; got %v", args)
	}
}

func TestBuildArgsVideoSync(t *testing.T) {
	args := BuildArgs(Options{Name: "x", Resolution: "1280x720", VideoSync: true})
	for _, a := range args {
		if a == "-vsync" {
			t.Fatal("VideoSync=true should omit -vsync no")
		}
	}
	args = BuildArgs(Options{Name: "x", Resolution: "1280x720"})
	if !hasPair(args, "-vsync", "no") {
		t.Errorf("expected -vsync no by default; got %v", args)
	}
}

func hasPair(args []string, k, v string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == k && args[i+1] == v {
			return true
		}
	}
	return false
}

func TestBuildArgsAudioOn(t *testing.T) {
	args := BuildArgs(Options{Name: "x", Resolution: "1280x720", Audio: true})
	for _, a := range args {
		if a == "-as" {
			t.Error("audio on should omit -as 0")
		}
	}
	if !slices.Contains(args, "-s") {
		t.Error("missing -s")
	}
}

func TestBuildArgsHardware(t *testing.T) {
	args := BuildArgs(Options{
		Name:           "x",
		Resolution:     "1280x720",
		VideoDecoder:   "vah264dec",
		VideoConverter: "v4l2convert",
		VideoSink:      "glimagesink",
	})
	if !hasPair(args, "-vd", "vah264dec") {
		t.Errorf("expected -vd vah264dec; got %v", args)
	}
	if !hasPair(args, "-vc", "v4l2convert") {
		t.Errorf("expected -vc v4l2convert; got %v", args)
	}
	if !hasPair(args, "-vs", "glimagesink") {
		t.Errorf("expected -vs glimagesink; got %v", args)
	}
}

func TestBuildArgsDefaultSink(t *testing.T) {
	args := BuildArgs(Options{Name: "x", Resolution: "1280x720"})
	if !hasPair(args, "-vs", "waylandsink") {
		t.Errorf("expected default -vs waylandsink; got %v", args)
	}
	for _, a := range args {
		if a == "-vd" || a == "-vc" {
			t.Errorf("did not expect %s without a decoder; got %v", a, args)
		}
	}
}
