package stream

import (
	"bytes"
	"image/jpeg"
	"testing"

	"github.com/lunch/monixtend/internal/capture"
)

func TestMJPEGEncode(t *testing.T) {
	const w, h = 8, 4
	pix := make([]byte, w*h*4)
	for i := 0; i < len(pix); i += 4 {
		pix[i] = 0x00   // B
		pix[i+1] = 0x00 // G
		pix[i+2] = 0xff // R
		pix[i+3] = 0x00 // X
	}
	f := &capture.Frame{Pix: pix, Stride: w * 4, Width: w, Height: h, Format: capture.FormatXRGB8888}

	enc := NewMJPEG(75)
	out, err := enc.Encode(f)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Errorf("decoded size = %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
	}
	// Top-left pixel should be red.
	r, g, b, _ := img.At(0, 0).RGBA()
	if r>>8 < 200 || g>>8 > 50 || b>>8 > 50 {
		t.Errorf("pixel = (%d,%d,%d), want red", r>>8, g>>8, b>>8)
	}
}

func TestMJPEGQualityClamp(t *testing.T) {
	enc := NewMJPEG(0)
	if enc.Quality() != 1 {
		t.Errorf("quality = %d, want 1", enc.Quality())
	}
	enc.SetQuality(1000)
	if enc.Quality() != 100 {
		t.Errorf("quality = %d, want 100", enc.Quality())
	}
}
