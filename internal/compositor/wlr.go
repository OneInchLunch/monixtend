package compositor

import (
	"context"
	"fmt"

	"github.com/lunch/monixtend/internal/compositor/wlrogm"
)

// WLR is a generic wlroots backend built on
// zwlr_output_management_unstable_v1. It can enumerate and reconfigure existing
// heads on any compositor that advertises the protocol.
//
// The protocol has no way to create or destroy heads, so CreateOutput and
// RemoveOutput report ErrUnsupported: creating a virtual output needs
// compositor-specific IPC (Hyprland, Sway, niri, River).
type WLR struct {
	client *wlrogm.Client
}

// NewWLRFromEnv connects to the ambient Wayland session and requires the
// output-management global.
func NewWLRFromEnv() (*WLR, error) {
	c, err := wlrogm.Connect()
	if err != nil {
		return nil, err
	}
	return &WLR{client: c}, nil
}

// Name implements Compositor.
func (w *WLR) Name() string { return "wlr" }

// SupportsVirtualOutputs implements Compositor. The generic protocol cannot
// create heads.
func (w *WLR) SupportsVirtualOutputs() bool { return false }

// Monitors implements Compositor.
func (w *WLR) Monitors(ctx context.Context) ([]Monitor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	outs, err := w.client.Monitors()
	if err != nil {
		return nil, err
	}
	mons := make([]Monitor, 0, len(outs))
	for _, o := range outs {
		mons = append(mons, Monitor{
			Name:        o.Name,
			Width:       o.Width,
			Height:      o.Height,
			RefreshRate: float64(o.Refresh) / 1000,
			X:           o.X,
			Y:           o.Y,
			Scale:       o.Scale,
			Transform:   o.Transform,
			Disabled:    !o.Enabled,
		})
	}
	return mons, nil
}

// CreateOutput implements Compositor. The generic protocol cannot create
// heads.
func (w *WLR) CreateOutput(ctx context.Context, name string, spec OutputSpec) (string, error) {
	_ = ctx
	return "", fmt.Errorf("%w: %s cannot create virtual outputs (use Hyprland/Sway or a compositor-specific backend)",
		ErrUnsupported, wlrogm.ManagerName)
}

// SetGeometry implements Compositor.
func (w *WLR) SetGeometry(ctx context.Context, name string, spec OutputSpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	scale := spec.Scale
	if scale <= 0 {
		scale = 1
	}
	refresh := spec.Refresh
	if refresh <= 0 {
		refresh = 60
	}
	return w.client.SetGeometry(name, spec.Width, spec.Height, refresh, spec.X, spec.Y, scale)
}

// RemoveOutput implements Compositor. The generic protocol cannot destroy
// heads.
func (w *WLR) RemoveOutput(ctx context.Context, name string) error {
	_ = ctx
	return fmt.Errorf("%w: %s cannot remove outputs",
		ErrUnsupported, wlrogm.ManagerName)
}
