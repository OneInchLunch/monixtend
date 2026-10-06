package compositor

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Sway drives a running Sway session over its IPC socket ($SWAYSOCK). It does
// not shell out to swaymsg. Sway's virtual outputs are always named HEADLESS-n
// by the compositor (create_output takes no name), so CreateOutput discovers
// and returns the assigned name.
type Sway struct {
	socketPath string
}

// swayIPCMagic is the 6-byte magic that prefixes every i3/Sway IPC message. The
// header is: magic(6) | payload length(uint32) | message type(uint32), with the
// two integers in native byte order.
const swayIPCMagic = "i3-ipc"

const (
	swayMsgRunCommand = 0
	swayMsgGetOutputs = 3
)

// NewSwayFromEnv builds a Sway client from the ambient environment.
func NewSwayFromEnv() (*Sway, error) {
	path := os.Getenv("SWAYSOCK")
	if path == "" {
		return nil, errors.New("SWAYSOCK not set")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("sway socket %s: %w", path, err)
	}
	return &Sway{socketPath: path}, nil
}

// Name implements Compositor.
func (s *Sway) Name() string { return "sway" }

// SupportsVirtualOutputs implements Compositor.
func (s *Sway) SupportsVirtualOutputs() bool { return true }

// request performs one i3-ipc round trip and returns the reply payload. A fresh
// connection is used per call so concurrent sessions do not interleave.
func (s *Sway) request(ctx context.Context, msgType uint32, payload string) ([]byte, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", s.socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial sway: %w", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(5 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	req := make([]byte, 0, 14+len(payload))
	req = append(req, swayIPCMagic...)
	var hdr [8]byte
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(len(payload)))
	binary.LittleEndian.PutUint32(hdr[4:8], msgType)
	req = append(req, hdr[:]...)
	req = append(req, payload...)
	if _, err := conn.Write(req); err != nil {
		return nil, fmt.Errorf("write sway request: %w", err)
	}

	var rhdr [14]byte
	if _, err := io.ReadFull(conn, rhdr[:]); err != nil {
		return nil, fmt.Errorf("read sway reply header: %w", err)
	}
	if string(rhdr[0:6]) != swayIPCMagic {
		return nil, fmt.Errorf("sway: bad reply magic %q", rhdr[0:6])
	}
	length := binary.LittleEndian.Uint32(rhdr[6:10])
	body := make([]byte, length)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, fmt.Errorf("read sway reply: %w", err)
	}
	return body, nil
}

// run executes a Sway command and fails if the reply reports an error.
func (s *Sway) run(ctx context.Context, cmd string) error {
	body, err := s.request(ctx, swayMsgRunCommand, cmd)
	if err != nil {
		return err
	}
	return checkCommandReply(body)
}

// checkCommandReply decodes a RUN_COMMAND reply. Sway returns a JSON array of
// result objects; i3 returns a single object. Either is accepted.
func checkCommandReply(body []byte) error {
	type result struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	fail := func(r result) error {
		if r.Error != "" {
			return errors.New(r.Error)
		}
		return errors.New("sway command failed")
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '[' {
		var results []result
		if err := json.Unmarshal(trimmed, &results); err != nil {
			return fmt.Errorf("parse sway reply: %w", err)
		}
		for _, r := range results {
			if !r.Success {
				return fail(r)
			}
		}
		return nil
	}
	var r result
	if err := json.Unmarshal(trimmed, &r); err != nil {
		return fmt.Errorf("parse sway reply: %w", err)
	}
	if !r.Success {
		return fail(r)
	}
	return nil
}

// swayOutput mirrors the fields of a sway get_outputs entry we care about.
type swayOutput struct {
	Name        string    `json:"name"`
	Active      bool      `json:"active"`
	Primary     bool      `json:"primary"`
	Focused     bool      `json:"focused"`
	CurrentMode *swayMode `json:"current_mode"`
	Rect        swayRect  `json:"rect"`
	Scale       float64   `json:"scale"`
	Transform   string    `json:"transform"`
}

type swayMode struct {
	Width   int `json:"width"`
	Height  int `json:"height"`
	Refresh int `json:"refresh"` // millihertz
}

type swayRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Monitors implements Compositor.
func (s *Sway) Monitors(ctx context.Context) ([]Monitor, error) {
	body, err := s.request(ctx, swayMsgGetOutputs, "")
	if err != nil {
		return nil, err
	}
	var outs []swayOutput
	if err := json.Unmarshal(body, &outs); err != nil {
		return nil, fmt.Errorf("parse sway outputs: %w", err)
	}
	mons := make([]Monitor, 0, len(outs))
	for _, o := range outs {
		m := Monitor{
			Name:      o.Name,
			X:         o.Rect.X,
			Y:         o.Rect.Y,
			Scale:     o.Scale,
			Transform: swayTransform(o.Transform),
			Focused:   o.Focused,
			Disabled:  !o.Active,
		}
		if o.CurrentMode != nil {
			m.Width = o.CurrentMode.Width
			m.Height = o.CurrentMode.Height
			m.RefreshRate = float64(o.CurrentMode.Refresh) / 1000
		} else {
			m.Width = o.Rect.Width
			m.Height = o.Rect.Height
		}
		mons = append(mons, m)
	}
	return mons, nil
}

// CreateOutput implements Compositor.
func (s *Sway) CreateOutput(ctx context.Context, name string, spec OutputSpec) (string, error) {
	before, err := s.outputNames(ctx)
	if err != nil {
		return "", err
	}
	if err := s.run(ctx, "create_output"); err != nil {
		return "", fmt.Errorf("create output: %w", err)
	}
	actual, err := s.waitForNewOutput(ctx, before, 2*time.Second)
	if err != nil {
		return "", err
	}
	if spec.Width > 0 && spec.Height > 0 {
		if err := s.SetGeometry(ctx, actual, spec); err != nil {
			return actual, err
		}
	}
	return actual, nil
}

// SetGeometry implements Compositor. A custom mode is used because the client
// chooses the size and headless outputs accept arbitrary modes.
func (s *Sway) SetGeometry(ctx context.Context, name string, spec OutputSpec) error {
	scale := spec.Scale
	if scale <= 0 {
		scale = 1
	}
	refresh := spec.Refresh
	if refresh <= 0 {
		refresh = 60
	}
	cmd := fmt.Sprintf("output %s mode --custom %dx%d@%dHz position %d %d scale %s",
		swayQuote(name), spec.Width, spec.Height, refresh, spec.X, spec.Y,
		strconv.FormatFloat(scale, 'f', -1, 64))
	if err := s.run(ctx, cmd); err != nil {
		return fmt.Errorf("configure output %q: %w", name, err)
	}
	return nil
}

// RemoveOutput implements Compositor.
func (s *Sway) RemoveOutput(ctx context.Context, name string) error {
	if err := s.run(ctx, "output "+swayQuote(name)+" unplug"); err != nil {
		return fmt.Errorf("remove output %q: %w", name, err)
	}
	return nil
}

// outputNames returns the set of currently known output names.
func (s *Sway) outputNames(ctx context.Context) (map[string]bool, error) {
	mons, err := s.Monitors(ctx)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(mons))
	for _, m := range mons {
		set[m.Name] = true
	}
	return set, nil
}

// waitForNewOutput polls until an output not present in before appears.
func (s *Sway) waitForNewOutput(ctx context.Context, before map[string]bool, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		mons, err := s.Monitors(ctx)
		if err != nil {
			return "", err
		}
		for _, m := range mons {
			if !before[m.Name] {
				return m.Name, nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("sway: new output did not appear within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// swayQuote wraps a name in double quotes so names containing spaces are passed
// as a single argument.
func swayQuote(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `\"`) + `"`
}

// swayTransform maps a sway transform name to the wl_output_transform value.
func swayTransform(name string) int {
	switch name {
	case "90":
		return 1
	case "180":
		return 2
	case "270":
		return 3
	case "flipped":
		return 4
	case "flipped-90":
		return 5
	case "flipped-180":
		return 6
	case "flipped-270":
		return 7
	default:
		return 0
	}
}
