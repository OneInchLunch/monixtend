package wlrogm

import (
	"os"
	"testing"
)

// TestLiveSession is an opt-in integration check against the ambient Wayland
// session. Run with MONIXTEND_WL_PROBE=1; it is skipped by default.
func TestLiveSession(t *testing.T) {
	if os.Getenv("MONIXTEND_WL_PROBE") == "" {
		t.Skip("set MONIXTEND_WL_PROBE=1 to probe the live session")
	}
	c, err := Connect()
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()
	outs, err := c.Monitors()
	if err != nil {
		t.Fatalf("Monitors: %v", err)
	}
	for _, o := range outs {
		t.Logf("%s %dx%d@%d enabled=%v pos=%d,%d scale=%v modes=%d",
			o.Name, o.Width, o.Height, o.Refresh, o.Enabled, o.X, o.Y, o.Scale, len(o.Modes))
	}
}
