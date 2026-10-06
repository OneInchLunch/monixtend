// Package capture turns a compositor output into frames the rest of monixtend
// can encode and stream.
package capture

import (
	"context"
	"errors"
	"image"
	"time"
)

// ErrNoChange is returned by Capture when the source reports that nothing in
// the output has changed since the previous frame.
var ErrNoChange = errors.New("capture: no change")

// PixelFormat is the in-memory layout of a Frame's pixels.
type PixelFormat uint32

const (
	// FormatXRGB8888 is 32-bit little-endian, bytes B,G,R,X with alpha ignored.
	FormatXRGB8888 PixelFormat = 0x01
	// FormatARGB8888 is 32-bit little-endian, bytes B,G,R,A.
	FormatARGB8888 PixelFormat = 0x00
)

// Frame is a single captured image.
type Frame struct {
	Pix    []byte
	Stride int
	Width  int
	Height int
	Format PixelFormat
	PTS    time.Time
}

// Source produces frames of a single output.
type Source interface {
	// Size returns the output dimensions in pixels.
	Size() (width, height int)
	// Capture blocks until a frame is available. It may return ErrNoChange.
	Capture(ctx context.Context) (*Frame, error)
	// Close releases the underlying connection and resources.
	Close() error
}

// ToRGBA converts a Frame into an *image.RGBA, swapping the byte order as
// needed. The returned image is always newly allocated.
func (f *Frame) ToRGBA() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, f.Width, f.Height))
	dst := img.Pix
	rowLen := f.Width * 4
	for y := 0; y < f.Height; y++ {
		srcRow := f.Pix[y*f.Stride : y*f.Stride+rowLen]
		dstRow := dst[y*img.Stride : y*img.Stride+rowLen]
		for x := 0; x < rowLen; x += 4 {
			dstRow[x] = srcRow[x+2]   // R
			dstRow[x+1] = srcRow[x+1] // G
			dstRow[x+2] = srcRow[x]   // B
			dstRow[x+3] = 0xff        // A
		}
	}
	return img
}
