package airplay

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseResolution parses "WxH" or "WxH@R". A zero refresh is allowed.
func ParseResolution(s string) (width, height, refresh int, err error) {
	if s == "" {
		return 0, 0, 0, fmt.Errorf("%w: empty", ErrUnsupportedResolution)
	}
	refresh = 0
	if i := strings.IndexByte(s, '@'); i >= 0 {
		r, e := strconv.Atoi(s[i+1:])
		if e != nil || r <= 0 {
			return 0, 0, 0, fmt.Errorf("%w: bad refresh in %q", ErrUnsupportedResolution, s)
		}
		refresh = r
		s = s[:i]
	}
	parts := strings.SplitN(s, "x", 2)
	if len(parts) != 2 {
		return 0, 0, 0, fmt.Errorf("%w: %q", ErrUnsupportedResolution, s)
	}
	w, e := strconv.Atoi(parts[0])
	if e != nil || w <= 0 {
		return 0, 0, 0, fmt.Errorf("%w: bad width in %q", ErrUnsupportedResolution, s)
	}
	h, e := strconv.Atoi(parts[1])
	if e != nil || h <= 0 {
		return 0, 0, 0, fmt.Errorf("%w: bad height in %q", ErrUnsupportedResolution, s)
	}
	return w, h, refresh, nil
}

// BuildArgs composes the uxplay command line for options.
func BuildArgs(o Options) []string {
	args := []string{"-n", o.Name}
	res := o.Resolution
	if res == "" {
		res = "1920x1080@60"
	}
	if w, h, r, err := ParseResolution(res); err == nil {
		res = fmt.Sprintf("%dx%d", w, h)
		if r > 0 {
			res += fmt.Sprintf("@%d", r)
		}
	}
	args = append(args, "-s", res)
	if !o.Audio {
		args = append(args, "-as", "0")
	}
	if !o.VideoSync {
		// Default UxPlay behaviour paces video to the A/V clock and adds
		// significant latency. For a passive display "off" renders frames as
		// they arrive.
		args = append(args, "-vsync", "no")
	}
	if o.SmoothingMS > 0 {
		// Bounded pre-roll that absorbs network jitter; dropped instead of
		// stalling when it overflows. Requires the patched UxPlay build.
		args = append(args, "-latency", strconv.Itoa(o.SmoothingMS))
		if o.Leaky {
			args = append(args, "-leaky", "on")
		} else {
			args = append(args, "-leaky", "off")
		}
	}
	if o.Pin != "" {
		args = append(args, "-pin", o.Pin)
	}
	if o.VideoDecoder != "" {
		args = append(args, "-vd", o.VideoDecoder)
	}
	if o.VideoConverter != "" {
		args = append(args, "-vc", o.VideoConverter)
	}
	sink := o.VideoSink
	if sink == "" {
		sink = "waylandsink"
	}
	args = append(args, "-vs", sink)
	args = append(args, o.ExtraArgs...)
	return args
}
