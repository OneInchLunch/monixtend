// Package config holds runtime configuration for monixtend.
package config

import (
	"flag"
	"time"
)

// Config holds the tunables that shape a streaming session.
type Config struct {
	// Bind is the interface address the HTTP server listens on.
	Bind string
	// Port is the TCP port the HTTP server listens on.
	Port int
	// Token authenticates clients. Generated when empty.
	Token string
	// OutputName is the base name for the created virtual output.
	OutputName string
	// MaxFPS caps the capture rate.
	MaxFPS int
	// MaxWidth and MaxHeight cap the virtual output size requested by a client.
	MaxWidth  int
	MaxHeight int
	// JPEGQuality is the encoder quality (1-100).
	JPEGQuality int
	// Codec selects the video codec: "mjpeg", "h264" or "auto".
	Codec string
	// Bitrate is the H.264 target bitrate in bits/s (0 derives a default).
	Bitrate int
	// FrameTimeout is how long to wait for a captured frame before retrying.
	FrameTimeout time.Duration
	// DisableInput disables pointer/keyboard injection (view-only).
	DisableInput bool
	// Verbose enables debug logging.
	Verbose bool
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		Bind:         "0.0.0.0",
		Port:         8777,
		OutputName:   "monixtend",
		MaxFPS:       30,
		MaxWidth:     1920,
		MaxHeight:    1200,
		JPEGQuality:  75,
		Codec:        "mjpeg",
		Bitrate:      0,
		FrameTimeout: 500 * time.Millisecond,
	}
}

// RegisterFlags binds the configuration to a flag set.
func (c *Config) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.Bind, "bind", c.Bind, "interface address to listen on")
	fs.IntVar(&c.Port, "port", c.Port, "TCP port to listen on")
	fs.StringVar(&c.Token, "token", c.Token, "authentication token (generated when empty)")
	fs.StringVar(&c.OutputName, "output-name", c.OutputName, "base name for the virtual output")
	fs.IntVar(&c.MaxFPS, "fps", c.MaxFPS, "maximum capture frames per second")
	fs.IntVar(&c.MaxWidth, "max-width", c.MaxWidth, "maximum virtual output width")
	fs.IntVar(&c.MaxHeight, "max-height", c.MaxHeight, "maximum virtual output height")
	fs.IntVar(&c.JPEGQuality, "jpeg-quality", c.JPEGQuality, "JPEG encoder quality (1-100)")
	fs.StringVar(&c.Codec, "codec", c.Codec, "video codec: mjpeg, h264 or auto")
	fs.IntVar(&c.Bitrate, "bitrate", c.Bitrate, "H.264 target bitrate in bits/s (0 = auto)")
	fs.BoolVar(&c.DisableInput, "no-input", c.DisableInput, "disable pointer and keyboard injection")
	fs.BoolVar(&c.Verbose, "v", c.Verbose, "enable debug logging")
}
