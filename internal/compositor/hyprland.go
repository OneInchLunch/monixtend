package compositor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// EnvHyprlandSignature is the environment variable Hyprland sets so clients
// can locate its IPC sockets.
const EnvHyprlandSignature = "HYPRLAND_INSTANCE_SIGNATURE"

// Hyprland drives a running Hyprland session over its request socket. It does
// not shell out to hyprctl.
type Hyprland struct {
	socketPath string
}

// NewHyprlandFromEnv builds a Hyprland client from the ambient environment.
func NewHyprlandFromEnv() (*Hyprland, error) {
	sig := os.Getenv(EnvHyprlandSignature)
	if sig == "" {
		return nil, fmt.Errorf("%s not set", EnvHyprlandSignature)
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		return nil, errors.New("XDG_RUNTIME_DIR not set")
	}
	path := filepath.Join(runtimeDir, "hypr", sig, ".socket.sock")
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("hyprland socket %s: %w", path, err)
	}
	return &Hyprland{socketPath: path}, nil
}

// Name implements Compositor.
func (h *Hyprland) Name() string { return "hyprland" }

// SupportsVirtualOutputs implements Compositor.
func (h *Hyprland) SupportsVirtualOutputs() bool { return true }

// request performs a single synchronous round trip with the Hyprland IPC
// socket. The socket is synchronous and single shot, so the write side is
// always closed before reading the reply.
func (h *Hyprland) request(ctx context.Context, msg string) (string, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", h.socketPath)
	if err != nil {
		return "", fmt.Errorf("dial hyprland: %w", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(5 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	if _, err := conn.Write([]byte(msg)); err != nil {
		return "", fmt.Errorf("write hyprland request: %w", err)
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		_ = uc.CloseWrite()
	}

	data, err := io.ReadAll(conn)
	if err != nil {
		return "", fmt.Errorf("read hyprland reply: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// Monitors implements Compositor.
func (h *Hyprland) Monitors(ctx context.Context) ([]Monitor, error) {
	raw, err := h.request(ctx, "j/monitors")
	if err != nil {
		return nil, err
	}
	var mons []Monitor
	if err := json.Unmarshal([]byte(raw), &mons); err != nil {
		return nil, fmt.Errorf("parse hyprland monitors: %w", err)
	}
	return mons, nil
}

// CreateOutput implements Compositor.
func (h *Hyprland) CreateOutput(ctx context.Context, name string, spec OutputSpec) (string, error) {
	if name == "" {
		return "", errors.New("output name must not be empty")
	}
	if err := h.ok(ctx, "output create headless "+name); err != nil {
		return "", fmt.Errorf("create output %q: %w", name, err)
	}
	// The output is registered synchronously, but wait until it is visible so
	// a following geometry call cannot race the monitor appearing.
	if err := h.waitForMonitor(ctx, name, 2*time.Second); err != nil {
		return "", err
	}
	if spec.Width > 0 && spec.Height > 0 {
		if err := h.SetGeometry(ctx, name, spec); err != nil {
			return name, err
		}
	}
	return name, nil
}

// SetGeometry implements Compositor.
func (h *Hyprland) SetGeometry(ctx context.Context, name string, spec OutputSpec) error {
	scale := spec.Scale
	if scale <= 0 {
		scale = 1
	}
	mode := fmt.Sprintf("%dx%d@%d", spec.Width, spec.Height, spec.Refresh)
	position := fmt.Sprintf("%dx%d", spec.X, spec.Y)
	code := fmt.Sprintf("hl.monitor({output=%q, mode=%q, position=%q, scale=%s})",
		name, mode, position, strconv.FormatFloat(scale, 'f', -1, 64))
	if err := h.ok(ctx, "eval "+code); err != nil {
		return fmt.Errorf("configure output %q: %w", name, err)
	}
	return nil
}

// RemoveOutput implements Compositor.
func (h *Hyprland) RemoveOutput(ctx context.Context, name string) error {
	if err := h.ok(ctx, "output remove "+name); err != nil {
		return fmt.Errorf("remove output %q: %w", name, err)
	}
	return nil
}

// ok sends a command and treats any reply other than "ok" as an error.
func (h *Hyprland) ok(ctx context.Context, msg string) error {
	reply, err := h.request(ctx, msg)
	if err != nil {
		return err
	}
	if reply != "ok" {
		return errors.New(reply)
	}
	return nil
}

// Eval executes a Lua snippet through Hyprland's dynamic (Lua) config API.
func (h *Hyprland) Eval(ctx context.Context, code string) error {
	return h.ok(ctx, "eval "+code)
}

// Reload asks Hyprland to reload its configuration, which also drops rules
// applied dynamically at runtime.
func (h *Hyprland) Reload(ctx context.Context) error {
	return h.ok(ctx, "reload")
}

// kioskRuleClass is the window class that KioskPrepare pins. GStreamer-based
// Wayland windows (waylandsink) report the application basename as their class.
const kioskRuleClass = "uxplay"

// KioskPrepare implements compositor.KioskManager for Hyprland.
func (h *Hyprland) KioskPrepare(ctx context.Context, monitor, class string) error {
	if class == "" {
		class = kioskRuleClass
	}
	if monitor != "" {
		code := fmt.Sprintf("hl.dispatch(hl.dsp.focus({ monitor = %q }))", monitor)
		if err := h.Eval(ctx, code); err != nil {
			return fmt.Errorf("focus monitor %q: %w", monitor, err)
		}
	}
	rule := fmt.Sprintf(
		"hl.window_rule({ match = { class = %q }, fullscreen = true, decorate = false, no_anim = true })",
		class)
	if err := h.Eval(ctx, rule); err != nil {
		return fmt.Errorf("apply kiosk window rule: %w", err)
	}
	return nil
}

// KioskTeardown implements compositor.KioskManager for Hyprland.
func (h *Hyprland) KioskTeardown(ctx context.Context) error {
	return h.Reload(ctx)
}

// waitForMonitor polls Monitors until name shows up or the timeout elapses.
func (h *Hyprland) waitForMonitor(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		mons, err := h.Monitors(ctx)
		if err != nil {
			return err
		}
		for _, m := range mons {
			if m.Name == name {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("output %q did not appear within %s", name, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}
