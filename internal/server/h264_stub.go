//go:build !cgo

package server

import (
	"errors"

	"github.com/lunch/monixtend/internal/capture"
)

// h264Encoder is a stub for builds without cgo; H.264 encoding requires the
// OpenH264 shared library.
type h264Encoder struct{}

func newH264Encoder(width, height, fps, bitrate int) (frameEncoder, error) {
	return nil, errors.New("h264 codec requires a cgo build (CGO_ENABLED=1)")
}

// Encode implements frameEncoder.
func (e *h264Encoder) Encode(f *capture.Frame, forceKey bool) ([]byte, bool, error) {
	return nil, false, errors.New("h264 codec not available in this build")
}

// Close implements frameEncoder.
func (e *h264Encoder) Close() {}
