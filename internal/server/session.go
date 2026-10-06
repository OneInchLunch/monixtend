package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/lunch/monixtend/internal/capture"
	"github.com/lunch/monixtend/internal/compositor"
	"github.com/lunch/monixtend/internal/input"
	"github.com/lunch/monixtend/internal/input/uinput"
	"github.com/lunch/monixtend/internal/stream"
)

// frameEncoder encodes a frame into an Annex-B access unit. The concrete
// implementation is provided by build-tagged files so that MJPEG-only builds
// work without cgo.
type frameEncoder interface {
	Encode(f *capture.Frame, forceKey bool) (data []byte, isKey bool, err error)
	Close()
}

// session is one live virtual output plus its capture/encode pipeline.
type session struct {
	srv        *Server
	outputName string
	src        capture.Source
	enc        *stream.MJPEG
	encH264    frameEncoder
	codec      string
	fps        int
	log        *slog.Logger

	injector input.Injector

	// Geometry in Hyprland layout coordinates.
	outX, outY, outW, outH int

	frameIndex uint64

	mu     sync.Mutex
	closed bool
}

func (s *Server) newSession(ctx context.Context, width, height int, codec string) (*session, error) {
	mons, err := s.comp.Monitors(ctx)
	if err != nil {
		return nil, err
	}
	rightmost := 0
	for _, m := range mons {
		if r := m.X + m.LogicalWidth(); r > rightmost {
			rightmost = r
		}
	}

	requested := s.cfg.OutputName + "-" + shortID()
	spec := compositor.OutputSpec{
		Width:   width,
		Height:  height,
		Refresh: 60,
		X:       rightmost,
		Y:       0,
		Scale:   1,
	}
	// Some compositors (Sway) assign their own name to virtual outputs, so use
	// the name the backend reports back for capture and cleanup.
	name, err := s.comp.CreateOutput(ctx, requested, spec)
	if err != nil {
		return nil, err
	}
	src, err := capture.NewWayland(name, true)
	if err != nil {
		rctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = s.comp.RemoveOutput(rctx, name)
		cancel()
		return nil, err
	}

	// Layout bounds must cover every monitor and the new output so the
	// absolute pointer maps across the whole desktop. Positions and sizes are
	// in logical layout coordinates.
	minX, minY := spec.X, spec.Y
	maxX, maxY := spec.X+width, spec.Y+height
	for _, m := range mons {
		minX = min(minX, m.X)
		minY = min(minY, m.Y)
		maxX = max(maxX, m.X+m.LogicalWidth())
		maxY = max(maxY, m.Y+m.LogicalHeight())
	}

	sess := &session{
		srv:        s,
		outputName: name,
		src:        src,
		enc:        stream.NewMJPEG(s.cfg.JPEGQuality),
		codec:      codec,
		fps:        s.cfg.MaxFPS,
		log:        s.log.With("output", name),
		outX:       spec.X,
		outY:       spec.Y,
		outW:       width,
		outH:       height,
	}

	if codec == "h264" {
		henc, err := newH264Encoder(width, height, s.cfg.MaxFPS, s.cfg.Bitrate)
		if err != nil {
			sess.log.Warn("h264 encoder unavailable, falling back to mjpeg", "err", err)
			sess.codec = "mjpeg"
		} else {
			sess.encH264 = henc
		}
	}

	if s.cfg.DisableInput {
		sess.log.Info("input disabled by configuration")
	} else if inj, err := uinput.New(uinput.Options{
		MinX: minX, MinY: minY,
		Width: maxX - minX, Height: maxY - minY,
	}); err != nil {
		sess.log.Warn("input injection unavailable, running view-only", "err", err)
	} else {
		sess.injector = inj
		sess.log.Info("input injection enabled")
	}
	s.mu.Lock()
	s.sessions[name] = sess
	s.mu.Unlock()
	s.log.Info("session started", "output", name, "width", width, "height", height)
	return sess, nil
}

// stream captures, encodes and pushes frames until ctx is cancelled.
func (sess *session) stream(ctx context.Context, sub *subscriber) {
	if sess.fps < 1 {
		sess.fps = 30
	}
	interval := time.Second / time.Duration(sess.fps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sinceAdjust := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		frame, err := sess.src.Capture(ctx)
		if errors.Is(err, capture.ErrNoChange) {
			continue
		}
		if err != nil {
			if ctx.Err() == nil {
				sess.log.Warn("capture failed", "err", err)
			}
			return
		}
		sess.frameIndex++
		if sess.codec == "h264" {
			buf := sub.getBuf()
			sent, err := sess.encodeH264(frame, buf)
			if err != nil {
				sess.log.Warn("h264 encode failed", "err", err)
				sub.putBuf(buf)
				continue
			}
			if !sent {
				sub.putBuf(buf)
				continue
			}
			sub.sendFrame(buf)
			continue
		}

		buf := sub.getBuf()
		if err := sess.enc.EncodeInto(frame, buf.bw); err != nil {
			sess.log.Warn("encode failed", "err", err)
			sub.putBuf(buf)
			continue
		}
		sub.sendFrame(buf)

		// Adapt JPEG quality to network/decoder backpressure roughly once a
		// second.
		sinceAdjust++
		if sinceAdjust >= sess.fps {
			sinceAdjust = 0
			q := sess.enc.Quality()
			if sub.takeDrops() > 0 {
				sess.enc.SetQuality(max(40, q-5))
			} else if q < 90 {
				sess.enc.SetQuality(q + 1)
			}
		}
	}
}

// encodeH264 encodes and writes one H.264 access unit into buf. The wire format
// is a five-byte header (flags byte with bit 0 set for keyframes, then a little
// endian uint32 timestamp in milliseconds) followed by the Annex-B access unit.
// It reports whether a frame was produced: the encoder skips frames when it is
// rate-limited.
func (sess *session) encodeH264(frame *capture.Frame, buf *frameBuf) (bool, error) {
	forceKey := sess.frameIndex == 1 || sess.frameIndex%(uint64(sess.fps*2)) == 0
	au, isKey, err := sess.encH264.Encode(frame, forceKey)
	if err != nil {
		return false, err
	}
	if len(au) == 0 {
		return false, nil
	}
	var hdr [5]byte
	if isKey {
		hdr[0] = 1
	}
	binary.LittleEndian.PutUint32(hdr[1:5], uint32(frame.PTS.UnixMilli()))
	buf.bb.Reset()
	buf.bb.Write(hdr[:])
	buf.bb.Write(au)
	return true, nil
}

// Close tears down the capture source and removes the virtual output.
func (sess *session) Close() {
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	sess.closed = true
	sess.mu.Unlock()

	if err := sess.src.Close(); err != nil {
		sess.log.Debug("capture close", "err", err)
	}
	if sess.encH264 != nil {
		sess.encH264.Close()
	}
	if sess.injector != nil {
		if err := sess.injector.Close(); err != nil {
			sess.log.Debug("input close", "err", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := sess.srv.comp.RemoveOutput(ctx, sess.outputName); err != nil {
		sess.log.Warn("remove output failed", "err", err)
	}
	sess.srv.mu.Lock()
	delete(sess.srv.sessions, sess.outputName)
	sess.srv.mu.Unlock()
	sess.log.Info("session ended", "output", sess.outputName)
}

// clientMessage is the JSON control envelope sent by the browser.
type clientMessage struct {
	Type   string  `json:"type"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Button int     `json:"button"`
	Down   bool    `json:"down"`
	DX     float64 `json:"dx"`
	DY     float64 `json:"dy"`
	Code   string  `json:"code"`
}

// handleClientMessage processes a control message from the client.
func (sess *session) handleClientMessage(ctx context.Context, typ websocket.MessageType, data []byte) {
	if typ != websocket.MessageText {
		return
	}
	_ = ctx
	if sess.injector == nil {
		return
	}
	var msg clientMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	switch msg.Type {
	case "pointer":
		sess.movePointer(msg.X, msg.Y)
	case "button":
		_ = sess.injector.Button(browserButton(msg.Button), msg.Down)
	case "wheel":
		_ = sess.injector.Scroll(msg.DX, msg.DY)
	case "key":
		if code, ok := input.Keycode(msg.Code); ok {
			_ = sess.injector.Key(code, msg.Down)
		}
	}
}

func (sess *session) movePointer(nx, ny float64) {
	if nx < 0 {
		nx = 0
	} else if nx > 1 {
		nx = 1
	}
	if ny < 0 {
		ny = 0
	} else if ny > 1 {
		ny = 1
	}
	x := sess.outX + int(nx*float64(sess.outW))
	y := sess.outY + int(ny*float64(sess.outH))
	_ = sess.injector.MoveAbs(x, y)
}

func browserButton(b int) input.Button {
	switch b {
	case 1:
		return input.ButtonMiddle
	case 2:
		return input.ButtonRight
	default:
		return input.ButtonLeft
	}
}

// frameBuf bundles an encoder output buffer with a buffered writer so that
// image/jpeg writes directly into it without allocating a scratch buffer each
// frame. Buffers are recycled per in-flight frame.
type frameBuf struct {
	bb bytes.Buffer
	bw *bufio.Writer
}

func newFrameBuf() *frameBuf {
	fb := &frameBuf{}
	fb.bw = bufio.NewWriter(&fb.bb)
	return fb
}

func (f *frameBuf) reset() {
	f.bb.Reset()
	f.bw.Reset(&f.bb)
}

func (f *frameBuf) bytes() []byte { return f.bb.Bytes() }

// frameMsg is one encoded frame queued for the writer, plus the reusable
// buffer backing it so the writer can recycle it once the data is sent.
type frameMsg struct {
	buf  *frameBuf
	data []byte
}

// subscriber owns the websocket writer for a single client.
type subscriber struct {
	conn   *websocket.Conn
	frames chan frameMsg
	ctrl   chan []byte
	free   chan *frameBuf
	done   chan struct{}
	once   sync.Once
	drops  int
}

func newSubscriber(conn *websocket.Conn) *subscriber {
	return &subscriber{
		conn:   conn,
		frames: make(chan frameMsg, 1),
		ctrl:   make(chan []byte, 8),
		free:   make(chan *frameBuf, 4),
		done:   make(chan struct{}),
	}
}

func (s *subscriber) writeLoop(ctx context.Context) {
	for {
		select {
		case f := <-s.frames:
			if err := s.write(ctx, websocket.MessageBinary, f.data); err != nil {
				s.putBuf(f.buf)
				s.close()
				return
			}
			s.putBuf(f.buf)
		case c := <-s.ctrl:
			if err := s.write(ctx, websocket.MessageText, c); err != nil {
				s.close()
				return
			}
		case <-s.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *subscriber) write(ctx context.Context, typ websocket.MessageType, data []byte) error {
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.conn.Write(wctx, typ, data)
}

// getBuf returns a reusable encoding buffer. Only called from the session's
// stream goroutine.
func (s *subscriber) getBuf() *frameBuf {
	select {
	case b := <-s.free:
		b.reset()
		return b
	default:
		return newFrameBuf()
	}
}

// putBuf returns a frame buffer to the free list. Safe to call from the stream
// or writer goroutine.
func (s *subscriber) putBuf(b *frameBuf) {
	if b == nil {
		return
	}
	b.reset()
	select {
	case s.free <- b:
	default:
	}
}

// sendFrame enqueues the encoded frame held in b, dropping an older queued
// frame if the client is too slow to keep up. Dropped and delivered buffers are
// recycled via the free list. Only called from the session's stream goroutine.
func (s *subscriber) sendFrame(b *frameBuf) {
	data := b.bytes()
	select {
	case s.frames <- frameMsg{buf: b, data: data}:
		return
	default:
		s.drops++
	}
	select {
	case old := <-s.frames:
		s.putBuf(old.buf)
	default:
	}
	select {
	case s.frames <- frameMsg{buf: b, data: data}:
	default:
		s.drops++
		s.putBuf(b)
	}
}

// takeDrops returns and resets the dropped-frame counter. Only called from the
// session's stream goroutine.
func (s *subscriber) takeDrops() int {
	d := s.drops
	s.drops = 0
	return d
}

// sendControl enqueues a text control message.
func (s *subscriber) sendControl(data []byte) {
	select {
	case s.ctrl <- data:
	default:
	}
}

func (s *subscriber) close() {
	s.once.Do(func() { close(s.done) })
}

func shortID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
