package compositor

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSway is a minimal i3-ipc server for exercising the Sway backend without a
// running compositor.
type fakeSway struct {
	ln   net.Listener
	sock string

	mu       sync.Mutex
	outputs  []swayOutput
	commands []string
}

func newFakeSway(t *testing.T) *fakeSway {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sway.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeSway{
		ln:   ln,
		sock: path,
		outputs: []swayOutput{{
			Name:        "eDP-1",
			Active:      true,
			Focused:     true,
			Scale:       1,
			CurrentMode: &swayMode{Width: 1920, Height: 1080, Refresh: 60000},
			Rect:        swayRect{X: 0, Y: 0, Width: 1920, Height: 1080},
		}},
	}
	go f.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakeSway) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeSway) handle(conn net.Conn) {
	defer conn.Close()
	var hdr [14]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return
	}
	length := binary.LittleEndian.Uint32(hdr[6:10])
	msgType := binary.LittleEndian.Uint32(hdr[10:14])
	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return
	}

	f.mu.Lock()
	f.commands = append(f.commands, string(payload))
	var reply []byte
	switch msgType {
	case swayMsgRunCommand:
		if string(payload) == "create_output" {
			f.outputs = append(f.outputs, swayOutput{
				Name:        "HEADLESS-2",
				Active:      true,
				Scale:       1,
				CurrentMode: &swayMode{Width: 1920, Height: 1080, Refresh: 60000},
				Rect:        swayRect{X: 1920, Y: 0, Width: 1920, Height: 1080},
			})
		}
		reply = []byte(`[{"success":true}]`)
	case swayMsgGetOutputs:
		reply, _ = json.Marshal(f.outputs)
	}
	f.mu.Unlock()

	var out bytes.Buffer
	out.WriteString(swayIPCMagic)
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(len(reply)))
	out.Write(b[:])
	binary.LittleEndian.PutUint32(b[:], msgType)
	out.Write(b[:])
	out.Write(reply)
	_, _ = conn.Write(out.Bytes())
}

func (f *fakeSway) recordedCommands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...)
}

func TestSwayMonitors(t *testing.T) {
	f := newFakeSway(t)
	t.Setenv("SWAYSOCK", f.sock)

	s, err := NewSwayFromEnv()
	if err != nil {
		t.Fatalf("NewSwayFromEnv: %v", err)
	}
	if s.Name() != "sway" {
		t.Errorf("Name() = %q, want sway", s.Name())
	}
	mons, err := s.Monitors(context.Background())
	if err != nil {
		t.Fatalf("Monitors: %v", err)
	}
	if len(mons) != 1 {
		t.Fatalf("got %d monitors, want 1", len(mons))
	}
	m := mons[0]
	if m.Name != "eDP-1" || m.Width != 1920 || m.Height != 1080 || m.RefreshRate != 60 {
		t.Errorf("monitor = %+v, want eDP-1 1920x1080@60", m)
	}
	if !m.Focused || m.Disabled {
		t.Errorf("monitor focus/disabled = %v/%v, want true/false", m.Focused, m.Disabled)
	}
}

func TestSwayCreateAndRemove(t *testing.T) {
	f := newFakeSway(t)
	t.Setenv("SWAYSOCK", f.sock)

	s, err := NewSwayFromEnv()
	if err != nil {
		t.Fatalf("NewSwayFromEnv: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	name, err := s.CreateOutput(ctx, "monixtend-abcd", OutputSpec{
		Width: 1280, Height: 720, Refresh: 60, X: 1920, Y: 0, Scale: 1,
	})
	if err != nil {
		t.Fatalf("CreateOutput: %v", err)
	}
	if name != "HEADLESS-2" {
		t.Errorf("CreateOutput name = %q, want HEADLESS-2", name)
	}

	cmds := f.recordedCommands()
	if !slices.Contains(cmds, "create_output") {
		t.Errorf("expected create_output command, got %q", cmds)
	}
	var configured bool
	for _, c := range cmds {
		if strings.Contains(c, `"HEADLESS-2"`) &&
			strings.Contains(c, "mode --custom 1280x720@60Hz") {
			configured = true
		}
	}
	if !configured {
		t.Errorf("expected geometry command for HEADLESS-2, got %q", cmds)
	}

	if err := s.RemoveOutput(ctx, name); err != nil {
		t.Fatalf("RemoveOutput: %v", err)
	}
	last := f.recordedCommands()
	if got := last[len(last)-1]; got != `output "HEADLESS-2" unplug` {
		t.Errorf("last command = %q, want unplug", got)
	}
}

func TestCheckCommandReply(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"sway success", `[{"success":true}]`, false},
		{"sway failure", `[{"success":false,"error":"boom"}]`, true},
		{"i3 success", `{"success":true}`, false},
		{"i3 failure", `{"success":false}`, true},
		{"empty", ``, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkCommandReply([]byte(tc.body))
			if (err != nil) != tc.wantErr {
				t.Errorf("checkCommandReply(%q) err = %v, wantErr %v", tc.body, err, tc.wantErr)
			}
		})
	}
}

func TestSwayTransform(t *testing.T) {
	cases := map[string]int{
		"normal": 0, "90": 1, "180": 2, "270": 3,
		"flipped": 4, "flipped-90": 5, "flipped-180": 6, "flipped-270": 7,
		"": 0,
	}
	for in, want := range cases {
		if got := swayTransform(in); got != want {
			t.Errorf("swayTransform(%q) = %d, want %d", in, got, want)
		}
	}
}
