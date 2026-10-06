//go:build cgo

// Package h264 encodes frames as H.264 (AVC) using OpenH264 via cgo. The
// encoder emits Annex-B access units that browsers can decode with WebCodecs
// (VideoDecoder configured with avc.format="annexb").
package h264

/*
#cgo pkg-config: openh264
#include <wels/codec_api.h>
#include <stdlib.h>
#include <string.h>

// monixtend_encoder_create builds and initializes an OpenH264 encoder. It
// returns an opaque handle or NULL, with *err set to a negative code.
static void* monixtend_encoder_create(int w, int h, int bitrate, int fps, int intraPeriod, int* err) {
    ISVCEncoder* enc = NULL;
    if (WelsCreateSVCEncoder(&enc) != 0 || enc == NULL) {
        *err = -1;
        return NULL;
    }
    SEncParamExt param;
    memset(&param, 0, sizeof(param));
    (*enc)->GetDefaultParams(enc, &param);
    param.iUsageType = SCREEN_CONTENT_REAL_TIME;
    param.iPicWidth = w;
    param.iPicHeight = h;
    param.iTargetBitrate = bitrate;
    param.iRCMode = RC_BITRATE_MODE;
    param.fMaxFrameRate = (float)fps;
    param.iTemporalLayerNum = 1;
    param.iSpatialLayerNum = 1;
    param.uiIntraPeriod = intraPeriod;
    param.iNumRefFrame = 1;
    param.bEnableFrameSkip = true;
    param.iEntropyCodingModeFlag = 0;
    param.iMultipleThreadIdc = 0;
    SSpatialLayerConfig* layer = &param.sSpatialLayers[0];
    layer->iVideoWidth = w;
    layer->iVideoHeight = h;
    layer->fFrameRate = (float)fps;
    layer->iSpatialBitrate = bitrate;
    layer->iMaxSpatialBitrate = bitrate;
    layer->uiProfileIdc = PRO_BASELINE;
    layer->uiLevelIdc = LEVEL_UNKNOWN;
    layer->iDLayerQp = 26;
    layer->sSliceArgument.uiSliceMode = SM_SINGLE_SLICE;

    if ((*enc)->InitializeExt(enc, &param) != 0) {
        WelsDestroySVCEncoder(enc);
        *err = -2;
        return NULL;
    }
    int logLevel = WELS_LOG_ERROR;
    (*enc)->SetOption(enc, ENCODER_OPTION_TRACE_LEVEL, &logLevel);
    *err = 0;
    return (void*)enc;
}

// monixtend_encoder_encode encodes one I420 frame. Returns bytes written, 0 for
// a skipped frame, or a negative error code.
static int monixtend_encoder_encode(void* handle, const unsigned char* yuv,
                                    int w, int h, long long ts, int forceKey,
                                    unsigned char* outbuf, int outcap, int* isKey) {
    ISVCEncoder* enc = (ISVCEncoder*)handle;
    SSourcePicture pic;
    memset(&pic, 0, sizeof(pic));
    pic.iColorFormat = videoFormatI420;
    pic.iPicWidth = w;
    pic.iPicHeight = h;
    pic.iStride[0] = w;
    pic.iStride[1] = w / 2;
    pic.iStride[2] = w / 2;
    pic.pData[0] = (unsigned char*)yuv;
    pic.pData[1] = (unsigned char*)yuv + (w * h);
    pic.pData[2] = (unsigned char*)yuv + (w * h) + ((w / 2) * (h / 2));
    pic.uiTimeStamp = ts;

    if (forceKey) {
        (*enc)->ForceIntraFrame(enc, true);
    }

    SFrameBSInfo info;
    memset(&info, 0, sizeof(info));
    if ((*enc)->EncodeFrame(enc, &pic, &info) != 0) {
        return -1;
    }
    *isKey = (info.eFrameType == videoFrameTypeIDR || info.eFrameType == videoFrameTypeI) ? 1 : 0;
    if (info.eFrameType == videoFrameTypeSkip || info.iLayerNum <= 0) {
        return 0;
    }
    int total = 0;
    for (int i = 0; i < info.iLayerNum; i++) {
        SLayerBSInfo* layer = &info.sLayerInfo[i];
        unsigned char* p = layer->pBsBuf;
        for (int j = 0; j < layer->iNalCount; j++) {
            int len = layer->pNalLengthInByte[j];
            if (total + len > outcap) {
                return -2;
            }
            memcpy(outbuf + total, p, len);
            total += len;
            p += len;
        }
    }
    return total;
}

static void monixtend_encoder_destroy(void* handle) {
    if (handle != NULL) {
        WelsDestroySVCEncoder((ISVCEncoder*)handle);
    }
}
*/
import "C"

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/lunch/monixtend/internal/capture"
)

// Options configures the encoder.
type Options struct {
	Width            int
	Height           int
	Bitrate          int
	FPS              int
	KeyframeInterval int
}

// Encoder is an OpenH264-backed encoder. It is not safe for concurrent use.
type Encoder struct {
	enc           unsafe.Pointer
	width, height int
	yuv           []byte
	out           []byte
}

// New creates an encoder.
func New(opts Options) (*Encoder, error) {
	if opts.Width <= 0 || opts.Height <= 0 {
		return nil, fmt.Errorf("h264: invalid size %dx%d", opts.Width, opts.Height)
	}
	if opts.Width%2 != 0 || opts.Height%2 != 0 {
		return nil, fmt.Errorf("h264: dimensions must be even, got %dx%d", opts.Width, opts.Height)
	}
	if opts.FPS <= 0 {
		opts.FPS = 30
	}
	if opts.Bitrate <= 0 {
		opts.Bitrate = opts.Width * opts.Height * opts.FPS / 8
	}
	if opts.KeyframeInterval <= 0 {
		opts.KeyframeInterval = opts.FPS * 2
	}
	var cerr C.int
	handle := C.monixtend_encoder_create(
		C.int(opts.Width), C.int(opts.Height),
		C.int(opts.Bitrate), C.int(opts.FPS), C.int(opts.KeyframeInterval), &cerr)
	if handle == nil {
		return nil, fmt.Errorf("h264: initialize failed (code %d)", int(cerr))
	}
	return &Encoder{
		enc:    handle,
		width:  opts.Width,
		height: opts.Height,
		yuv:    make([]byte, opts.Width*opts.Height*3/2),
		out:    make([]byte, opts.Width*opts.Height*4+4096),
	}, nil
}

// Encode converts a frame into an Annex-B access unit. A nil slice with a nil
// error means the encoder skipped the frame. isKey reports whether the access
// unit is an IDR/keyframe.
func (e *Encoder) Encode(f *capture.Frame, forceKey bool) (data []byte, isKey bool, err error) {
	if f.Width != e.width || f.Height != e.height {
		return nil, false, fmt.Errorf("h264: frame %dx%d does not match encoder %dx%d",
			f.Width, f.Height, e.width, e.height)
	}
	bgraToI420(f, e.yuv)

	ts := C.longlong(f.PTS.UnixMilli())
	if f.PTS.IsZero() {
		ts = C.longlong(time.Now().UnixMilli())
	}
	var ckey C.int
	n := C.monixtend_encoder_encode(e.enc,
		(*C.uchar)(unsafe.Pointer(&e.yuv[0])),
		C.int(e.width), C.int(e.height), ts,
		boolToC(forceKey),
		(*C.uchar)(unsafe.Pointer(&e.out[0])), C.int(len(e.out)), &ckey)
	switch {
	case n == -1:
		return nil, false, errors.New("h264: encode failed")
	case n == -2:
		return nil, false, errors.New("h264: output buffer too small")
	case n == 0:
		return nil, false, nil
	}
	return e.out[:int(n)], ckey == 1, nil
}

// Close destroys the encoder.
func (e *Encoder) Close() {
	if e.enc != nil {
		C.monixtend_encoder_destroy(e.enc)
		e.enc = nil
	}
}

func boolToC(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// bgraToI420 converts a little-endian BGRX/BGRA frame into an I420 buffer. It
// walks 2x2 blocks so each source pixel is read exactly once, computing the
// four luma samples and the shared chroma sample together. Dimensions are
// guaranteed even by New.
func bgraToI420(f *capture.Frame, dst []byte) {
	w, h := f.Width, f.Height
	uvw := w / 2
	uvh := h / 2
	ysize := w * h
	for y := 0; y < h; y += 2 {
		row0 := f.Pix[y*f.Stride:]
		row1 := f.Pix[(y+1)*f.Stride:]
		dy0 := dst[y*w:]
		dy1 := dst[(y+1)*w:]
		ci := ysize + (y/2)*uvw
		for x := 0; x < w; x += 2 {
			b0 := int32(row0[x*4])
			g0 := int32(row0[x*4+1])
			r0 := int32(row0[x*4+2])
			b1 := int32(row0[x*4+4])
			g1 := int32(row0[x*4+5])
			r1 := int32(row0[x*4+6])
			b2 := int32(row1[x*4])
			g2 := int32(row1[x*4+1])
			r2 := int32(row1[x*4+2])
			b3 := int32(row1[x*4+4])
			g3 := int32(row1[x*4+5])
			r3 := int32(row1[x*4+6])

			dy0[x] = clamp8(((66*r0 + 129*g0 + 25*b0 + 128) >> 8) + 16)
			dy0[x+1] = clamp8(((66*r1 + 129*g1 + 25*b1 + 128) >> 8) + 16)
			dy1[x] = clamp8(((66*r2 + 129*g2 + 25*b2 + 128) >> 8) + 16)
			dy1[x+1] = clamp8(((66*r3 + 129*g3 + 25*b3 + 128) >> 8) + 16)

			r := (r0 + r1 + r2 + r3) / 4
			g := (g0 + g1 + g2 + g3) / 4
			b := (b0 + b1 + b2 + b3) / 4
			dst[ci+x/2] = clamp8(((-38*r - 74*g + 112*b + 128) >> 8) + 128)
			dst[ci+uvw*uvh+x/2] = clamp8(((112*r - 94*g - 18*b + 128) >> 8) + 128)
		}
	}
}

func clamp8(v int32) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}
