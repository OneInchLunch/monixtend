// Command monixtend exposes a device's screen as an extra monitor for another
// device over the network. This entry point currently provides diagnostics and
// manual control of the virtual output lifecycle.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"image/jpeg"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/skip2/go-qrcode"

	"github.com/lunch/monixtend/internal/airplay"
	"github.com/lunch/monixtend/internal/capture"
	"github.com/lunch/monixtend/internal/compositor"
	"github.com/lunch/monixtend/internal/config"
	"github.com/lunch/monixtend/internal/server"
)

// version is stamped at build time via -ldflags "-X main.version=...". The
// default keeps plain `go build`/`go run` informative.
var version = "0.2.0-alpha"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "monixtend:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	switch args[0] {
	case "detect":
		return cmdDetect(ctx, logger, args[1:])
	case "output":
		return cmdOutput(ctx, logger, args[1:])
	case "capture":
		return cmdCapture(ctx, logger, args[1:])
	case "run":
		return cmdRun(ctx, logger, args[1:])
	case "airplay":
		return cmdAirplay(ctx, logger, args[1:])
	case "version", "-version", "--version":
		fmt.Printf("monixtend %s\n", version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `monixtend - use a browser device as an extra monitor

usage:
  monixtend run                          start the host→browser server
  monixtend airplay                      host as AirPlay Display for a Mac
  monixtend version                      print the version
  monixtend detect                       show the detected compositor and outputs
  monixtend output list                  list current outputs
  monixtend output create <name> [WxH]   create a headless output
  monixtend output remove <name>         remove a headless output
  monixtend output geometry <name> WxH[@R] [XxY] [scale]
  monixtend capture <name> [out.jpg]     capture one frame of an output

flags:
`)
	fs := flag.NewFlagSet("monixtend", flag.ContinueOnError)
	cfg := config.Default()
	cfg.RegisterFlags(fs)
	fs.SetOutput(os.Stderr)
	fs.PrintDefaults()
}

func cmdRun(ctx context.Context, logger *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	cfg := config.Default()
	cfg.RegisterFlags(fs)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.Verbose {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	srv, err := server.New(cfg, logger)
	if err != nil {
		return err
	}

	logger.Info("monixtend " + version)

	base := srv.Token()
	ips := localIPs()
	if len(ips) == 0 {
		fmt.Printf("  http://127.0.0.1:%d/?token=%s\n", cfg.Port, base)
	}
	for _, ip := range ips {
		u := fmt.Sprintf("http://%s:%d/?token=%s", ip, cfg.Port, base)
		fmt.Printf("  %s\n", u)
	}
	if len(ips) > 0 {
		printQR(fmt.Sprintf("http://%s:%d/?token=%s", ips[0], cfg.Port, base))
	}
	return srv.Run(ctx)
}

// printQR renders a URL as a scannable QR code in the terminal.
func printQR(content string) {
	q, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return
	}
	fmt.Println()
	fmt.Print(q.ToSmallString(false))
	fmt.Println()
}

// cmdAirplay supervises an AirPlay Display receiver on the configured output,
// turning this host into a second monitor for a macOS machine.
func cmdAirplay(ctx context.Context, logger *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("airplay", flag.ContinueOnError)
	name := fs.String("name", "monixtend", "advertised AirPlay server name")
	res := fs.String("resolution", "1920x1080@60", "requested display resolution WxH or WxH@R")
	output := fs.String("output", "", "compositor monitor to display on (default: current)")
	audio := fs.Bool("audio", false, "also stream audio to the host")
	pin := fs.String("pin", "", "require a 4-digit access code")
	vsync := fs.String("vsync", "off", "A/V timestamp sync: on|off (off = lower latency)")
	smoothing := fs.Int("smoothing-ms", 100, "bounded smoothing pre-roll in ms (0 = off)")
	leaky := fs.String("leaky", "on", "drop old frames when the smoothing buffer overflows: on|off")
	bin := fs.String("uxplay", "uxplay", "path to the uxplay binary")
	extra := fs.String("args", "", "additional arguments passed to uxplay")
	hwdec := fs.Bool("hwdec", true, "auto-detect and use hardware H.264 decoding (disable with -hwdec=false)")
	vsink := fs.String("vs", "", "GStreamer video sink to display on (default: auto-detect)")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}

	videoSync := true
	switch *vsync {
	case "on":
		videoSync = true
	case "off":
		videoSync = false
	default:
		return fmt.Errorf("airplay: -vsync must be 'on' or 'off', got %q", *vsync)
	}
	leakyOn := true
	switch *leaky {
	case "on":
		leakyOn = true
	case "off":
		leakyOn = false
	default:
		return fmt.Errorf("airplay: -leaky must be 'on' or 'off', got %q", *leaky)
	}
	if *smoothing < 0 || *smoothing > 1000 {
		return fmt.Errorf("airplay: -smoothing-ms must be 0..1000, got %d", *smoothing)
	}

	if w, h, r, err := airplay.ParseResolution(*res); err != nil {
		return fmt.Errorf("airplay: %w", err)
	} else {
		logger.Info("airplay receiver", "name", *name, "resolution", fmt.Sprintf("%dx%d@%d", w, h, r))
	}

	var hw airplay.Hardware
	if *hwdec {
		hw = airplay.DetectHardware()
		if hw.Decoder != "" {
			logger.Info("hardware decoding enabled",
				"decoder", hw.Decoder,
				"converter", orDefault(hw.Converter, "(default)"),
				"sink", orDefault(hw.Sink, "waylandsink"))
		} else {
			logger.Info("no hardware H.264 decoder detected; using software decoding")
		}
	}
	sink := hw.Sink
	if *vsink != "" {
		sink = *vsink
	}

	comp, err := compositor.Detect()
	if err != nil {
		logger.Warn("compositor detection failed; kiosk pinning disabled", "err", err)
	}
	if *output != "" && comp != nil {
		if mons, merr := comp.Monitors(ctx); merr == nil {
			found := false
			for _, m := range mons {
				if m.Name == *output {
					found = true
					break
				}
			}
			if !found {
				logger.Warn("output not found; using current monitor", "output", *output)
				*output = ""
			}
		}
	}

	var extraArgs []string
	if *extra != "" {
		extraArgs = strings.Fields(*extra)
	}

	sess := airplay.NewSession(airplay.Options{
		Binary:         *bin,
		Name:           *name,
		Resolution:     *res,
		Output:         *output,
		Audio:          *audio,
		Pin:            *pin,
		VideoSync:      videoSync,
		SmoothingMS:    *smoothing,
		Leaky:          leakyOn,
		VideoDecoder:   hw.Decoder,
		VideoConverter: hw.Converter,
		VideoSink:      sink,
		ExtraArgs:      extraArgs,
	}, comp, logger)

	fmt.Println("monixtend airplay — this host is now an AirPlay Display receiver")
	fmt.Printf("  name:         %s\n", *name)
	fmt.Printf("  target output: %s\n", orDefault(*output, "(current monitor)"))
	fmt.Println("  On your Mac: Control Center → Screen Mirroring → pick this host →")
	fmt.Println("  Change → Extend Display.")
	if *pin != "" {
		fmt.Printf("  The Mac will ask for code %s.\n", *pin)
	}
	logger.Info("starting receiver; press Ctrl-C to stop")
	return sess.Run(ctx)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// localIPs returns non-loopback IPv4 addresses that clients on the LAN can use.
func localIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var ips []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil {
				continue
			}
			ips = append(ips, ip4.String())
		}
	}
	return ips
}

func cmdDetect(ctx context.Context, logger *slog.Logger, _ []string) error {
	comp, err := compositor.Detect()
	if err != nil {
		return err
	}
	mons, err := comp.Monitors(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("compositor: %s\n", comp.Name())
	fmt.Printf("outputs (%d):\n", len(mons))
	for _, m := range mons {
		focus := " "
		if m.Focused {
			focus = "*"
		}
		fmt.Printf("  %s %-12s %4dx%-5d @%6.2fHz  pos %5d,%-5d  scale %.2f\n",
			focus, m.Name, m.Width, m.Height, m.RefreshRate, m.X, m.Y, m.Scale)
	}
	return nil
}

func cmdOutput(ctx context.Context, logger *slog.Logger, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("output: missing subcommand (list|create|remove|geometry)")
	}
	comp, err := compositor.Detect()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		mons, err := comp.Monitors(ctx)
		if err != nil {
			return err
		}
		for _, m := range mons {
			fmt.Printf("%-12s %dx%d @%.2f pos %d,%d scale %.2f\n",
				m.Name, m.Width, m.Height, m.RefreshRate, m.X, m.Y, m.Scale)
		}
		return nil
	case "create":
		if len(args) < 2 {
			return fmt.Errorf("output create: missing name")
		}
		name := args[1]
		spec := compositor.OutputSpec{Refresh: 60, Scale: 1}
		if len(args) >= 3 {
			spec, err = parseSpec(args[2:])
			if err != nil {
				return err
			}
		}
		actual, err := comp.CreateOutput(ctx, name, spec)
		if err != nil {
			return err
		}
		fmt.Printf("created %s\n", actual)
		return nil
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("output remove: missing name")
		}
		if err := comp.RemoveOutput(ctx, args[1]); err != nil {
			return err
		}
		fmt.Printf("removed %s\n", args[1])
		return nil
	case "geometry":
		if len(args) < 3 {
			return fmt.Errorf("output geometry: usage <name> WxH[@R] [XxY] [scale]")
		}
		spec, err := parseSpec(args[2:])
		if err != nil {
			return err
		}
		if err := comp.SetGeometry(ctx, args[1], spec); err != nil {
			return err
		}
		fmt.Printf("reconfigured %s\n", args[1])
		return nil
	default:
		return fmt.Errorf("output: unknown subcommand %q", args[0])
	}
}

func cmdCapture(ctx context.Context, logger *slog.Logger, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("capture: missing output name")
	}
	name := args[0]
	outPath := ""
	if len(args) >= 2 {
		outPath = args[1]
	}

	src, err := capture.NewWayland(name, true)
	if err != nil {
		return err
	}
	defer src.Close()

	frame, err := src.Capture(ctx)
	if err != nil {
		return err
	}
	w, h := src.Size()
	logger.Info("captured frame", "output", name, "width", w, "height", h)

	img := frame.ToRGBA()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return err
	}
	if outPath == "" {
		outPath = "capture.jpg"
	}
	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d bytes, %dx%d)\n", outPath, buf.Len(), w, h)
	return nil
}

// parseSpec parses "WxH[@R] [XxY] [scale]".
func parseSpec(args []string) (compositor.OutputSpec, error) {
	spec := compositor.OutputSpec{Refresh: 60, Scale: 1}
	if len(args) >= 1 {
		w, h, r, err := parseResolution(args[0])
		if err != nil {
			return spec, err
		}
		spec.Width, spec.Height = w, h
		if r > 0 {
			spec.Refresh = r
		}
	}
	if len(args) >= 2 {
		x, y, err := parsePosition(args[1])
		if err != nil {
			return spec, err
		}
		spec.X, spec.Y = x, y
	}
	if len(args) >= 3 {
		s, err := strconv.ParseFloat(args[2], 64)
		if err != nil {
			return spec, fmt.Errorf("invalid scale %q: %w", args[2], err)
		}
		spec.Scale = s
	}
	return spec, nil
}

func parseResolution(s string) (int, int, int, error) {
	refresh := 0
	if i := strings.IndexByte(s, '@'); i >= 0 {
		r, err := strconv.Atoi(s[i+1:])
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid refresh in %q", s)
		}
		refresh = r
		s = s[:i]
	}
	parts := strings.SplitN(s, "x", 2)
	if len(parts) != 2 {
		return 0, 0, 0, fmt.Errorf("invalid resolution %q (want WxH)", s)
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid width in %q", s)
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid height in %q", s)
	}
	return w, h, refresh, nil
}

func parsePosition(s string) (int, int, error) {
	parts := strings.SplitN(s, "x", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid position %q (want XxY)", s)
	}
	x, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid x in %q", s)
	}
	y, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid y in %q", s)
	}
	return x, y, nil
}
