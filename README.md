# monixtend

Turn any browser-capable device on your LAN into an extra monitor for your
Linux desktop.

A small Go daemon runs on the host, creates a **virtual (headless) display**,
streams it over HTTP/WebSocket to a browser, and forwards the browser's
mouse, keyboard and touch back into the desktop. No client app to install —
open a URL (or scan a QR code) on the second device.

## Features

- **Extended, not mirrored**: the virtual output is a real extra workspace to
  the right of your existing monitors (wlroots compositors: Hyprland, Sway).
- **Full interactivity**: pointer, left/middle/right buttons, scroll wheel and
  keyboard are injected into the host (absolute-position mapped, so a tablet
  is a true touchscreen).
- **Reverse mode (macOS 12+)**: run `monixtend airplay` and this host becomes
  an **AirPlay Display** — a macOS machine extends its desktop onto one of your
  monitors via Control Center → Screen Mirroring → **Extend Display**. The host
  is a passive display; no software is installed on the Mac. Powered by the
  [UxPlay](https://github.com/FDH2/UxPlay) receiver.
- **Two codecs**: low-latency **MJPEG** (works everywhere, no HTTPS needed)
  and **H.264** via OpenH264 + browser WebCodecs (much lower bandwidth; needs
  a secure context, i.e. HTTPS).
- **Painless pairing**: a token in every URL plus a scannable QR code.
- **Adaptive quality**: encoder quality adapts to the client's decode speed.
- **Pure Go core**: Wayland wlr-screencopy capture and compositor IPC are
  implemented directly against the protocols — no `grim`, `hyprctl` or
  `ydotool` subprocesses. The AirPlay mode additionally supervises the
  `uxplay` binary as a subprocess.

## Requirements

- Host: Linux with a running **Hyprland** or **Sway** session (wlroots).
- Go 1.22+ to build.
- For H.264: OpenH264 dev headers/library (`pkg-config openh264`) and a cgo
  build (the default).
- Pointer/keyboard injection: write access to `/dev/uinput` (see
  [docs/SETUP.md](docs/SETUP.md)).

## Build and run

```sh
make build                 # builds bin/monixtend (also the default `make`)
make run                   # starts the server with the built binary
```

`make run` accepts extra flags via `ARGS`:

```sh
make run ARGS="--codec auto --port 9000"
```

Alternatively build and run directly:

```sh
go build ./cmd/monixtend
./monixtend run
```

`bin/monixtend` is stamped with `0.2.0-alpha`; override with
`make VERSION=…`, and check it with `monixtend version`.

The terminal prints one or more URLs plus a QR code, e.g.
`http://192.168.2.114:8777/?token=...`. Open one on the second device, tap to
enter fullscreen, and start using the extra screen.

## Reverse mode: macOS extends onto this host

```sh
make run ARGS="airplay"               # or: ./bin/monixtend airplay
make run ARGS="airplay -output DP-1 -resolution 2560x1440@60"
```

The host advertises itself as an AirPlay Display. On your Mac:

1. Control Center → **Screen Mirroring** → pick this host.
2. **Change** → **Extend Display**.

The Mac creates a virtual extended display, H.264 video flows over the LAN,
and this host pins it fullscreen (borderless) onto the chosen `-output`
monitor. Nothing is installed on the Mac, and the host stays a passive display
(control from the Mac's own keyboard/mouse). Requires the `uxplay` receiver —
see [docs/SETUP.md](docs/SETUP.md#reverse-mode-airplay-display).

Hardware H.264 decoding is auto-detected and enabled by default: monixtend
probes the host and passes the matching UxPlay/GStreamer decoder and sink
(`vah264dec` for Intel/AMD/Nouveau, `nvh264dec` + `glimagesink` for NVIDIA,
`v4l2h264dec` + `v4l2convert` on Raspberry Pi). When no hardware decoder is
found it leaves UxPlay to software decoding. Disable the probe and let UxPlay
pick on its own with `-hwdec=false`; override just the sink with
`-vs <sink>` (e.g. `-vs glimagesink`), or any element through `-args`.

`-vsync` defaults to **off** (low latency). If the stream stutters despite low
latency, add a small bounded smoothing pre-roll (default is already 100 ms):

```sh
./bin/monixtend airplay -smoothing-ms 150      # raise if choppy
./bin/monixtend airplay -smoothing-ms 0        # no pre-roll at all
./bin/monixtend airplay -smoothing-ms 150 -leaky off
```
`-leaky on` (default) drops stale frames instead of stalling when the buffer
overflows. These options require the patched UxPlay build from
`docs/SETUP.md`.

## Options (host → browser server)

```
  -bind           interface to listen on (default 0.0.0.0)
  -port           TCP port (default 8777)
  -token          pairing token (generated if empty)
  -codec          mjpeg | h264 | auto (default mjpeg)
  -bitrate        H.264 target bitrate in bits/s (0 = auto)
  -fps            maximum capture rate (default 30)
  -max-width, -max-height   virtual output size cap (default 1920x1200)
  -jpeg-quality   MJPEG quality 1-100 (default 75)
  -no-input       view-only mode
  -v              verbose logging
```

Diagnostics:

```
  monixtend detect                       show compositor and outputs
  monixtend output list|create|remove|geometry ...
  monixtend capture <output> [out.jpg]   capture a single frame
```

## How it works

```
 browser ──HTTP/WS──► server ──Hyprland IPC──► virtual headless output
   ▲                    │                              │
   │      MJPEG / H.264 │                        wlr-screencopy capture
   input◄────────────────┘                              │
                        ▲                              ▼
              pointer/keyboard mapping          encode + push
```

1. The browser reports its native resolution in a `hello` message.
2. The server tells the compositor to create a headless output at that size,
   positioned to the right of the rightmost monitor.
3. A dedicated Wayland client captures that output with `wlr-screencopy`
   (damage-based, switching off when the screen is static).
4. Frames are encoded (MJPEG or H.264) and pushed over the WebSocket.
5. Browser events (`pointer`, `button`, `wheel`, `key`) are mapped from the
   client's normalized coordinates into the desktop layout and injected via
   `/dev/uinput`.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the detailed design and
how to add new environments.

## Status / roadmap

- [x] Skeleton, config, compositor detection, Hyprland IPC (pure Go)
- [x] Virtual output lifecycle (create / resize / remove, crash-safe cleanup)
- [x] wlr-screencopy capture (pure Go) + MJPEG over WebSocket + web client
- [x] uinput pointer/keyboard/touch injection
- [x] Token + QR pairing, wake lock, auto-reconnect, adaptive quality
- [x] H.264 (OpenH264 cgo) + WebCodecs client, `-codec auto` negotiation
- [x] Reverse mode: host as AirPlay Display for macOS (UxPlay supervisor)
- [ ] Sway/i3 compositor backend (kiosk pinning)
- [ ] X11 backend (Xvfb + XTest)
- [ ] mDNS discovery