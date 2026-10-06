// Package wlrogm is a small, self-contained Wayland client for the
// zwlr_output_management_unstable_v1 protocol.
//
// The general-purpose go-wayland client cannot route events for objects the
// server creates via new_id (this protocol creates heads and modes that way),
// so this package maintains its own object table and dispatch loop while
// reusing the client package only for socket I/O and wire helpers.
package wlrogm

import (
	"errors"
	"fmt"
	"sync"

	"github.com/rajveermalviya/go-wayland/wayland/client"
)

// ManagerName is the wl_registry interface name of the manager global.
const ManagerName = "zwlr_output_manager_v1"

// Output is a snapshot of one head.
type Output struct {
	Name        string
	Description string
	Make        string
	Model       string
	Serial      string
	Enabled     bool
	Width       int
	Height      int
	Refresh     int // millihertz
	X           int
	Y           int
	Transform   int
	Scale       float64
	Modes       []Mode
}

// Mode is one output mode.
type Mode struct {
	Width     int
	Height    int
	Refresh   int // millihertz
	Preferred bool
}

// transport abstracts the connection so the dispatch logic can be unit tested.
type transport interface {
	readMsg() (senderID, opcode uint32, fd int, data []byte, err error)
	writeMsg(b, oob []byte) error
}

type connTransport struct{ ctx *client.Context }

func (t connTransport) readMsg() (uint32, uint32, int, []byte, error) {
	return t.ctx.ReadMsg()
}

func (t connTransport) writeMsg(b, oob []byte) error { return t.ctx.WriteMsg(b, oob) }

// Client owns one Wayland connection and the output-management state.
type Client struct {
	display  *client.Display
	ctx      *client.Context
	registry *client.Registry
	tr       transport

	mu      sync.Mutex
	objects map[uint32]client.Dispatcher

	manager *Manager
	serial  uint32
	heads   []*Head
	err     error
}

// Connect opens the ambient Wayland session and binds the output manager.
func Connect() (*Client, error) {
	display, err := client.Connect("")
	if err != nil {
		return nil, err
	}
	ctx := display.Context()
	c := &Client{
		display: display,
		ctx:     ctx,
		tr:      connTransport{ctx: ctx},
		objects: map[uint32]client.Dispatcher{},
	}
	c.add(display)
	display.SetErrorHandler(func(e client.DisplayErrorEvent) {
		c.err = fmt.Errorf("wayland display error: %s (object %d)", e.Message, proxyID(e.ObjectId))
	})

	registry, err := display.GetRegistry()
	if err != nil {
		_ = ctx.Close()
		return nil, err
	}
	c.registry = registry
	c.add(registry)
	registry.SetGlobalHandler(c.onGlobal)

	if err := c.init(); err != nil {
		_ = ctx.Close()
		return nil, err
	}
	return c, nil
}

// Close releases the Wayland connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctx.Close()
}

func (c *Client) init() error {
	// First round trip discovers globals (and sends the bind request from the
	// registry handler); the second receives the manager's head events.
	if err := c.roundtrip(); err != nil {
		return err
	}
	if c.manager == nil {
		return fmt.Errorf("wlrogm: compositor does not advertise %s", ManagerName)
	}
	return c.roundtrip()
}

func (c *Client) onGlobal(e client.RegistryGlobalEvent) {
	if e.Interface != ManagerName {
		return
	}
	version := e.Version
	if version > 1 {
		// Only version-1 requests are used; higher event versions are ignored.
		version = 1
	}
	m := newManager(c)
	if err := c.bind(c.registry.ID(), e.Name, e.Interface, version, m.ID()); err != nil {
		c.err = err
		return
	}
	c.manager = m
	c.add(m)
}

// bind issues a wl_registry.bind request. It is reimplemented because the
// go-wayland Registry.Bind writes the padded interface length instead of the
// true length including the trailing NUL.
func (c *Client) bind(registryID, name uint32, iface string, version, id uint32) error {
	ifaceLen := client.PaddedLen(len(iface) + 1)
	reqBufLen := 8 + 4 + (4 + ifaceLen) + 4 + 4
	buf := make([]byte, reqBufLen)
	client.PutUint32(buf[0:4], registryID)
	client.PutUint32(buf[4:8], uint32(reqBufLen<<16)) // opcode 0
	client.PutUint32(buf[8:12], name)
	client.PutUint32(buf[12:16], uint32(len(iface)+1))
	copy(buf[16:], iface)
	client.PutUint32(buf[16+ifaceLen:16+ifaceLen+4], version)
	client.PutUint32(buf[16+ifaceLen+4:16+ifaceLen+8], id)
	return c.tr.writeMsg(buf, nil)
}

// send writes one request with the given object id, opcode and raw arguments.
func (c *Client) send(objID, opcode uint32, args []byte) error {
	size := 8 + len(args)
	buf := make([]byte, size)
	client.PutUint32(buf[0:4], objID)
	client.PutUint32(buf[4:8], uint32(size)<<16|opcode&0xffff)
	copy(buf[8:], args)
	return c.tr.writeMsg(buf, nil)
}

// roundtrip flushes pending requests and dispatches events until a wl_callback
// created after them fires.
func (c *Client) roundtrip() error {
	cb, err := c.display.Sync()
	if err != nil {
		return err
	}
	c.add(cb)
	defer func() {
		c.remove(cb)
		c.ctx.Unregister(cb)
	}()

	done := false
	cb.SetDoneHandler(func(client.CallbackDoneEvent) { done = true })
	for !done {
		if err := c.readAndDispatch(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) readAndDispatch() error {
	sender, opcode, _, data, err := c.tr.readMsg()
	if err != nil {
		return err
	}
	if c.err != nil {
		return c.err
	}
	if err := c.handleEvent(sender, opcode, data); err != nil {
		return err
	}
	return c.err
}

// handleEvent routes one event to the object that sent it.
func (c *Client) handleEvent(sender, opcode uint32, data []byte) error {
	d := c.objects[sender]
	if d == nil {
		return fmt.Errorf("wlrogm: event (opcode %d) for unknown object %d", opcode, sender)
	}
	d.Dispatch(opcode, 0, data)
	return nil
}

func (c *Client) add(d client.Dispatcher) {
	if p, ok := d.(client.Proxy); ok {
		c.objects[p.ID()] = d
	}
}

func (c *Client) remove(d client.Dispatcher) {
	if p, ok := d.(client.Proxy); ok {
		delete(c.objects, p.ID())
	}
}

// Monitors returns a snapshot of all heads. It synchronizes with the compositor
// first so the snapshot reflects any pending changes.
func (c *Client) Monitors() ([]Output, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.roundtrip(); err != nil {
		return nil, err
	}
	return c.snapshot(), nil
}

func (c *Client) snapshot() []Output {
	outs := make([]Output, 0, len(c.heads))
	for _, h := range c.heads {
		if h.finished || h.name == "" {
			continue
		}
		o := Output{
			Name:        h.name,
			Description: h.description,
			Make:        h.make,
			Model:       h.model,
			Serial:      h.serial,
			Enabled:     h.enabled,
			X:           h.posX,
			Y:           h.posY,
			Transform:   h.transform,
			Scale:       h.scale,
		}
		if h.current != nil {
			o.Width = h.current.width
			o.Height = h.current.height
			o.Refresh = h.current.refresh
		}
		for _, m := range h.modes {
			o.Modes = append(o.Modes, Mode{
				Width: m.width, Height: m.height,
				Refresh: m.refresh, Preferred: m.preferred,
			})
		}
		outs = append(outs, o)
	}
	return outs
}

// SetGeometry reconfigures an existing head with a custom mode, position and
// scale. The protocol cannot create heads, so this only works on outputs the
// compositor already provides.
func (c *Client) SetGeometry(name string, width, height, refresh, x, y int, scale float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.roundtrip(); err != nil {
		return err
	}
	head := c.headByName(name)
	if head == nil {
		return fmt.Errorf("wlrogm: output %q not found", name)
	}
	if !head.enabled {
		return fmt.Errorf("wlrogm: output %q is disabled", name)
	}

	cfg := newConfiguration(c)
	cfgHead := newConfigurationHead(c)
	c.add(cfg)
	c.add(cfgHead)
	defer func() {
		c.remove(cfg)
		c.remove(cfgHead)
		c.ctx.Unregister(cfg)
		c.ctx.Unregister(cfgHead)
	}()

	if err := cfg.enableHead(cfgHead.ID(), head.ID()); err != nil {
		return err
	}
	if err := cfgHead.setCustomMode(width, height, refresh); err != nil {
		return err
	}
	if err := cfgHead.setPosition(x, y); err != nil {
		return err
	}
	if err := cfgHead.setScale(scale); err != nil {
		return err
	}
	if err := cfg.apply(); err != nil {
		return err
	}

	for !cfg.done {
		if err := c.readAndDispatch(); err != nil {
			return err
		}
	}
	_ = cfg.destroy()
	if cfg.failed {
		return errors.New("wlrogm: compositor rejected the output configuration")
	}
	if cfg.cancelled {
		return errors.New("wlrogm: output configuration was cancelled")
	}
	return nil
}

func (c *Client) headByName(name string) *Head {
	for _, h := range c.heads {
		if h.name == name && !h.finished {
			return h
		}
	}
	return nil
}

func proxyID(p client.Proxy) uint32 {
	if p == nil {
		return 0
	}
	return p.ID()
}

// u32 packs uint32 arguments in wire order.
func u32(vals ...uint32) []byte {
	b := make([]byte, 4*len(vals))
	for i, v := range vals {
		client.PutUint32(b[i*4:], v)
	}
	return b
}

func readString(data []byte) string {
	l := client.Uint32(data[0:4])
	if int(4+l) > len(data) {
		l = uint32(len(data) - 4)
	}
	return client.String(data[4 : 4+int(l)])
}

// --- protocol objects ---

// Manager is zwlr_output_manager_v1.
type Manager struct {
	client.BaseProxy
	owner *Client
}

func newManager(c *Client) *Manager {
	m := &Manager{owner: c}
	c.ctx.Register(m)
	return m
}

// Stop stops the OpenGL ES and emits a finished event.
func (m *Manager) Stop() error { return m.owner.send(m.ID(), 1, nil) }

// Dispatch implements client.Dispatcher.
func (m *Manager) Dispatch(opcode uint32, _ int, data []byte) {
	switch opcode {
	case 0: // head
		h := newHead(m.owner, client.Uint32(data[0:4]))
		m.owner.heads = append(m.owner.heads, h)
	case 1: // done
		m.owner.serial = client.Uint32(data[0:4])
	case 2: // finished
		m.owner.manager = nil
	}
}

// Head is zwlr_output_head_v1.
type Head struct {
	client.BaseProxy
	owner *Client

	name        string
	description string
	make        string
	model       string
	serial      string
	enabled     bool
	posX, posY  int
	transform   int
	scale       float64
	current     *outputMode
	modes       []*outputMode
	finished    bool
}

func newHead(c *Client, id uint32) *Head {
	h := &Head{owner: c}
	h.SetID(id)
	h.SetContext(c.ctx)
	c.add(h)
	return h
}

// Release asks the compositor to release the head object.
func (h *Head) Release() error { return h.owner.send(h.ID(), 0, nil) }

// Dispatch implements client.Dispatcher.
func (h *Head) Dispatch(opcode uint32, _ int, data []byte) {
	switch opcode {
	case 0:
		h.name = readString(data)
	case 1:
		h.description = readString(data)
	case 2: // physical_size (mm), unused
	case 3: // mode
		m := newMode(h.owner, client.Uint32(data[0:4]))
		h.modes = append(h.modes, m)
	case 4: // enabled
		h.enabled = client.Uint32(data[0:4]) != 0
	case 5: // current_mode
		if m, ok := h.owner.objects[client.Uint32(data[0:4])].(*outputMode); ok {
			h.current = m
		}
	case 6: // position
		h.posX = int(int32(client.Uint32(data[0:4])))
		h.posY = int(int32(client.Uint32(data[4:8])))
	case 7: // transform
		h.transform = int(int32(client.Uint32(data[0:4])))
	case 8: // scale
		h.scale = client.Fixed(data[0:4])
	case 9: // finished
		h.finished = true
	case 10:
		h.make = readString(data)
	case 11:
		h.model = readString(data)
	case 12:
		h.serial = readString(data)
	}
}

// outputMode is zwlr_output_mode_v1.
type outputMode struct {
	client.BaseProxy
	owner *Client

	width     int
	height    int
	refresh   int
	preferred bool
	finished  bool
}

func newMode(c *Client, id uint32) *outputMode {
	m := &outputMode{owner: c}
	m.SetID(id)
	m.SetContext(c.ctx)
	c.add(m)
	return m
}

// Release asks the compositor to release the mode object.
func (m *outputMode) Release() error { return m.owner.send(m.ID(), 0, nil) }

// Dispatch implements client.Dispatcher.
func (m *outputMode) Dispatch(opcode uint32, _ int, data []byte) {
	switch opcode {
	case 0: // size
		m.width = int(int32(client.Uint32(data[0:4])))
		m.height = int(int32(client.Uint32(data[4:8])))
	case 1: // refresh
		m.refresh = int(int32(client.Uint32(data[0:4])))
	case 2: // preferred
		m.preferred = true
	case 3: // finished
		m.finished = true
	}
}

// Configuration is zwlr_output_configuration_v1.
type Configuration struct {
	client.BaseProxy
	owner *Client

	done      bool
	succeeded bool
	failed    bool
	cancelled bool
}

func newConfiguration(c *Client) *Configuration {
	cf := &Configuration{owner: c}
	c.ctx.Register(cf)
	_ = c.send(c.manager.ID(), 0, u32(cf.ID(), c.serial))
	return cf
}

func (cf *Configuration) enableHead(cfgHeadID, headID uint32) error {
	return cf.owner.send(cf.ID(), 0, u32(cfgHeadID, headID))
}

func (cf *Configuration) apply() error { return cf.owner.send(cf.ID(), 2, nil) }

func (cf *Configuration) test() error { return cf.owner.send(cf.ID(), 3, nil) }

func (cf *Configuration) destroy() error { return cf.owner.send(cf.ID(), 4, nil) }

// Dispatch implements client.Dispatcher.
func (cf *Configuration) Dispatch(opcode uint32, _ int, _ []byte) {
	switch opcode {
	case 0:
		cf.done, cf.succeeded = true, true
	case 1:
		cf.done, cf.failed = true, true
	case 2:
		cf.done, cf.cancelled = true, true
	}
}

// ConfigurationHead is zwlr_output_configuration_head_v1.
type ConfigurationHead struct {
	client.BaseProxy
	owner *Client
}

func newConfigurationHead(c *Client) *ConfigurationHead {
	ch := &ConfigurationHead{owner: c}
	c.ctx.Register(ch)
	return ch
}

func (ch *ConfigurationHead) setCustomMode(width, height, refresh int) error {
	return ch.owner.send(ch.ID(), 1, u32(uint32(width), uint32(height), uint32(refresh)))
}

func (ch *ConfigurationHead) setPosition(x, y int) error {
	return ch.owner.send(ch.ID(), 2, u32(uint32(x), uint32(y)))
}

func (ch *ConfigurationHead) setTransform(transform int) error {
	return ch.owner.send(ch.ID(), 3, u32(uint32(transform)))
}

func (ch *ConfigurationHead) setScale(scale float64) error {
	b := make([]byte, 4)
	client.PutFixed(b, scale)
	return ch.owner.send(ch.ID(), 4, b)
}

// Dispatch implements client.Dispatcher.
func (ch *ConfigurationHead) Dispatch(uint32, int, []byte) {}
