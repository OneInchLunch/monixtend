# Setup

## Host requirements

- A running **Hyprland** or **Sway** Wayland session.
- `/dev/uinput` writable by your user (for input injection).

### Compositor

monixtend talks to the compositor over its IPC socket, so no extra tools are
needed.

- **Hyprland**: detected via `HYPRLAND_INSTANCE_SIGNATURE`. Creates outputs
  with `output create headless` and configures them with `hl.monitor(...)`.
- **Sway**: detected via `SWAYSOCK` (i3-compatible IPC; `create_output`) —
  creates a `HEADLESS-n` output, configures it with a custom mode, and removes
  it with `output <name> unplug`.
- **Other wlroots compositors** (River, niri, labwc, …): detected when they
  advertise `zwlr_output_management_unstable_v1`. Enumeration and
  `output geometry` work; the protocol cannot create virtual outputs, so use
  Hyprland or Sway for `run`.

### /dev/uinput

Without it you get a view-only session. To enable input injection, add a udev
rule so your user can open the device:

```
# /etc/udev/rules.d/70-monixtend-uinput.rules
KERNEL=="uinput", GROUP="input", MODE="0660"
```

```sh
sudo udevadm control --reload-rules
sudo udevadm trigger
sudo usermod -aG input <youruser>
```

Reboot or re-login, then verify:

```sh
test -w /dev/uinput && echo ok
```

### H.264 encode (optional, but recommended)

```sh
sudo apt install libopenh264-dev     # Debian/Ubuntu
sudo dnf install openh264-devel      # Fedora
```

monixtend pkg-configs OpenH264: `pkg-config --exists openh264`. If it is
missing, the binary still builds and runs with MJPEG; H.264 sessions simply
fall back.

### HTTPS / WebCodecs

H.264 in the browser uses the **WebCodecs API**, which is only available in
secure contexts. Over plain `http://<lan-ip>` the client reports no H.264
support and the server falls back to MJPEG. To use H.264 you need HTTPS; the
current build serves plain HTTP and MJPEG is the recommended default on LANs.
A self-signed TLS option is on the roadmap.

## Run it

```sh
go build ./cmd/monixtend
./monixtend run
```

Pick the URL shown for your LAN IP (or scan the QR). Options:

```sh
./monixtend run --codec auto        # H.264 when the client supports it
./monixtend run --no-input          # view-only
./monixtend run -v                  # debug logs
```

## Running as a user service

```ini
# ~/.config/systemd/user/monixtend.service
[Unit]
Description=monixtend extra display
After=graphical-session.target

[Service]
Type=simple
ExecStart=%h/projects/monixtend/monixtend run
Restart=on-failure

[Install]
WantedBy=default.target
```

```sh
systemctl --user daemon-reload
systemctl --user enable --now monixtend
journalctl --user -u monixtend -f
```

The service must inherit the Wayland session environment
(`WAYLAND_DISPLAY`, `HYPRLAND_INSTANCE_SIGNATURE`, `XDG_RUNTIME_DIR`), which it
does if you normally log into a graphical session.

## Troubleshooting

- **`input injection unavailable`** — `/dev/uinput` not writable, see above.
- **`compositor does not advertise wlr-screencopy`** — the compositor lacks the
  `wlr-screencopy-unstable-v1` protocol (only wlroots compositors offer it).
- **output stays after a crash** — the server removes stale `monixtend-*`
  outputs on startup.
- **cursor misses on a scaled primary** — pointer mapping uses logical layout
  coordinates (scale-aware); a monitor reported as `1920x1080 @1.5` occupies
  `1280x720` of layout space.
- **H.264 shows blank / never selected** — check `-v`; if the client reachable
  over plain HTTP, it will always negotiate MJPEG.

## Reverse mode (AirPlay Display)

Turns this host into a second monitor for a macOS machine (macOS 12+), using
the Mac's stock **Screen Mirroring → Extend Display**. The host merely
receives; nothing is installed on the Mac.

### Dependencies

- The **UxPlay** receiver executable (`uxplay`) on `PATH`.
- GStreamer with the H.264 decoders and a video sink:
  `gstreamer*` base/good/bad, `gst-plugins` with `openh264dec` and
  `waylandsink` (or `ximagesink` as a fallback).
- `avahi-daemon` (Bonjour advertising). Optional with UxPlay ≥ 1.74, which can
  use its built-in mDNS instead.
- Optional hardware decode drivers: VA-API (Intel/AMD), the NVIDIA CUDA driver,
  or the Raspberry Pi `bcm2835-codec` module.

On Arch:

```sh
# install a uxplay package from AUR if available, e.g.:
#   paru -S uxplay  (or uxplay-git)
sudo pacman -S --needed gstreamer gst-plugins-base gst-plugins-good \
  gst-plugins-bad gst-plugins-ugly gst-libav avahi openh264
sudo systemctl enable --now avahi-daemon
```

On Debian/Ubuntu:

```sh
sudo apt install -y \
  gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
  gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-libav \
  avahi-daemon
sudo apt install -y uxplay || echo "not packaged here — build from source below"
sudo systemctl enable --now avahi-daemon
```

On Fedora:

```sh
sudo dnf install -y \
  gstreamer1 gstreamer1-plugins-base gstreamer1-plugins-good \
  gstreamer1-plugins-bad-free gstreamer1-plugins-ugly-free gstreamer1-libav \
  avahi
sudo systemctl enable --now avahi-daemon
```

If your distro does not ship UxPlay, build it from source (no root needed).
This needs `cmake`, a C++ compiler, OpenSSL and libplist development headers,
and the GStreamer development packages (`libgstreamer1.0-dev
libgstreamer-plugins-base1.0-dev` on Debian/Ubuntu, `gstreamer1-devel
gstreamer1-plugins-base-devel` on Fedora). Add `libavahi-compat-libdnssd-dev`
(and `cmake -DUSE_DNS_SD=1`) only if you want external Avahi instead of
UxPlay's built-in mDNS.

```sh
git clone https://github.com/FDH2/UxPlay.git && cd UxPlay
cmake -B build -DCMAKE_INSTALL_PREFIX=$HOME/.local -DCMAKE_BUILD_TYPE=Release
cmake --build build -j
cmake --install build
```

UxPlay requires the GStreamer `libav` plugin at startup even when audio is off;
if you cannot install `gst-libav` (e.g. it does not build against a very new
FFmpeg), patch the requirement out and rebuild:

```sh
# in UxPlay renderers/audio_renderer.c, remove "libav" from the needed[] list
sed -i 's/const gchar \*needed\[\] = { "app", "libav",/const gchar *needed[] = { "app",/' \
  renderers/audio_renderer.c
cmake --build build -j && cmake --install build
```

Video decode is handled by `decodebin`, so `openh264dec` covers H.264 without
`libav`; audio is off by default (`-as 0`).

### Hardware decoding

`monixtend airplay` auto-detects a hardware H.264 decoder by default (`-hwdec`,
default `on`). It probes the GStreamer installation and the GPU, then forwards
the matching pipeline to UxPlay:

- **Intel / AMD / Nouveau (VA-API)** — `-vd vah264dec` (`vaapih264dec` if the
  newer `va` plugin is absent) with the default `waylandsink`.
- **NVIDIA (proprietary)** — `-vd nvh264dec` and `-vs glimagesink`.
- **Raspberry Pi (Pi 4B and older)** — `-vd v4l2h264dec -vc v4l2convert`
  (Pi 5 has no H.264 decoder and falls back to software).

If no matching element is installed, monixtend passes nothing and UxPlay's
`decodebin` falls back to software decoding, so the default never breaks a
host. Use `-hwdec=false` to skip detection entirely, `-vs <sink>` to override
just the video sink (for example `-vs glimagesink` or `-vs ximagesink`), or
`-args` to override any element (for example `-args "-vd omxh264dec"`).

### Low-latency smoothing patch (recommended)

With `-vsync no` (monixtend's default now) the stream is low-latency but
network jitter is visible as choppiness. UxPlay's stock `queue` buffers up to
**1 second**, which causes bursts. Apply this patch so UxPlay accepts
`-latency <ms>` (a bounded smoothing pre-roll) and `-leaky on|off`:

```sh
# renderers/video_renderer.h and .c : add  int queue_ms, bool queue_leaky
#   between "bool h265_support" and "bool coverart_support" in the
#   video_renderer_init signature.
# renderers/video_renderer.c : replace the bare pipeline queue with
#     if (queue_ms > 0) {
#         char *q = g_strdup_printf("queue max-size-time=%d000000%s ! ",
#                                   queue_ms, queue_leaky ? " leaky=downstream" : "");
#         g_string_append(launch, q); g_free(q);
#     } else {
#         g_string_append(launch, "queue ! ");
#     }
# uxplay.cpp : declare   static int video_queue_ms = 100;
#                        static bool video_queue_leaky = true;
#   parse "-latency <ms>" (0..1000) and "-leaky on|off", pass both values into
#   video_renderer_init at every call site.
cmake --build build -j && cmake --install build
```

`-latency n` builds `queue max-size-time=<n ms>` (default `100`); `-leaky on`
drops old frames on overflow instead of stalling. `monixtend airplay`
forwards its `-smoothing-ms` / `-leaky` flags to these options.

### Run it

```sh
./monixtend airplay --resolution 1920x1080@60     # advertise, pin to current monitor
./monixtend airplay --output DP-1 --resolution 2560x1440@60
make run ARGS="airplay -output DP-1"
```

Flags: `-name` (advertised name, default `monixtend`), `-resolution WxH[@R]`,
`-output <monitor>`, `-audio`, `-pin <4-digit>`, `-vsync on|off` (default
`off`), `-smoothing-ms <0..1000>` (default `100`), `-leaky on|off` (default
`on`), `-hwdec on|off` (default `on`), `-vs <sink>` (default: auto-detected),
`-uxplay <path>`, `-args`.

On the Mac: **Control Center → Screen Mirroring → <host> → Change →
Extend Display.**

Tuning: raise `-smoothing-ms` (150–200) if the picture still stutters; lower
it toward 60 until it starts to flicker, then keep the lowest steady value.
If it is perfectly smooth but *delayed*, lower `-smoothing-ms`; you trade ~one
pre-roll window of latency for smoothness.

### How the kiosk pinning works

`airplay` asks the compositor (Hyprland) to borderless-fullscreen the receiver
window on the chosen output before opening it, then removes the rule on exit
(via a compositor config reload). The rule matches the `uxplay` window class;
pass `-args "-vs ximagesink"` if `waylandsink` misbehaves on your setup.

### Notes

- The receiver advertises `_airplay._tcp` over Bonjour; both machines must be
  on the same LAN.
- UxPlay is GPL-3.0. monixtend only **supervises** it as a subprocess (pipe
  IPC), never links to it, so monixtend's license is unaffected.
- `monixtend airplay` and `monixtend run` can coexist on the same host, but
  exiting `airplay` triggers a Hyprland config reload, which also drops any
  live headless outputs from a concurrently running `run` session.