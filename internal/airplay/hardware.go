package airplay

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Hardware describes the UxPlay/GStreamer video pipeline chosen for this host.
// Empty fields mean "not forced": UxPlay falls back to decodebin/autovideosink,
// which selects a software decoder when no hardware one is available.
type Hardware struct {
	// Decoder is the GStreamer element passed to UxPlay's "-vd" option.
	Decoder string
	// Converter is the GStreamer element passed to UxPlay's "-vc" option.
	Converter string
	// Sink is the GStreamer element passed to UxPlay's "-vs" option.
	Sink string
}

// DetectHardware inspects the host and returns the preferred hardware H.264
// video pipeline. A decoder is only selected when the matching GStreamer
// element is actually installed, so the result is safe to use by default.
func DetectHardware() Hardware {
	return classify(hwInfo{
		model:       readTrimmed("/proc/device-tree/model"),
		nvidia:      hasNVIDIA(),
		driVendors:  driVendors(),
		pluginAvail: pluginAvailable,
	})
}

// hwInfo is the raw probe result fed to classify. Keeping it a plain struct
// makes the decision logic testable without touching the host.
type hwInfo struct {
	model       string
	nvidia      bool
	driVendors  []string
	pluginAvail func(string) bool
}

// classify picks a pipeline from detected hardware, preferring proprietary
// NVIDIA, then Raspberry Pi v4l2, then VA-API (Intel/AMD/Nouveau).
func classify(info hwInfo) Hardware {
	avail := info.pluginAvail
	if avail == nil {
		avail = func(string) bool { return false }
	}
	if info.nvidia && avail("nvh264dec") {
		return Hardware{Decoder: "nvh264dec", Sink: "glimagesink"}
	}
	if isRaspberryPi(info.model) && !isRaspberryPi5(info.model) && avail("v4l2h264dec") {
		return Hardware{Decoder: "v4l2h264dec", Converter: "v4l2convert", Sink: "waylandsink"}
	}
	if len(info.driVendors) > 0 {
		if avail("vah264dec") {
			return Hardware{Decoder: "vah264dec", Sink: "waylandsink"}
		}
		if avail("vaapih264dec") {
			return Hardware{Decoder: "vaapih264dec", Sink: "waylandsink"}
		}
	}
	return Hardware{}
}

func isRaspberryPi(model string) bool {
	return strings.Contains(model, "Raspberry Pi")
}

func isRaspberryPi5(model string) bool {
	// The Pi 5 has no hardware H.264 decoder.
	return strings.Contains(model, "Raspberry Pi 5")
}

func hasNVIDIA() bool {
	if matches, _ := filepath.Glob("/dev/nvidia[0-9]*"); len(matches) > 0 {
		return true
	}
	if _, err := os.Stat("/dev/nvidiactl"); err == nil {
		return true
	}
	if _, err := os.Stat("/proc/driver/nvidia/version"); err == nil {
		return true
	}
	return false
}

func driVendors() []string {
	paths, _ := filepath.Glob("/sys/class/drm/card*/device/vendor")
	var vendors []string
	for _, p := range paths {
		if v := readTrimmed(p); v != "" {
			vendors = append(vendors, v)
		}
	}
	return vendors
}

func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// pluginAvailable reports whether gst-inspect-1.0 knows the named element. If
// the tool is not installed it returns false, so no decoder is forced.
func pluginAvailable(element string) bool {
	path, err := exec.LookPath("gst-inspect-1.0")
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, path, "--exists", element).Run() == nil
}
