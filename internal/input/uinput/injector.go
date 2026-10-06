//go:build linux

// Package uinput implements input.Injector using the Linux uinput interface.
// It creates three virtual devices: an absolute pointer for positioning, a
// relative mouse for buttons and scrolling, and a keyboard.
package uinput

import (
	"fmt"
	"sync"

	ue "github.com/bendahl/uinput"

	"github.com/lunch/monixtend/internal/input"
)

const devicePath = "/dev/uinput"

// Injector is a uinput-backed input.Injector.
type Injector struct {
	touch ue.TouchPad
	mouse ue.Mouse
	kb    ue.Keyboard

	offX, offY int
	width      int
	height     int

	mu      sync.Mutex
	scrollX float64
	scrollY float64
}

// Options configures the virtual devices.
type Options struct {
	// MinX/MinY are the layout origin; Width/Height the layout extent.
	MinX, MinY    int
	Width, Height int
}

// New creates the virtual devices. A clear error is returned when /dev/uinput
// is not accessible.
func New(opts Options) (*Injector, error) {
	if opts.Width <= 0 || opts.Height <= 0 {
		return nil, fmt.Errorf("uinput: invalid layout size %dx%d", opts.Width, opts.Height)
	}
	kb, err := ue.CreateKeyboard(devicePath, []byte("monixtend-keyboard"))
	if err != nil {
		return nil, fmt.Errorf("uinput: create keyboard: %w", err)
	}
	mouse, err := ue.CreateMouse(devicePath, []byte("monixtend-mouse"))
	if err != nil {
		_ = kb.Close()
		return nil, fmt.Errorf("uinput: create mouse: %w", err)
	}
	touch, err := ue.CreateTouchPad(devicePath, []byte("monixtend-pointer"),
		0, int32(opts.Width-1), 0, int32(opts.Height-1))
	if err != nil {
		_ = mouse.Close()
		_ = kb.Close()
		return nil, fmt.Errorf("uinput: create pointer: %w", err)
	}
	return &Injector{
		touch:  touch,
		mouse:  mouse,
		kb:     kb,
		offX:   opts.MinX,
		offY:   opts.MinY,
		width:  opts.Width,
		height: opts.Height,
	}, nil
}

// MoveAbs implements input.Injector.
func (i *Injector) MoveAbs(x, y int) error {
	px := x - i.offX
	py := y - i.offY
	if px < 0 {
		px = 0
	}
	if py < 0 {
		py = 0
	}
	if px > i.width-1 {
		px = i.width - 1
	}
	if py > i.height-1 {
		py = i.height - 1
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.touch.MoveTo(int32(px), int32(py))
}

// Button implements input.Injector.
func (i *Injector) Button(b input.Button, down bool) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	switch b {
	case input.ButtonLeft:
		if down {
			return i.mouse.LeftPress()
		}
		return i.mouse.LeftRelease()
	case input.ButtonRight:
		if down {
			return i.mouse.RightPress()
		}
		return i.mouse.RightRelease()
	case input.ButtonMiddle:
		if down {
			return i.mouse.MiddlePress()
		}
		return i.mouse.MiddleRelease()
	default:
		return fmt.Errorf("uinput: unknown button %d", b)
	}
}

// Scroll implements input.Injector. Browser wheel deltas are typically ±100
// per notch; they are accumulated and emitted as whole notches.
func (i *Injector) Scroll(dx, dy float64) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.scrollX += dx
	i.scrollY += dy
	const notch = 100.0
	if steps := int(i.scrollY / notch); steps != 0 {
		i.scrollY -= float64(steps) * notch
		// uinput REL_WHEEL is positive for scrolling up; browsers report
		// positive deltaY for scrolling down.
		if err := i.mouse.Wheel(false, int32(-steps)); err != nil {
			return err
		}
	}
	if steps := int(i.scrollX / notch); steps != 0 {
		i.scrollX -= float64(steps) * notch
		if err := i.mouse.Wheel(true, int32(steps)); err != nil {
			return err
		}
	}
	return nil
}

// Key implements input.Injector.
func (i *Injector) Key(code uint32, down bool) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if down {
		return i.kb.KeyDown(int(code))
	}
	return i.kb.KeyUp(int(code))
}

// Close implements input.Injector.
func (i *Injector) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	err1 := i.touch.Close()
	err2 := i.mouse.Close()
	err3 := i.kb.Close()
	if err1 != nil {
		return err1
	}
	if err2 != nil {
		return err2
	}
	return err3
}
