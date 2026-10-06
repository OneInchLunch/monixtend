package compositor

import (
	"errors"
	"os"

	"github.com/lunch/monixtend/internal/compositor/wlrogm"
)

// Detect returns a Compositor for the active session, or an error when the
// environment is not supported yet.
func Detect() (Compositor, error) {
	if os.Getenv(EnvHyprlandSignature) != "" {
		return NewHyprlandFromEnv()
	}
	if os.Getenv("SWAYSOCK") != "" {
		return NewSwayFromEnv()
	}
	// Any wlroots compositor exposing the generic output-management protocol
	// can at least enumerate and reconfigure outputs. Creation still needs a
	// compositor-specific backend.
	if os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland" {
		if w, err := NewWLRFromEnv(); err == nil {
			return w, nil
		}
		return nil, errors.New("unsupported Wayland compositor (supported: Hyprland, Sway, or a wlroots compositor advertising " + wlrogm.ManagerName + ")")
	}
	return nil, errors.New("no Wayland compositor detected (are you in a graphical session?)")
}
