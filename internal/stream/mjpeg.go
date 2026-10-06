// Package stream encodes captured frames into wire formats the browser can
// decode.
package stream

import (
	"bytes"
	"image"
	"image/jpeg"

	"github.com/lunch/monixtend/internal/capture"
)

// MJPEG encodes frames as baseline JPEG. It reuses its scratch buffers across
// calls and is not safe for concurrent use.
type MJPEG struct {
	quality int
	buf     bytes.Buffer
	img     *image.YCbCr
	cbAcc   []int32
	crAcc   []int32
}

// NewMJPEG returns an encoder with the given quality (1-100).
func NewMJPEG(quality int) *MJPEG {
	e := &MJPEG{}
	e.SetQuality(quality)
	return e
}

// SetQuality changes the encoder quality. It is safe to call between Encode
// calls.
func (e *MJPEG) SetQuality(q int) {
	if q < 1 {
		q = 1
	}
	if q > 100 {
		q = 100
	}
	e.quality = q
}

// Quality returns the current encoder quality.
func (e *MJPEG) Quality() int { return e.quality }

// Encode converts a frame to JPEG. The returned slice is owned by the caller.
func (e *MJPEG) Encode(f *capture.Frame) ([]byte, error) {
	if e.img == nil || e.img.Rect.Dx() != f.Width || e.img.Rect.Dy() != f.Height {
		e.img = image.NewYCbCr(image.Rect(0, 0, f.Width, f.Height), image.YCbCrSubsampleRatio420)
		e.cbAcc = make([]int32, (f.Width+1)/2*((f.Height+1)/2))
		e.crAcc = make([]int32, len(e.cbAcc))
	}
	bgraToYCbCr(f, e.img, e.cbAcc, e.crAcc)
	e.buf.Reset()
	if err := jpeg.Encode(&e.buf, e.img, &jpeg.Options{Quality: e.quality}); err != nil {
		return nil, err
	}
	out := make([]byte, e.buf.Len())
	copy(out, e.buf.Bytes())
	return out, nil
}

// bgraToYCbCr rewrites a little-endian BGRX/BGRA frame directly into a 4:2:0
// YCbCr image, using the same JFIF coefficients as image/jpeg's own RGBA
// conversion. Building the planes here lets the encoder copy them verbatim,
// avoiding both a full-frame RGBA buffer and the encoder's per-pixel colour
// conversion. Dimensions are assumed even; callers guarantee this.
func bgraToYCbCr(f *capture.Frame, dst *image.YCbCr, cbAcc, crAcc []int32) {
	w, h := f.Width, f.Height
	cw := (w + 1) / 2
	for y := 0; y < h; y++ {
		src := f.Pix[y*f.Stride:]
		dy := dst.Y[y*dst.YStride:]
		crow := (y / 2) * cw
		for x := 0; x < w; x++ {
			b := int32(src[x*4])
			g := int32(src[x*4+1])
			r := int32(src[x*4+2])
			dy[x] = byte((19595*r + 38470*g + 7471*b + 1<<15) >> 16)

			cb := -11056*r - 21712*g + 32768*b + 257<<15
			if uint32(cb)&0xff000000 == 0 {
				cb >>= 16
			} else {
				cb = ^(cb >> 31)
			}
			cr := 32768*r - 27440*g - 5328*b + 257<<15
			if uint32(cr)&0xff000000 == 0 {
				cr >>= 16
			} else {
				cr = ^(cr >> 31)
			}
			ci := crow + x/2
			cbAcc[ci] += cb
			crAcc[ci] += cr
		}
	}
	for i := range cbAcc {
		dst.Cb[i] = byte((cbAcc[i] + 2) >> 2)
		dst.Cr[i] = byte((crAcc[i] + 2) >> 2)
		cbAcc[i] = 0
		crAcc[i] = 0
	}
}
