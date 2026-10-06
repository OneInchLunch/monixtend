// Package compositor abstracts the management of virtual outputs on a
// compositor. The first implementation targets wlroots based compositors
// (Hyprland, Sway) via their native IPC sockets, which keeps the host free of
// external command line tools.
package compositor

import (
	"context"
	"errors"
	"math"
)

// ErrUnsupported is returned when the requested operation cannot be served by
// the active compositor.
var ErrUnsupported = errors.New("operation not supported by compositor")

// OutputSpec describes a virtual output to create or reconfigure. A zero
// Width/Height means "use the compositor default". A zero Scale means 1.
type OutputSpec struct {
	Width   int
	Height  int
	Refresh int
	X       int
	Y       int
	Scale   float64
}

// Monitor describes an existing output as reported by the compositor.
type Monitor struct {
	Name        string  `json:"name"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	RefreshRate float64 `json:"refreshRate"`
	X           int     `json:"x"`
	Y           int     `json:"y"`
	Scale       float64 `json:"scale"`
	Transform   int     `json:"transform"`
	Focused     bool    `json:"focused"`
	Disabled    bool    `json:"disabled"`
	MirrorOf    string  `json:"mirrorOf"`
}

// LogicalWidth returns the monitor width in the compositor's logical layout
// space, accounting for fractional scaling.
func (m Monitor) LogicalWidth() int {
	if m.Scale <= 0 {
		return m.Width
	}
	return int(math.Round(float64(m.Width) / m.Scale))
}

// LogicalHeight returns the monitor height in the compositor's logical layout
// space, accounting for fractional scaling.
func (m Monitor) LogicalHeight() int {
	if m.Scale <= 0 {
		return m.Height
	}
	return int(math.Round(float64(m.Height) / m.Scale))
}

// Compositor is the output-management surface required by monixtend.
type Compositor interface {
	// Name identifies the compositor backend, e.g. "hyprland".
	Name() string
	// Monitors returns the currently configured outputs.
	Monitors(ctx context.Context) ([]Monitor, error)
	// CreateOutput creates a virtual (headless) output named name. When spec
	// carries a size the output is configured immediately afterwards.
	CreateOutput(ctx context.Context, name string, spec OutputSpec) error
	// SetGeometry reconfigures an existing output.
	SetGeometry(ctx context.Context, name string, spec OutputSpec) error
	// RemoveOutput destroys a virtual output.
	RemoveOutput(ctx context.Context, name string) error
}

// KioskManager optionally allows forcing an application window fullscreen onto
// a chosen output. It is implemented by compositors that expose dynamic window
// rules (Hyprland).
type KioskManager interface {
	// KioskPrepare arranges for windows whose Wayland/X11 class matches the
	// given class to open borderless and fullscreen on monitor. When monitor is
	// empty the current monitor is kept.
	KioskPrepare(ctx context.Context, monitor, class string) error
	// KioskTeardown removes any rules applied by KioskPrepare.
	KioskTeardown(ctx context.Context) error
}
