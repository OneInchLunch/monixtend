package wlrogm

import (
	"io"
	"testing"

	"github.com/rajveermalviya/go-wayland/wayland/client"
)

// fakeTransport records writes and never produces reads.
type fakeTransport struct{ msgs [][]byte }

func (f *fakeTransport) readMsg() (uint32, uint32, int, []byte, error) {
	return 0, 0, 0, nil, io.EOF
}

func (f *fakeTransport) writeMsg(b, _ []byte) error {
	f.msgs = append(f.msgs, append([]byte(nil), b...))
	return nil
}

func strData(s string) []byte {
	b := make([]byte, 4+client.PaddedLen(len(s)+1))
	client.PutUint32(b[0:4], uint32(len(s)+1))
	copy(b[4:], s)
	return b
}

// newTestClient builds a Client with a manager already bound at id 2, without a
// real Wayland connection.
func newTestClient() (*Client, *Manager) {
	c := &Client{objects: map[uint32]client.Dispatcher{}}
	m := &Manager{owner: c}
	m.SetID(2)
	c.add(m)
	c.manager = m
	return c, m
}

func TestHandleHeadNewIDAndSnapshot(t *testing.T) {
	c, _ := newTestClient()

	// manager.head -> creates head object 0xFF000001
	if err := c.handleEvent(2, 0, u32(0xFF000001)); err != nil {
		t.Fatalf("head event: %v", err)
	}
	if _, ok := c.objects[0xFF000001].(*Head); !ok {
		t.Fatalf("head object not registered")
	}

	// head events
	mustEvent(t, c, 0xFF000001, 0, strData("eDP-1"))
	mustEvent(t, c, 0xFF000001, 4, u32(1))          // enabled
	mustEvent(t, c, 0xFF000001, 3, u32(0xFF000002)) // mode -> object
	mustEvent(t, c, 0xFF000002, 0, u32(1280, 720))  // mode.size
	mustEvent(t, c, 0xFF000002, 1, u32(60000))      // mode.refresh
	mustEvent(t, c, 0xFF000002, 2, nil)             // mode.preferred
	mustEvent(t, c, 0xFF000001, 5, u32(0xFF000002)) // head.current_mode
	mustEvent(t, c, 0xFF000001, 6, u32(1920, 0))    // head.position
	mustEvent(t, c, 0xFF000001, 7, u32(1))          // head.transform 90
	mustEvent(t, c, 0xFF000001, 1, strData("desc")) // description

	outs := c.snapshot()
	if len(outs) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(outs))
	}
	o := outs[0]
	if o.Name != "eDP-1" || !o.Enabled {
		t.Errorf("name/enabled = %q/%v", o.Name, o.Enabled)
	}
	if o.Width != 1280 || o.Height != 720 || o.Refresh != 60000 {
		t.Errorf("mode = %dx%d@%d", o.Width, o.Height, o.Refresh)
	}
	if o.X != 1920 || o.Y != 0 || o.Transform != 1 {
		t.Errorf("geometry = %d,%d transform %d", o.X, o.Y, o.Transform)
	}
	if len(o.Modes) != 1 || !o.Modes[0].Preferred {
		t.Errorf("modes = %+v", o.Modes)
	}
}

func TestManagerDoneAndConfigurationResult(t *testing.T) {
	c, _ := newTestClient()
	mustEvent(t, c, 2, 1, u32(77)) // done(serial)
	if c.serial != 77 {
		t.Errorf("serial = %d, want 77", c.serial)
	}

	cfg := &Configuration{owner: c}
	cfg.SetID(9)
	c.add(cfg)
	mustEvent(t, c, 9, 0, nil)
	if !cfg.done || !cfg.succeeded {
		t.Errorf("configuration success = %v/%v", cfg.done, cfg.succeeded)
	}
}

func TestSendEncoding(t *testing.T) {
	ft := &fakeTransport{}
	c := &Client{tr: ft}
	if err := c.send(7, 2, u32(0x11223344)); err != nil {
		t.Fatalf("send: %v", err)
	}
	got := ft.msgs[0]
	if len(got) != 12 {
		t.Fatalf("message len = %d, want 12", len(got))
	}
	if client.Uint32(got[0:4]) != 7 {
		t.Errorf("object id = %d, want 7", client.Uint32(got[0:4]))
	}
	word := client.Uint32(got[4:8])
	if size := word >> 16; size != 12 {
		t.Errorf("size = %d, want 12", size)
	}
	if opcode := word & 0xffff; opcode != 2 {
		t.Errorf("opcode = %d, want 2", opcode)
	}
	if client.Uint32(got[8:12]) != 0x11223344 {
		t.Errorf("arg = %#x", client.Uint32(got[8:12]))
	}
}

func TestBindEncoding(t *testing.T) {
	ft := &fakeTransport{}
	c := &Client{tr: ft}
	if err := c.bind(2, 5, ManagerName, 1, 9); err != nil {
		t.Fatalf("bind: %v", err)
	}
	got := ft.msgs[0]
	if client.Uint32(got[0:4]) != 2 || client.Uint32(got[4:8])>>16 != uint32(len(got)) {
		t.Fatalf("bind header wrong: % x", got)
	}
	if client.Uint32(got[8:12]) != 5 {
		t.Errorf("name = %d, want 5", client.Uint32(got[8:12]))
	}
	if l := client.Uint32(got[12:16]); l != uint32(len(ManagerName)+1) {
		t.Errorf("iface length = %d, want %d (must include NUL)", l, len(ManagerName)+1)
	}
	if got := client.String(got[16 : 16+len(ManagerName)+1]); got != ManagerName {
		t.Errorf("iface = %q", got)
	}
	tail := got[16+client.PaddedLen(len(ManagerName)+1):]
	if client.Uint32(tail[0:4]) != 1 || client.Uint32(tail[4:8]) != 9 {
		t.Errorf("version/id = %d/%d, want 1/9", client.Uint32(tail[0:4]), client.Uint32(tail[4:8]))
	}
}

func TestConfigurationHeadSetters(t *testing.T) {
	ft := &fakeTransport{}
	c := &Client{tr: ft}
	ch := &ConfigurationHead{owner: c}
	ch.SetID(11)

	if err := ch.setCustomMode(1280, 720, 60); err != nil {
		t.Fatal(err)
	}
	if err := ch.setPosition(1920, 0); err != nil {
		t.Fatal(err)
	}
	if err := ch.setScale(1.5); err != nil {
		t.Fatal(err)
	}
	if len(ft.msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(ft.msgs))
	}
	if op := client.Uint32(ft.msgs[0][4:8]) & 0xffff; op != 1 {
		t.Errorf("custom mode opcode = %d, want 1", op)
	}
	if client.Uint32(ft.msgs[0][8:12]) != 1280 || client.Uint32(ft.msgs[0][12:16]) != 720 {
		t.Errorf("custom mode args wrong: % x", ft.msgs[0])
	}
	if client.Fixed(ft.msgs[2][8:12]) != 1.5 {
		t.Errorf("scale = %v, want 1.5", client.Fixed(ft.msgs[2][8:12]))
	}
}

func mustEvent(t *testing.T, c *Client, sender, opcode uint32, data []byte) {
	t.Helper()
	if err := c.handleEvent(sender, opcode, data); err != nil {
		t.Fatalf("event(%d, %d): %v", sender, opcode, err)
	}
}
