// Package input injects pointer and keyboard events into the desktop.
package input

// Button identifies a mouse button.
type Button uint8

const (
	// ButtonLeft is the primary mouse button.
	ButtonLeft Button = iota
	// ButtonMiddle is the middle mouse button.
	ButtonMiddle
	// ButtonRight is the secondary mouse button.
	ButtonRight
)

// Injector forwards input to the host. Coordinates passed to MoveAbs are in
// the desktop layout's coordinate space.
type Injector interface {
	// MoveAbs moves the pointer to an absolute layout position.
	MoveAbs(x, y int) error
	// Button presses or releases a mouse button.
	Button(b Button, down bool) error
	// Scroll scrolls by dx/dy steps; the sign follows browser wheel deltas.
	Scroll(dx, dy float64) error
	// Key presses or releases a key identified by a Linux input event code.
	Key(code uint32, down bool) error
	// Close releases the virtual devices.
	Close() error
}
