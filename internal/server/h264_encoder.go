//go:build cgo

package server

import (
	"github.com/lunch/monixtend/internal/capture"
	"github.com/lunch/monixtend/internal/stream/h264"
)

// h264Encoder adapts the OpenH264 cgo encoder to the frameEncoder interface.
type h264Encoder struct {
	enc *h264.Encoder
}

func newH264Encoder(width, height, fps, bitrate int) (frameEncoder, error) {
	e, err := h264.New(h264.Options{
		Width: width, Height: height,
		FPS: fps, Bitrate: bitrate,
	})
	if err != nil {
		return nil, err
	}
	return &h264Encoder{enc: e}, nil
}

// Encode implements frameEncoder.
func (e *h264Encoder) Encode(f *capture.Frame, forceKey bool) ([]byte, bool, error) {
	return e.enc.Encode(f, forceKey)
}

// Close implements frameEncoder.
func (e *h264Encoder) Close() { e.enc.Close() }
