package capture

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rajveermalviya/go-wayland/wayland/client"
	"golang.org/x/sys/unix"

	"github.com/lunch/monixtend/internal/capture/screencopy"
)

// Wayland captures a single output using the wlr-screencopy protocol over a
// dedicated Wayland client connection. All work is single threaded; the caller
// must not call Capture concurrently.
type Wayland struct {
	display *client.Display
	shm     *client.Shm
	manager *screencopy.Manager
	output  *client.Output

	overlayCursor bool
	firstFrame    bool

	protoErr error

	width  int
	height int

	buffers []*waylandBuffer
	scratch []byte
	closed  bool
}

// NewWayland connects to the ambient Wayland session and prepares to capture
// outputName. The wlr-screencopy manager must be advertised by the compositor.
func NewWayland(outputName string, overlayCursor bool) (*Wayland, error) {
	display, err := client.Connect("")
	if err != nil {
		return nil, fmt.Errorf("capture: connect wayland: %w", err)
	}
	w := &Wayland{display: display, overlayCursor: overlayCursor, firstFrame: true}
	if err := w.setup(outputName); err != nil {
		_ = display.Context().Close()
		return nil, err
	}
	return w, nil
}

func (w *Wayland) setup(outputName string) error {
	ctx := w.display.Context()
	w.display.SetErrorHandler(func(e client.DisplayErrorEvent) {
		w.protoErr = fmt.Errorf("wayland protocol error: code=%d message=%s", e.Code, e.Message)
	})
	registry, err := w.display.GetRegistry()
	if err != nil {
		return err
	}

	type outputRef struct {
		name string
		out  *client.Output
	}
	var outputs []*outputRef

	registry.SetGlobalHandler(func(e client.RegistryGlobalEvent) {
		switch e.Interface {
		case "wl_shm":
			shm := client.NewShm(ctx)
			if err := bindGlobal(ctx, registry.ID(), e.Name, e.Interface, 1, shm); err == nil {
				w.shm = shm
			}
		case "wl_output":
			v := e.Version
			if v > 4 {
				v = 4
			}
			out := client.NewOutput(ctx)
			ref := &outputRef{out: out}
			out.SetNameHandler(func(ev client.OutputNameEvent) { ref.name = ev.Name })
			outputs = append(outputs, ref)
			_ = bindGlobal(ctx, registry.ID(), e.Name, e.Interface, v, out)
		case screencopy.ManagerName:
			v := e.Version
			if v > 3 {
				v = 3
			}
			m := screencopy.NewManager(ctx)
			if err := bindGlobal(ctx, registry.ID(), e.Name, e.Interface, v, m); err == nil {
				w.manager = m
			}
		}
	})

	if err := w.roundtrip(); err != nil {
		return err
	}
	// Second roundtrip lets output name events arrive.
	if err := w.roundtrip(); err != nil {
		return err
	}
	_ = registry.Destroy()

	if w.shm == nil {
		return errors.New("capture: compositor does not advertise wl_shm")
	}
	if w.manager == nil {
		return errors.New("capture: compositor does not advertise wlr-screencopy (wlr-screencopy-unstable-v1)")
	}
	for _, ref := range outputs {
		if ref.name == outputName {
			w.output = ref.out
			break
		}
	}
	if w.output == nil {
		names := make([]string, 0, len(outputs))
		for _, ref := range outputs {
			names = append(names, ref.name)
		}
		return fmt.Errorf("capture: output %q not found among %v", outputName, names)
	}
	return nil
}

// bindGlobal issues a wl_registry.bind request. It is reimplemented here
// because the go-wayland version in use writes the padded interface name
// length instead of the true length including the trailing NUL, which the
// compositor rejects with "invalid arguments for wl_registry.bind".
func bindGlobal(ctx *client.Context, registryID, name uint32, iface string, version uint32, id client.Proxy) error {
	ifaceLen := client.PaddedLen(len(iface) + 1)
	reqBufLen := 8 + 4 + (4 + ifaceLen) + 4 + 4
	buf := make([]byte, reqBufLen)
	client.PutUint32(buf[0:4], registryID)
	client.PutUint32(buf[4:8], uint32(reqBufLen<<16))
	client.PutUint32(buf[8:12], name)
	client.PutUint32(buf[12:16], uint32(len(iface)+1))
	copy(buf[16:], iface)
	client.PutUint32(buf[16+ifaceLen:16+ifaceLen+4], version)
	client.PutUint32(buf[16+ifaceLen+4:16+ifaceLen+8], id.ID())
	return ctx.WriteMsg(buf, nil)
}

func (w *Wayland) roundtrip() error {
	cb, err := w.display.Sync()
	if err != nil {
		return err
	}
	done := false
	cb.SetDoneHandler(func(client.CallbackDoneEvent) { done = true })
	for !done {
		if err := w.display.Context().Dispatch(); err != nil {
			if w.protoErr != nil {
				return w.protoErr
			}
			return err
		}
	}
	if w.protoErr != nil {
		return w.protoErr
	}
	return nil
}

// Size implements Source.
func (w *Wayland) Size() (int, int) { return w.width, w.height }

// Capture implements Source.
func (w *Wayland) Capture(ctx context.Context) (*Frame, error) {
	if w.closed {
		return nil, errors.New("capture: source closed")
	}

	frame, err := w.manager.CaptureOutput(w.overlayCursor, w.output)
	if err != nil {
		return nil, fmt.Errorf("capture: request frame: %w", err)
	}

	var (
		ready     bool
		failed    bool
		capErr    error
		hasDamage bool
		bufRef    *waylandBuffer
	)

	frame.SetBufferHandler(func(e screencopy.FrameBufferEvent) {
		b, err := w.acquireBuffer(e)
		if err != nil {
			capErr = err
			failed = true
			return
		}
		bufRef = b
		w.width = int(e.Width)
		w.height = int(e.Height)
		if err := frame.CopyWithDamage(b.buf); err != nil {
			capErr = err
			failed = true
		}
	})
	frame.SetDamageHandler(func(e screencopy.FrameDamageEvent) {
		if e.Width > 0 && e.Height > 0 {
			hasDamage = true
		}
	})
	frame.SetReadyHandler(func(screencopy.FrameReadyEvent) { ready = true })
	frame.SetFailedHandler(func(screencopy.FrameFailedEvent) { failed = true })

	for !ready && !failed && capErr == nil {
		if err := ctx.Err(); err != nil {
			_ = frame.Destroy()
			return nil, err
		}
		if err := w.display.Context().Dispatch(); err != nil {
			_ = frame.Destroy()
			return nil, fmt.Errorf("capture: dispatch: %w", err)
		}
	}
	_ = frame.Destroy()

	if capErr != nil {
		return nil, capErr
	}
	if failed {
		return nil, errors.New("capture: compositor reported frame failure")
	}
	if !hasDamage && !w.firstFrame {
		return nil, ErrNoChange
	}
	w.firstFrame = false

	// Copy the compositor-owned shm buffer into a reused scratch buffer. The
	// buffer may be resubmitted on the next Capture, so a copy keeps the frame
	// valid for the caller; reusing the allocation avoids ~one frame's worth
	// of garbage per tick.
	if cap(w.scratch) < len(bufRef.data) {
		w.scratch = make([]byte, len(bufRef.data))
	}
	pix := w.scratch[:len(bufRef.data)]
	copy(pix, bufRef.data)

	f := &Frame{
		Pix:    pix,
		Stride: bufRef.stride,
		Width:  bufRef.width,
		Height: bufRef.height,
		Format: PixelFormat(bufRef.format),
		PTS:    time.Now(),
	}
	return f, nil
}

// Close implements Source.
func (w *Wayland) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.manager != nil {
		_ = w.manager.Destroy()
	}
	w.destroyAll()
	if w.manager != nil {
		// Flush pending destroy requests without waiting indefinitely.
		_ = w.display.Context().Dispatch()
	}
	return w.display.Context().Close()
}

type waylandBuffer struct {
	w      *Wayland
	pool   *client.ShmPool
	buf    *client.Buffer
	data   []byte
	fd     int
	width  int
	height int
	stride int
	format uint32
	busy   bool
}

// acquireBuffer returns a buffer matching the requested geometry, allocating a
// new one if necessary. Buffers are never destroyed mid-session because the
// compositor may deliver wl_buffer.release events at unpredictable times; the
// proxy must remain registered for Dispatch to find it.
func (w *Wayland) acquireBuffer(e screencopy.FrameBufferEvent) (*waylandBuffer, error) {
	var fallback *waylandBuffer
	for _, b := range w.buffers {
		if b.width != int(e.Width) || b.height != int(e.Height) || b.stride != int(e.Stride) || b.format != e.Format {
			continue
		}
		if !b.busy {
			b.busy = true
			return b, nil
		}
		if fallback == nil {
			fallback = b
		}
	}
	if fallback != nil && len(w.buffers) >= maxBuffers {
		// All buffers are busy; the previous copy is complete (Capture is
		// synchronous and waits for ready), so reuse it.
		return fallback, nil
	}
	b, err := w.newBuffer(e)
	if err != nil {
		return nil, err
	}
	b.busy = true
	w.buffers = append(w.buffers, b)
	return b, nil
}

// maxBuffers bounds the buffer pool while still allowing a little pipelining.
const maxBuffers = 4

func (w *Wayland) newBuffer(e screencopy.FrameBufferEvent) (*waylandBuffer, error) {
	if e.Format != uint32(client.ShmFormatXrgb8888) && e.Format != uint32(client.ShmFormatArgb8888) {
		return nil, fmt.Errorf("capture: unsupported shm format %d", e.Format)
	}
	size := int(e.Stride) * int(e.Height)
	fd, err := unix.MemfdCreate("monixtend-shm", unix.MFD_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("capture: memfd: %w", err)
	}
	if err := unix.Ftruncate(fd, int64(size)); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("capture: ftruncate: %w", err)
	}
	data, err := unix.Mmap(fd, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("capture: mmap: %w", err)
	}
	pool, err := w.shm.CreatePool(fd, int32(size))
	if err != nil {
		_ = unix.Munmap(data)
		_ = unix.Close(fd)
		return nil, fmt.Errorf("capture: create pool: %w", err)
	}
	buf, err := pool.CreateBuffer(0, int32(e.Width), int32(e.Height), int32(e.Stride), e.Format)
	if err != nil {
		_ = pool.Destroy()
		_ = unix.Munmap(data)
		_ = unix.Close(fd)
		return nil, fmt.Errorf("capture: create buffer: %w", err)
	}

	b := &waylandBuffer{
		w:      w,
		pool:   pool,
		buf:    buf,
		data:   data,
		fd:     fd,
		width:  int(e.Width),
		height: int(e.Height),
		stride: int(e.Stride),
		format: e.Format,
	}
	buf.SetReleaseHandler(func(client.BufferReleaseEvent) { b.busy = false })
	return b, nil
}

// destroyAll releases every pooled buffer. It is only called from Close, after
// the capture loop has stopped, so no further events are dispatched.
func (w *Wayland) destroyAll() {
	for _, b := range w.buffers {
		_ = b.buf.Destroy()
		_ = b.pool.Destroy()
		if b.data != nil {
			_ = unix.Munmap(b.data)
			b.data = nil
		}
		if b.fd >= 0 {
			_ = unix.Close(b.fd)
			b.fd = -1
		}
	}
	w.buffers = nil
}
