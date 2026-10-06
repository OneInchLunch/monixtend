# monixtend

<div style="font-family: sans-serif; font-weight: bold; display: flex; align-items: baseline; gap: 2px;">
  <span> Disclaimer: This product is </span>
  <span style="color: #FF0000; font-size: 1.8rem;">v</span>
  <span style="color: #FF7F00; font-size: 1.1rem;">i</span>
  <span style="color: #FFFF00; font-size: 1.6rem;">b</span>
  <span style="color: #00FF00; font-size: 1.2rem;">e</span>
  <span style="color: #0000FF; font-size: 1.4rem;">-</span>
  <span style="color: #4B0082; font-size: 1.5rem;">c</span>
  <span style="color: #9400D3; font-size: 1.0rem;">o</span>
  <span style="color: #FF007F; font-size: 1.7rem;">d</span>
  <span style="color: #00E5FF; font-size: 1.3rem;">e</span>
  <span style="color: #76FF03; font-size: 1.6rem;">d</span>
  <span> I simply needed something quick so I made this, I make no guaratees for it.</span>
</div>

**Turn a Linux screen into an AirPlay display for your Mac — no dongle, no
Apple TV, no app on the Mac.** Put your Mac's desktop on the laptop screen next
to it, on a spare monitor, or on the TV in the room you're in.

[![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Linux%20%2F%20wlroots-1793D1?logo=linux&logoColor=white)](#compositor-support)
[![AirPlay](https://img.shields.io/badge/AirPlay-via%20UxPlay-000000?logo=apple&logoColor=white)](https://github.com/FDH2/UxPlay)
[![Status](https://img.shields.io/badge/status-alpha-orange)](#status)

monixtend is a small Go daemon that runs on a Linux host and makes it appear in
macOS's **Control Center → Screen Mirroring** list as a display. Pick it, choose
**Extend Display**, and your Mac treats that Linux screen as a real second
monitor. Nothing is installed on the Mac.

It can *also* do the reverse: serve a Linux desktop as a virtual monitor to any
browser on your LAN. See [Browser mode](#browser-mode-a-second-screen-for-any-device).

---

## On the go: Mac extended display on a Linux laptop

Traveling with a Linux laptop? Instead of packing a portable monitor, run
monixtend and use the laptop's own panel as an extra display for your Mac.

```sh
# on the Linux host
monixtend airplay
```

Then on the Mac:

1. **Control Center → Screen Mirroring** → select the host.
2. **Change → Extend Display.**

That's it. H.264 flows over the LAN, and the host renders it on the chosen
output. The Mac's own keyboard and trackpad keep driving it — the Linux side is
a passive display.

**Why it feels good**

- **Zero install on the Mac** — it's built into macOS 12+.
- **Low latency by default** — `-vsync off` and a small jitter buffer tuned for
  a live second screen rather than video playback.
- **Hardware decoding, auto-detected** — VA-API (Intel/AMD/Nouveau), NVDEC
  (NVIDIA), or V4L2 (Raspberry Pi), with a software fallback.
- **Optional PIN** so only your Mac can connect (`-pin 1234`).
- **Optional audio** onto the host's speakers (`-audio`).

### Quick start

```sh
make build
./bin/monixtend airplay                      # advertise on the current monitor
./bin/monixtend airplay -output DP-1 -resolution 2560x1440@60
./bin/monixtend airplay -pin 1234            # require a 4-digit code
```

Requires the [UxPlay](https://github.com/FDH2/UxPlay) receiver on the LAN host —
see [docs/SETUP.md](docs/SETUP.md#reverse-mode-airplay-display) for the
one-time setup (GStreamer plugins, optional low-latency patch).

---

## Supported

| Area | Status | Notes |
| --- | --- | --- |
| AirPlay display receiver (macOS → this host) | Yes | macOS 12+ **Extend Display** and mirroring, via UxPlay |
| AirPlay PIN pairing | Yes | `-pin <4-digit>` |
| AirPlay audio to host | Yes | `-audio` (played on the host's audio sink) |
| Hardware H.264 decode | Yes | Auto-detects VA-API / NVDEC / V4L2; `-hwdec` (on by default) |
| Low-latency smoothing | Yes | `-smoothing-ms` / `-leaky` (needs the patched UxPlay build) |
| Compositors: **Hyprland** | Yes | Virtual outputs **and** fullscreen kiosk pinning |
| Compositors: **Sway** | Yes | Virtual outputs via Sway IPC (no kiosk pinning) |
| Compositors: other **wlroots** (River, niri, labwc, Wayfire) | Partial | Detect + `output geometry` via `zwlr_output_management`; **cannot create** virtual outputs |
| Browser mode (Linux desktop → any browser) | Yes | MJPEG + H.264, QR/token pairing, mouse/keyboard/touch injection |

## Planned

| Area | Status | Notes |
| --- | --- | --- |
| Virtual-output creation on niri / River | Planned | niri `create-virtual-output`, River IPC |
| AirPlay kiosk pinning on non-Hyprland compositors | Planned | Needs a portable fullscreen/window-rule mechanism |
| X11 host support | Planned | Capture (XShm), input (XTest), outputs (Xvfb/RandR) |
| GNOME / KDE Wayland host support | Planned | PipeWire + `xdg-desktop-portal` ScreenCast capture, portal/libei input |
| mDNS discovery | Planned | Today: manual URL / QR / advertised AirPlay name |
| Browser-mode audio, hardware encode, H.264 over plain HTTP, built-in TLS | Planned | See [docs/BACKLOG.md](docs/BACKLOG.md) |

Full list and rationale: [docs/BACKLOG.md](docs/BACKLOG.md).

---

## How the AirPlay mode works

```
   ┌─────────┐   AirPlay (RTSP/RTP, H.264)   ┌──────────────────────────┐
   │   Mac   │ ────────────────────────────► │  monixtend host (Linux)  │
   │ (sender)│                               │  ┌────────────────────┐  │
   └─────────┘                               │  │ UxPlay (subprocess) │  │
                                             │  │  decode → GStreamer │  │
                                             │  └─────────┬──────────┘  │
                                             │            ▼             │
                                             │   fullscreen window on   │
                                             │   the chosen -output     │
                                             └──────────────────────────┘
```

monixtend supervises UxPlay (Bonjour advertising, pairing, RTSP/RTP, decoding)
and pins the receiver window to the monitor you choose. It never links to
UxPlay, only supervises it.

---

## Browser mode: a second screen for any device

The original mode: give a Linux desktop an extra virtual monitor that any
browser on the LAN can view and control.

```sh
monixtend run                 # prints URLs + a QR code
monixtend run --codec auto    # MJPEG, or H.264 when the client supports it
```

- **Extended, not mirrored** — a real headless output placed to the right of
  your monitors.
- **Full interactivity** — pointer, buttons, wheel and keyboard are injected
  into the host; absolute mapping makes a tablet a true touchscreen.
- **Two codecs** — low-latency **MJPEG** (no HTTPS needed) or **H.264** via
  OpenH264 + browser WebCodecs (lower bandwidth; needs a secure context).
- **Pairing** — a token in every URL plus a scannable QR code.

---

## Installation

### 1. Host

monixtend runs on a **wlroots Wayland session**. For AirPlay mode you want
**Hyprland** or **Sway**, which can create the virtual output and pin the
receiver window. Other wlroots compositors work for `detect` / `output
geometry` only (see [Compositor support](#compositor-support)).

### 2. Build tools

| Dependency | Needed for |
| --- | --- |
| **Go** (1.22+; exact minimum in `go.mod`) | building monixtend |
| `git`, a C compiler, `pkg-config` | building monixtend (cgo / OpenH264) |
| `cmake` | building UxPlay from source (optional) |
| **OpenH264** dev headers (`pkg-config openh264`) | H.264 in browser mode (optional; without it the build is MJPEG-only) |

### 3. AirPlay runtime

| Dependency | Why |
| --- | --- |
| [`uxplay`](https://github.com/FDH2/UxPlay) | the AirPlay receiver monixtend supervises |
| GStreamer plugins (`base`, `good`, `bad`, `libav`) | H.264 decode and the `waylandsink` video sink |
| `openh264` (`openh264dec`) | software H.264 decode in UxPlay |
| `avahi-daemon` | Bonjour/mDNS advertising (optional with UxPlay ≥ 1.74, which has built-in mDNS) |

Per-distro (UxPlay ships in the AUR on Arch; use your distro package elsewhere,
or build it from source below):

<details><summary>Arch</summary>

```sh
sudo pacman -S --needed go git base-devel pkgconf cmake \
  gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad \
  gst-plugins-ugly gst-libav avahi openh264
paru -S uxplay                     # or: uxplay-git
sudo systemctl enable --now avahi-daemon
```

</details>

<details><summary>Debian / Ubuntu</summary>

```sh
sudo apt update
sudo apt install -y golang git build-essential pkg-config cmake \
  gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
  gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-libav \
  libopenh264-dev avahi-daemon
sudo apt install -y uxplay || echo "not packaged here — build from source below"
sudo systemctl enable --now avahi-daemon
```

</details>

<details><summary>Fedora</summary>

```sh
sudo dnf install -y golang git gcc gcc-c++ make pkgconf cmake \
  gstreamer1 gstreamer1-plugins-base gstreamer1-plugins-good \
  gstreamer1-plugins-bad-free gstreamer1-plugins-ugly-free gstreamer1-libav \
  openh264 avahi
sudo systemctl enable --now avahi-daemon
```

</details>

<details><summary>Build UxPlay from source</summary>

Needs `cmake`, a C++ compiler, OpenSSL and libplist development headers, and
the GStreamer development packages. Use `-DUSE_DNS_SD=1` only if you want
external Avahi instead of UxPlay's built-in mDNS.

```sh
git clone https://github.com/FDH2/UxPlay.git && cd UxPlay
cmake -B build -DCMAKE_INSTALL_PREFIX=$HOME/.local -DCMAKE_BUILD_TYPE=Release
cmake --build build -j
cmake --install build
# ensure $HOME/.local/bin is on PATH
```

UxPlay requires the GStreamer `libav` plugin at startup even with audio off. If
`gst-libav` is unavailable, the [Setup guide](docs/SETUP.md#reverse-mode-airplay-display)
shows the one-line patch to drop that requirement. The optional
[low-latency patch](docs/SETUP.md#low-latency-smoothing-patch-recommended)
enables `-smoothing-ms` / `-leaky`.

</details>

### 4. Hardware decode (optional)

monixtend auto-detects whatever GStreamer provides and otherwise falls back to
software:

- **Intel / AMD** — VA-API drivers + `gst-plugins-bad` (`vah264dec`).
- **NVIDIA (proprietary)** — the CUDA driver + `nvh264dec` / `glimagesink`
  (`gst-plugins-bad`).
- **Raspberry Pi (4B and older)** — the `bcm2835-codec` kernel module +
  `v4l2h264dec` (`gst-plugins-good`).

### 5. Browser mode extras

- **Input injection** needs `/dev/uinput` writable by your user:

  ```sh
  echo 'KERNEL=="uinput", GROUP="input", MODE="0660"' | \
    sudo tee /etc/udev/rules.d/70-monixtend-uinput.rules
  sudo udevadm control --reload-rules && sudo udevadm trigger
  sudo usermod -aG input "$USER"     # re-login afterwards
  test -w /dev/uinput && echo ok
  ```

- **H.264** additionally needs the OpenH264 dev headers (build-time, §2) and a
  secure context (HTTPS) in the browser; otherwise MJPEG is used.

### 6. Build and install

```sh
git clone https://github.com/lunch/monixtend && cd monixtend
make build            # -> bin/monixtend
make install          # or: go install ./cmd/monixtend
make airplay ARGS="-output DP-1"
make run ARGS="--codec auto --port 9000"
```

Quick smoke test:

```sh
./bin/monixtend detect        # lists the compositor and outputs
./bin/monixtend airplay       # start the AirPlay receiver
```

### Firewall

AirPlay uses mDNS (UDP 5353) plus UxPlay's TCP/UDP media ports; browser mode
listens on the chosen `-port` (default 8777). On the same LAN you usually need
to allow those inbound.

## AirPlay flags

| Flag | Default | Description |
| --- | --- | --- |
| `-name` | `monixtend` | Advertised AirPlay server name |
| `-resolution` | `1920x1080@60` | Requested display size `WxH` or `WxH@R` |
| `-output` | current | Compositor monitor to pin the receiver to |
| `-audio` | off | Also play the Mac's audio on the host |
| `-pin` | none | Require a 4-digit access code |
| `-vsync` | `off` | A/V timestamp sync (`off` = lower latency) |
| `-smoothing-ms` | `100` | Bounded smoothing pre-roll (0 = off) |
| `-leaky` | `on` | Drop stale frames on overflow instead of stalling |
| `-hwdec` | `on` | Auto-detect hardware decoding |
| `-vs` | auto | Override the GStreamer video sink |
| `-uxplay` | `uxplay` | Path to the UxPlay binary |
| `-args` | none | Extra arguments passed through to UxPlay |

## Compositor support

| Backend | Detect | Create output | Geometry | Kiosk pinning |
| --- | :-: | :-: | :-: | :-: |
| Hyprland | Yes | Yes | Yes | Yes |
| Sway | Yes | Yes | Yes | No |
| Generic wlroots | Yes | No | Yes | No |
| GNOME / KDE Wayland | No | No | No | No |
| X11 | No | No | No | No |

## Docs

- [docs/SETUP.md](docs/SETUP.md) — host setup, GStreamer, the low-latency UxPlay patch
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — design and how to add a backend
- [docs/BACKLOG.md](docs/BACKLOG.md) — supported/planned details and rationale

## Status

Alpha (`0.2.0-alpha`). AirPlay display mode and the Hyprland/Sway backends are
in daily use; GNOME/KDE and X11 support are not implemented yet. UxPlay is
GPL-3.0 and is run as a subprocess — monixtend never links to it.
