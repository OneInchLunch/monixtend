// Package screencopy contains hand-maintained Go bindings for the
// wlr-screencopy-unstable-v1 protocol, modelled on the code emitted by
// go-wayland-scanner. It is the only Wayland protocol monixtend needs that the
// upstream go-wayland module does not ship.
//
// Protocol source: wlr-protocols/unstable/wlr-screencopy-unstable-v1.xml
package screencopy

import "github.com/rajveermalviya/go-wayland/wayland/client"

// ManagerName is the wl_registry interface name for the manager global.
const ManagerName = "zwlr_screencopy_manager_v1"

// Manager : manager to inform clients and begin capturing.
type Manager struct {
	client.BaseProxy
}

// NewManager registers a new manager proxy with the context.
func NewManager(ctx *client.Context) *Manager {
	m := &Manager{}
	ctx.Register(m)
	return m
}

// CaptureOutput captures the next frame of an output. When overlayCursor is
// true the cursor is composited into the frame. The returned Frame is
// populated by events dispatched on the shared context.
func (m *Manager) CaptureOutput(overlayCursor bool, output *client.Output) (*Frame, error) {
	frame := NewFrame(m.Context())
	const opcode = 0
	const reqBufLen = 8 + 4 + 4 + 4
	var reqBuf [reqBufLen]byte
	l := 0
	client.PutUint32(reqBuf[l:l+4], m.ID())
	l += 4
	client.PutUint32(reqBuf[l:l+4], uint32(reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	client.PutUint32(reqBuf[l:l+4], frame.ID())
	l += 4
	client.PutUint32(reqBuf[l:l+4], boolToUint32(overlayCursor))
	l += 4
	client.PutUint32(reqBuf[l:l+4], output.ID())
	l += 4
	err := m.Context().WriteMsg(reqBuf[:], nil)
	return frame, err
}

// CaptureOutputRegion captures the next frame of a region of an output.
func (m *Manager) CaptureOutputRegion(overlayCursor bool, output *client.Output, x, y, width, height int32) (*Frame, error) {
	frame := NewFrame(m.Context())
	const opcode = 1
	const reqBufLen = 8 + 4 + 4 + 4 + 4 + 4 + 4 + 4
	var reqBuf [reqBufLen]byte
	l := 0
	client.PutUint32(reqBuf[l:l+4], m.ID())
	l += 4
	client.PutUint32(reqBuf[l:l+4], uint32(reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	client.PutUint32(reqBuf[l:l+4], frame.ID())
	l += 4
	client.PutUint32(reqBuf[l:l+4], boolToUint32(overlayCursor))
	l += 4
	client.PutUint32(reqBuf[l:l+4], output.ID())
	l += 4
	client.PutUint32(reqBuf[l:l+4], uint32(x))
	l += 4
	client.PutUint32(reqBuf[l:l+4], uint32(y))
	l += 4
	client.PutUint32(reqBuf[l:l+4], uint32(width))
	l += 4
	client.PutUint32(reqBuf[l:l+4], uint32(height))
	l += 4
	err := m.Context().WriteMsg(reqBuf[:], nil)
	return frame, err
}

// Destroy asks the compositor to release the manager object.
func (m *Manager) Destroy() error {
	defer m.Context().Unregister(m)
	const opcode = 2
	const reqBufLen = 8
	var reqBuf [reqBufLen]byte
	client.PutUint32(reqBuf[0:4], m.ID())
	client.PutUint32(reqBuf[4:8], uint32(reqBufLen<<16|opcode&0x0000ffff))
	return m.Context().WriteMsg(reqBuf[:], nil)
}

// Frame : a frame ready to be copied.
type Frame struct {
	client.BaseProxy
	bufferHandler      FrameBufferHandlerFunc
	flagsHandler       FrameFlagsHandlerFunc
	readyHandler       FrameReadyHandlerFunc
	failedHandler      FrameFailedHandlerFunc
	damageHandler      FrameDamageHandlerFunc
	linuxDmabufHandler FrameLinuxDmabufHandlerFunc
	bufferDoneHandler  FrameBufferDoneHandlerFunc
}

// NewFrame registers a new frame proxy with the context.
func NewFrame(ctx *client.Context) *Frame {
	f := &Frame{}
	ctx.Register(f)
	return f
}

// Copy copies the frame into the supplied shared-memory buffer.
func (f *Frame) Copy(buffer *client.Buffer) error {
	const opcode = 0
	const reqBufLen = 8 + 4
	var reqBuf [reqBufLen]byte
	client.PutUint32(reqBuf[0:4], f.ID())
	client.PutUint32(reqBuf[4:8], uint32(reqBufLen<<16|opcode&0x0000ffff))
	client.PutUint32(reqBuf[8:12], buffer.ID())
	return f.Context().WriteMsg(reqBuf[:], nil)
}

// Destroy releases the frame object.
func (f *Frame) Destroy() error {
	defer f.Context().Unregister(f)
	const opcode = 1
	const reqBufLen = 8
	var reqBuf [reqBufLen]byte
	client.PutUint32(reqBuf[0:4], f.ID())
	client.PutUint32(reqBuf[4:8], uint32(reqBufLen<<16|opcode&0x0000ffff))
	return f.Context().WriteMsg(reqBuf[:], nil)
}

// CopyWithDamage copies the frame, emitting damage events for changed regions.
func (f *Frame) CopyWithDamage(buffer *client.Buffer) error {
	const opcode = 2
	const reqBufLen = 8 + 4
	var reqBuf [reqBufLen]byte
	client.PutUint32(reqBuf[0:4], f.ID())
	client.PutUint32(reqBuf[4:8], uint32(reqBufLen<<16|opcode&0x0000ffff))
	client.PutUint32(reqBuf[8:12], buffer.ID())
	return f.Context().WriteMsg(reqBuf[:], nil)
}

// FrameBufferEvent describes the buffer the compositor will copy into.
type FrameBufferEvent struct {
	Format uint32 // wl_shm.format
	Width  uint32
	Height uint32
	Stride uint32
}

// FrameFlagsHandlerFunc handles FrameBufferEvent.
type FrameBufferHandlerFunc func(FrameBufferEvent)

// SetBufferHandler sets the handler for FrameBufferEvent.
func (f *Frame) SetBufferHandler(h FrameBufferHandlerFunc) { f.bufferHandler = h }

// FrameFlagsEvent carries hint flags about the frame contents.
type FrameFlagsEvent struct {
	Flags uint32
}

// FrameFlagsHandlerFunc handles FrameFlagsEvent.
type FrameFlagsHandlerFunc func(FrameFlagsEvent)

// SetFlagsHandler sets the handler for FrameFlagsEvent.
func (f *Frame) SetFlagsHandler(h FrameFlagsHandlerFunc) { f.flagsHandler = h }

// FrameReadyEvent signals the copy completed and carries a timestamp.
type FrameReadyEvent struct {
	TvSecHi uint32
	TvSecLo uint32
	TvNsec  uint32
}

// FrameReadyHandlerFunc handles FrameReadyEvent.
type FrameReadyHandlerFunc func(FrameReadyEvent)

// SetReadyHandler sets the handler for FrameReadyEvent.
func (f *Frame) SetReadyHandler(h FrameReadyHandlerFunc) { f.readyHandler = h }

// FrameFailedEvent signals the capture failed.
type FrameFailedEvent struct{}

// FrameFailedHandlerFunc handles FrameFailedEvent.
type FrameFailedHandlerFunc func(FrameFailedEvent)

// SetFailedHandler sets the handler for FrameFailedEvent.
func (f *Frame) SetFailedHandler(h FrameFailedHandlerFunc) { f.failedHandler = h }

// FrameDamageEvent describes a changed region.
type FrameDamageEvent struct {
	X      uint32
	Y      uint32
	Width  uint32
	Height uint32
}

// FrameDamageHandlerFunc handles FrameDamageEvent.
type FrameDamageHandlerFunc func(FrameDamageEvent)

// SetDamageHandler sets the handler for FrameDamageEvent.
func (f *Frame) SetDamageHandler(h FrameDamageHandlerFunc) { f.damageHandler = h }

// FrameLinuxDmabufEvent describes a dmabuf buffer the compositor will use.
type FrameLinuxDmabufEvent struct {
	Format uint32
	Width  uint32
	Height uint32
}

// FrameLinuxDmabufHandlerFunc handles FrameLinuxDmabufEvent.
type FrameLinuxDmabufHandlerFunc func(FrameLinuxDmabufEvent)

// SetLinuxDmabufHandler sets the handler for FrameLinuxDmabufEvent.
func (f *Frame) SetLinuxDmabufHandler(h FrameLinuxDmabufHandlerFunc) { f.linuxDmabufHandler = h }

// FrameBufferDoneEvent signals all buffer descriptions have been sent.
type FrameBufferDoneEvent struct{}

// FrameBufferDoneHandlerFunc handles FrameBufferDoneEvent.
type FrameBufferDoneHandlerFunc func(FrameBufferDoneEvent)

// SetBufferDoneHandler sets the handler for FrameBufferDoneEvent.
func (f *Frame) SetBufferDoneHandler(h FrameBufferDoneHandlerFunc) { f.bufferDoneHandler = h }

// Dispatch implements client.Dispatcher.
func (f *Frame) Dispatch(opcode uint32, fd int, data []byte) {
	switch opcode {
	case 0:
		if f.bufferHandler == nil {
			return
		}
		var e FrameBufferEvent
		l := 0
		e.Format = client.Uint32(data[l : l+4])
		l += 4
		e.Width = client.Uint32(data[l : l+4])
		l += 4
		e.Height = client.Uint32(data[l : l+4])
		l += 4
		e.Stride = client.Uint32(data[l : l+4])
		f.bufferHandler(e)
	case 1:
		if f.flagsHandler == nil {
			return
		}
		var e FrameFlagsEvent
		e.Flags = client.Uint32(data[0:4])
		f.flagsHandler(e)
	case 2:
		if f.readyHandler == nil {
			return
		}
		var e FrameReadyEvent
		l := 0
		e.TvSecHi = client.Uint32(data[l : l+4])
		l += 4
		e.TvSecLo = client.Uint32(data[l : l+4])
		l += 4
		e.TvNsec = client.Uint32(data[l : l+4])
		f.readyHandler(e)
	case 3:
		if f.failedHandler == nil {
			return
		}
		f.failedHandler(FrameFailedEvent{})
	case 4:
		if f.damageHandler == nil {
			return
		}
		var e FrameDamageEvent
		l := 0
		e.X = client.Uint32(data[l : l+4])
		l += 4
		e.Y = client.Uint32(data[l : l+4])
		l += 4
		e.Width = client.Uint32(data[l : l+4])
		l += 4
		e.Height = client.Uint32(data[l : l+4])
		f.damageHandler(e)
	case 5:
		if f.linuxDmabufHandler == nil {
			return
		}
		var e FrameLinuxDmabufEvent
		l := 0
		e.Format = client.Uint32(data[l : l+4])
		l += 4
		e.Width = client.Uint32(data[l : l+4])
		l += 4
		e.Height = client.Uint32(data[l : l+4])
		f.linuxDmabufHandler(e)
	case 6:
		if f.bufferDoneHandler == nil {
			return
		}
		f.bufferDoneHandler(FrameBufferDoneEvent{})
	}
}

func boolToUint32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}
