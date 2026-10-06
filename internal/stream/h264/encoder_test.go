//go:build cgo

package h264

import (
	"testing"
	"time"

	"github.com/lunch/monixtend/internal/capture"
)

func makeFrame(w, h int, v byte, pts time.Time) *capture.Frame {
	pix := make([]byte, w*h*4)
	for i := 0; i < len(pix); i += 4 {
		pix[i] = v
		pix[i+1] = v
		pix[i+2] = v
		pix[i+3] = 0xff
	}
	return &capture.Frame{Pix: pix, Stride: w * 4, Width: w, Height: h, PTS: pts}
}

// nalTypes extracts the nal_unit_type of every Annex-B NAL in a buffer.
func nalTypes(t *testing.T, data []byte) []byte {
	t.Helper()
	var types []byte
	i := 0
	for i+3 < len(data) {
		var start int
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			start = i + 4
			i += 4
		} else if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			start = i + 3
			i += 3
		} else {
			i++
			continue
		}
		if start < len(data) {
			types = append(types, data[start]&0x1f)
		}
	}
	return types
}

func TestEncoderKeyframeAndDelta(t *testing.T) {
	enc, err := New(Options{Width: 64, Height: 48, FPS: 30, Bitrate: 500000, KeyframeInterval: 30})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer enc.Close()

	now := time.Now()
	au, isKey, err := enc.Encode(makeFrame(64, 48, 0x80, now), true)
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	if len(au) == 0 {
		t.Fatal("expected a non-empty access unit")
	}
	if !isKey {
		t.Error("first forced frame should be a keyframe")
	}
	types := nalTypes(t, au)
	want := map[byte]bool{7: false, 8: false, 5: false} // SPS, PPS, IDR
	for _, nt := range types {
		if _, ok := want[nt]; ok {
			want[nt] = true
		}
	}
	for nt, seen := range want {
		if !seen {
			t.Errorf("expected NAL type %d in first access unit; got types %v", nt, types)
		}
	}

	// A changed frame should produce a delta access unit.
	au2, _, err := enc.Encode(makeFrame(64, 48, 0x40, now.Add(33*time.Millisecond)), false)
	if err != nil {
		t.Fatalf("Encode delta: %v", err)
	}
	if len(au2) == 0 {
		t.Error("expected a non-empty delta access unit")
	}
}

func TestEncoderRejectsOddSize(t *testing.T) {
	if _, err := New(Options{Width: 63, Height: 48, FPS: 30}); err == nil {
		t.Error("expected error for odd width")
	}
}
