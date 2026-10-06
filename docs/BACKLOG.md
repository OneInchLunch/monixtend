# Backlog

Deferred work, roughly in priority order. The compositor layer now supports
Hyprland and Sway (see `internal/compositor`); this file records what is not
done yet and why.

## Host platform / backend

### Generic wlroots (River, niri, labwc, Wayfire)

**Enumeration and geometry now work** through `internal/compositor/wlrogm`, a
self-contained client for `zwlr_output_management_unstable_v1` that keeps its
own object table and dispatch loop (the `go-wayland` client used elsewhere
cannot route objects the server creates via `new_id`, which this protocol does
for heads and modes). `monixtend detect` and `output geometry` work on any
compositor advertising the protocol.

**Creating a virtual output is still missing.** The protocol has no create
request; virtual-output creation is compositor-specific (Hyprland `output
create headless`, Sway `create_output`, niri `niri msg action
create-virtual-output`). Next step: add a niri backend over its IPC, then
River.

### GNOME and KDE Wayland

Requires the `xdg-desktop-portal` ScreenCast interface and a PipeWire capture
client (a whole subsystem), plus input injection via the RemoteDesktop portal
or libei. Capture is per-session-consent, not protocol-direct.

### X11

A separate backend: capture via XShm/XComposite, input via XTest, virtual
outputs via Xvfb or RandR. Needs an X11 client library; none is vendored today.

### Input injection

- libei / xdg-desktop-portal `RemoteDesktop` as a portable alternative to
  `/dev/uinput`.
- XTest for the X11 backend.

### Capture

- dmabuf capture (`ext-image-copy-capture` / wlr-screencopy dmabuf) to avoid
  any CPU copy. Only pays off together with a hardware encoder, since the
  current encoders (OpenH264, `image/jpeg`) need CPU-visible pixels.

### Kiosk pinning

`compositor.KioskManager` is Hyprland-only. Sway has no runtime window-rule
equivalent to Hyprland's Lua `window_rule`; a portable approach would use the
`wlr-foreign-toplevel` protocol (not implemented) to find and fullscreen the
receiver window.

## Capabilities

- Audio to the browser client (AirPlay mode already forwards audio to a host
  sink via UxPlay).
- Additional codecs (H.265/AV1) and a WebRTC transport for lower latency.
- Adaptive resolution / bitrate negotiation (only MJPEG quality adapts today).
- Hardware encode (VA-API / NVENC / VideoToolbox / QuickSync).
- DPR-aware output sizing: `hello.dpr` is sent by the client but unused.
- Multi-monitor selection, multiple virtual outputs, sharing one output across
  clients, session persistence.
- Client features: clipboard sync, file transfer, pen/pressure, gamepad, IME,
  full keyboard-layout mapping (only `KeyboardEvent.code` → Linux keycode).
- `-codec h264` silently falls back to MJPEG when the client lacks WebCodecs;
  make an explicit request a hard error or document it.
- AirPlay: expose UxPlay `-h265`, `-hls`, `-reg`/`-allow`/`-block`, PIN
  persistence and a windowed mode; drop the patched-UxPlay dependency for the
  smoothing (`-latency`/`-leaky`) options.

## Security / transport

- Built-in TLS (currently H.264/WebCodecs effectively needs an external TLS
  terminator because it requires a secure context).
- Rotating pairing tokens and per-session auth.
- WebSocket origin check (`InsecureSkipVerify: true` is used today).
- Rate limiting on `/ws`.

## Discovery / UX

- mDNS discovery (manual URL / QR only today).
- Health/metrics endpoint beyond logs and the in-browser FPS counter.

## Engineering quality

- Tests for `capture` and `input` (no test files today); encoder benchmarks.
- H.264 keyframe cadence is controlled twice (OpenH264 `uiIntraPeriod` and
  `ForceIntraFrame` in `session.encodeH264`); pick one.
- De-duplicate the BGRX→RGBA conversion shared by `capture.ToRGBA` and the
  (now replaced) MJPEG path.
- AirPlay kiosk window-class matching is brittle; make it configurable.
