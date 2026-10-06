package compositor

import (
	"errors"
	"os"
)

// Detect returns a Compositor for the active session, or an error when the
// environment is not supported yet.
func Detect() (Compositor, error) {
	if os.Getenv(EnvHyprlandSignature) != "" {
		return NewHyprlandFromEnv()
	}
	if os.Getenv("SWAYSOCK") != "" {
		return nil, errors.New("sway backend not implemented yet")
	}
	if os.Getenv("XDG_SESSION_TYPE") == "wayland" {
		return nil, errors.New("unsupported Wayland compositor (only Hyprland is implemented)")
	}
	return nil, errors.New("no Wayland compositor detected (are you in a graphical session?)")
}
