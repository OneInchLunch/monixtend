package input

import "testing"

func TestKeycode(t *testing.T) {
	cases := map[string]uint32{
		"KeyA":        30,
		"KeyZ":        44,
		"Digit1":      2,
		"Digit0":      11,
		"Enter":       28,
		"Escape":      1,
		"Space":       57,
		"ShiftLeft":   42,
		"ArrowUp":     103,
		"ArrowDown":   108,
		"ControlLeft": 29,
		"NumpadEnter": 96,
		"F12":         88,
	}
	for code, want := range cases {
		got, ok := Keycode(code)
		if !ok {
			t.Errorf("Keycode(%q) not found", code)
			continue
		}
		if got != want {
			t.Errorf("Keycode(%q) = %d, want %d", code, got, want)
		}
	}
	if _, ok := Keycode("NotAKey"); ok {
		t.Error("expected unknown key to be unmapped")
	}
}

func TestKeycodesUnique(t *testing.T) {
	seen := map[uint32]string{}
	for code, kc := range keycodes {
		if prev, dup := seen[kc]; dup {
			t.Errorf("keycode %d mapped by both %q and %q", kc, prev, code)
		}
		seen[kc] = code
	}
}
