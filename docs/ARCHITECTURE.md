# Architecture

monixtend is a host-side daemon (Go) plus a zero-install browser client. The
host layer is structured around a small number of interfaces so new
environments can be added without touching the streaming or web stack.

## Package map

```
cmd/monixtend          CLI: run, detect, output …, capture
internal/
  config/              flags and runtime configuration
  compositor/          Compositor interface + Hyprland/Sway IPC + generic wlroots
    wlrogm/            self-contained zwlr_output_management_unstable_v1 client
  capture/             Source interface + wlr-screencopy capture (pure Go)
    screencopy/        hand-generated wlr-screencopy protocol bindings
  input/               Injector interface + browser→evdev keycode map
    uinput/            input.Injector over /dev/uinput (pure Go)
  stream/              MJPEG encoder
    h264/              OpenH264 Annex-B encoder (cgo, optional)
  airplay/             UxPlay supervisor (reverse mode; macOS → this monitor)
  server/              HTTP/WS server, session lifecycle, wire protocol
  web/                 embedded browser client (HTML/JS/CSS)
```

## Interfaces

Every environment-specific concern is behind one of four interfaces. Three
compositor backends exist: Hyprland and Sway over their native IPC (both can
create virtual outputs), and a generic `wlr` backend over
`zwlr_output_management_unstable_v1` that enumerates and reconfigures heads but
cannot create them. See `docs/BACKLOG.md` for the rest.

```go
// internal/compositor
type Compositor interface {
    Name() string
    SupportsVirtualOutputs() bool
    Monitors(ctx) ([]Monitor, error)
    CreateOutput(ctx, name, OutputSpec) (string, error) // returns the actual name
    SetGeometry(ctx, name, OutputSpec) error
    RemoveOutput(ctx, name) error
}
// hyprland.go: request socket at
// $XDG_RUNTIME_DIR/hypr/$HYPRLAND_INSTANCE_SIGNATURE/.socket.sock
// sway.go: i3-ipc protocol on $SWAYSOCK (create_output / output ... unplug)
// wlr.go: generic wlroots via wlrogm (enumeration + geometry only)

// internal/capture
type Source interface {
    Size() (width, height int)
    Capture(ctx) (*Frame, error)   // may return ErrNoChange
    Close() error
}
// wayland.go: wlr-screencopy-unstable-v1, SHM buffers, damage tracking

// internal/input
type Injector interface {
    MoveAbs(x, y int) error       // desktop layout coordinates
    Button(b Button, down bool) error
    Scroll(dx, dy float64) error
    Key(code, down) error         // Linux input event code
    Close() error
}
// uinput/: virtual touchpad (absolute) + mouse (buttons/wheel) + keyboard

// internal/stream + internal/server.h264Encoder
frameEncoder interface {          // H.264 only, behind a build tag
    Encode(*capture.Frame, forceKey bool) ([]byte, bool, error)
    Close()
}
```

The MJPEG path is pure Go; the H.264 encoder is cgo/OpenH264 and is behind
`//go:build cgo`, so `CGO_ENABLED=0` builds still work (MJPEG only).

## Wire protocol

Single WebSocket per session (`/ws?token=…`). Text messages are JSON,
binary messages carry encoded frames.

**Client → server**

```jsonc
{ "type": "hello", "width": 1280, "height": 720, "dpr": 2, "h264": true }
{ "type": "pointer", "x": 0.42, "y": 0.71 }      // normalized 0..1
{ "type": "button", "button": 0, "down": true }   // 0=left 1=middle 2=right
{ "type": "wheel", "dx": -0, "dy": 120 }
{ "type": "key", "code": "KeyA", "down": true }   // KeyboardEvent.code
```

**Server → client**

```jsonc
{ "type": "ready", "width": 1280, "height": 720,
  "output": "monixtend-a1b2c3d4", "input": true, "codec": "h264" }
{ "type": "error", "message": "..." }
```

Binary frames:

- **MJPEG**: one JPEG image per message.
- **H.264**: 5-byte header + Annex-B access unit:
  `flags(1) | timestamp_ms(4, little-endian)`; bit 0 of `flags` marks a
  keyframe. The browser configures `VideoDecoder` with
  `avc: { format: "annexb" }`.

Codec negotiation: the client advertises `h264: true` only when it is in a
secure context and has WebCodecs; the server selects H.264 when configured
(`--codec auto` or `--codec h264`) and the client supports it, else MJPEG.

## Session lifecycle

1. `handleWS` validates the token, reads `hello`, clamps the size.
2. `newSession`: computes layout bounds, asks the compositor to place a
   `monixtend-<id>` headless output right of the rightmost monitor (Sway
   assigns its own `HEADLESS-n` name, so the backend's returned name is used),
   opens the capture source, and (when permitted) creates the uinput devices
   sized to the layout.
3. `stream`: a ticker-triggered loop captures a frame, encodes it, and pushes
   it to a per-client queue that drops stale frames under backpressure and
   adapts MJPEG quality. A reader goroutine detects disconnect and cancels the
   session context.
4. `Close` (deferred) closes the capture source, encoder and input devices and
   removes the output.

## Coordinate mapping

The virtual output lives at layout rect `(outX, outY, W, H)` (integer logical
coordinates). Monitor sizes are divided by `scale` to obtain logical size
(`Monitor.LogicalWidth/Height`). The client sends normalized coordinates; the
session converts to layout coordinates (`outX + nx*W`), and the uinput
absolute device maps the whole layout bounding box, so the pointer lands
exactly on the requested pixel regardless of mixed DPI/scale.

## Reverse mode (AirPlay Display)

`monixtend airplay` does not produce video; instead it lets a macOS machine
extend its own desktop onto this host's monitor:

- `internal/airplay` supervises the **UxPlay** receiver subprocess (Bonjour
  advertising, RTSP/RTP negotiation, PIN) and restarts it on crash.
- Before launch it asks the compositor (Hyprland) to borderless-fullscreen the
  receiver window on the chosen output via a dynamic window rule
  (`compositor.KioskManager`); on exit it reloads the config to drop the rule.
  Kiosk pinning is Hyprland-only; on Sway the receiver opens as a normal
  window.
- Because the window appears before any remote client connects, the rule is
  applied ahead of the process start (static fullscreen props only take effect
  at window map time).
- Location: `cmd/monixtend/cmdAirplay`, `internal/airplay/{airplay,args}.go`,
  `KioskPrepare`/`KioskTeardown` in `internal/compositor/hyprland.go`.

## Adding a new backend

1. Implement `compositor.Compositor` (create/configure/remove the virtual
   output for that environment).
2. Implement a `capture.Source` that yields `*capture.Frame` in
   `XRGB8888`/`ARGB8888`.
3. Implement `input.Injector` (or advertise view-only when input is
   impossible).
4. Register detection in `compositor.Detect` and select the capture/input
   implementations in `server.newSession`.
5. Nothing in `stream`, `web`, or the session lifecycle changes.

The `internal/capture/screencopy` bindings were hand-written from
`wlr-screencopy-unstable-v1.xml` in the `go-wayland` generated style, partly
because the upstream module in use encodes `wl_registry.bind` interface names
with the padded length, which compositors reject; `bindGlobal` in
`internal/capture/wayland.go` reimplements the request correctly.

`internal/compositor/wlrogm` is a second hand-written client, for
`zwlr_output_management_unstable_v1`. The `go-wayland` client cannot dispatch
events for objects the server creates via `new_id` (this protocol creates heads
and modes that way), so `wlrogm` keeps its own object table and read loop,
reusing the `client` package only for socket I/O and wire helpers. It binds the
manager directly because the library's `Registry.Bind` has the padded-name bug
above.