"use strict";

const canvas = document.getElementById("screen");
const ctx = canvas.getContext("2d", { alpha: false, desynchronized: true });
const hud = document.getElementById("hud");
const statusEl = document.getElementById("status");
const statsEl = document.getElementById("stats");

const token = new URLSearchParams(location.search).get("token") || "";

let ws = null;
let ready = false;
let inputEnabled = false;
let codec = "mjpeg";
let decoding = false;
let pendingBlob = null;
let retryTimer = null;
let decodeCount = 0;
let lastStats = performance.now();
let decoder = null;

function showStatus(text, isError) {
  statusEl.textContent = text;
  hud.classList.toggle("error", !!isError);
  hud.classList.add("visible");
  clearTimeout(showStatus._t);
  showStatus._t = setTimeout(() => hud.classList.remove("visible"), 4000);
}

function send(obj) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(obj));
  }
}

function desiredSize() {
  const dpr = window.devicePixelRatio || 1;
  let w = Math.round((window.screen?.width || window.innerWidth) * dpr);
  let h = Math.round((window.screen?.height || window.innerHeight) * dpr);
  w = Math.min(w, 1920);
  h = Math.min(h, 1200);
  return { width: w & ~1, height: h & ~1, dpr };
}

function connect() {
  if (ws) {
    try { ws.close(); } catch (_) {}
  }
  teardownDecoder();
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const url = `${proto}://${location.host}/ws?token=${encodeURIComponent(token)}`;
  ws = new WebSocket(url);
  ws.binaryType = "blob";

  ws.onopen = () => {
    showStatus("connected, starting session…");
    const s = desiredSize();
    ws.send(JSON.stringify({
      type: "hello",
      ...s,
      h264: supportsH264(),
      ua: navigator.userAgent,
    }));
  };

  ws.onmessage = (ev) => {
    if (typeof ev.data === "string") {
      handleControl(ev.data);
    } else {
      handleFrame(ev.data);
    }
  };

  ws.onclose = () => {
    ready = false;
    teardownDecoder();
    showStatus("disconnected, retrying…", true);
    scheduleReconnect();
  };

  ws.onerror = () => {};
}

function scheduleReconnect() {
  if (retryTimer) return;
  retryTimer = setTimeout(() => {
    retryTimer = null;
    connect();
  }, 1000);
}

function supportsH264() {
  return window.isSecureContext &&
    typeof VideoDecoder !== "undefined" &&
    typeof EncodedVideoChunk !== "undefined";
}

function h264CodecString() {
  // Baseline profile; level 3.1 (720p) or 4.0 (1080p) or 5.0 for larger.
  const w = canvas.width || 1280;
  const h = canvas.height || 720;
  let level;
  if (w * h <= 1280 * 720) {
    level = 0x1f; // 3.1
  } else if (w * h <= 1920 * 1080) {
    level = 0x28; // 4.0
  } else {
    level = 0x32; // 5.0
  }
  return `avc1.42E0${level.toString(16).padStart(2, "0")}`;
}

function setupDecoder() {
  teardownDecoder();
  decoder = new VideoDecoder({
    output(frame) {
      if (canvas.width !== frame.displayWidth || canvas.height !== frame.displayHeight) {
        canvas.width = frame.displayWidth;
        canvas.height = frame.displayHeight;
        layoutCanvas();
      }
      ctx.drawImage(frame, 0, 0);
      frame.close();
      decodeCount++;
    },
    error(e) {
      showStatus(`decoder error: ${e.message || e}`, true);
    },
  });
  decoder.configure({
    codec: h264CodecString(),
    optimizeForLatency: true,
    avc: { format: "annexb" },
  });
}

function teardownDecoder() {
  if (decoder) {
    try { decoder.close(); } catch (_) {}
    decoder = null;
  }
}

function handleControl(text) {
  let msg;
  try { msg = JSON.parse(text); } catch (_) { return; }
  switch (msg.type) {
    case "ready":
      ready = true;
      inputEnabled = !!msg.input;
      codec = msg.codec || "mjpeg";
      // H.264 is framed manually, so take raw ArrayBuffers and skip the Blob
      // copy per frame; MJPEG hands a Blob straight to createImageBitmap.
      ws.binaryType = codec === "h264" ? "arraybuffer" : "blob";
      canvas.width = msg.width;
      canvas.height = msg.height;
      ctx.fillStyle = "#000";
      ctx.fillRect(0, 0, canvas.width, canvas.height);
      layoutCanvas();
      canvas.style.cursor = inputEnabled ? "none" : "default";
      if (codec === "h264") {
        setupDecoder();
      } else {
        teardownDecoder();
      }
      showStatus(`connected · ${msg.width}×${msg.height} · ${codec.toUpperCase()} · ${msg.output}`);
      break;
    case "error":
      ready = false;
      showStatus(msg.message || "server error", true);
      break;
    default:
      break;
  }
}

// layoutCanvas sizes the element to preserve the output aspect ratio.
function layoutCanvas() {
  if (!canvas.width || !canvas.height) return;
  const availW = window.innerWidth;
  const availH = window.innerHeight;
  const scale = Math.min(availW / canvas.width, availH / canvas.height);
  canvas.style.width = `${Math.floor(canvas.width * scale)}px`;
  canvas.style.height = `${Math.floor(canvas.height * scale)}px`;
}

function handleFrame(data) {
  if (!ready) return;
  if (codec === "h264") {
    decodeH264(data);
    return;
  }
  if (decoding) {
    pendingBlob = data;
    return;
  }
  decodeAndDraw(data);
}

function decodeH264(buffer) {
  if (!decoder || decoder.state !== "configured") return;
  const data = new Uint8Array(buffer);
  const flags = data[0];
  const isKey = (flags & 1) === 1;
  const ts = (data[1] | (data[2] << 8) | (data[3] << 16) | (data[4] << 24)) >>> 0;
  // Drop delta frames when the decoder is falling behind.
  if (!isKey && decoder.decodeQueueSize > 5) return;
  decoder.decode(new EncodedVideoChunk({
    type: isKey ? "key" : "delta",
    timestamp: ts * 1000,
    data: data.subarray(5),
  }));
}

function decodeAndDraw(blob) {
  decoding = true;
  createImageBitmap(blob)
    .then((bmp) => {
      if (canvas.width !== bmp.width || canvas.height !== bmp.height) {
        canvas.width = bmp.width;
        canvas.height = bmp.height;
        layoutCanvas();
      }
      ctx.drawImage(bmp, 0, 0);
      bmp.close();
      decodeCount++;
    })
    .catch(() => {})
    .finally(() => {
      decoding = false;
      if (pendingBlob) {
        const next = pendingBlob;
        pendingBlob = null;
        handleFrame(next);
      }
    });
}

function updateStats() {
  const now = performance.now();
  if (now - lastStats >= 1000) {
    const fps = Math.round((decodeCount * 1000) / (now - lastStats));
    statsEl.textContent = `${fps} fps`;
    decodeCount = 0;
    lastStats = now;
  }
  requestAnimationFrame(updateStats);
}

/* ---- input ---- */

function normalize(clientX, clientY) {
  const r = canvas.getBoundingClientRect();
  if (r.width === 0 || r.height === 0) return null;
  return {
    x: clamp((clientX - r.left) / r.width),
    y: clamp((clientY - r.top) / r.height),
  };
}

function clamp(v) {
  return v < 0 ? 0 : v > 1 ? 1 : v;
}

let pointer = { x: 0.5, y: 0.5 };
let pointerRaf = false;

function queuePointer(x, y) {
  pointer.x = x;
  pointer.y = y;
  if (pointerRaf) return;
  pointerRaf = true;
  requestAnimationFrame(() => {
    pointerRaf = false;
    send({ type: "pointer", x: pointer.x, y: pointer.y });
  });
}

function bindInput() {
  canvas.addEventListener("mousemove", (e) => {
    if (!inputEnabled) return;
    const p = normalize(e.clientX, e.clientY);
    if (p) queuePointer(p.x, p.y);
  });

  canvas.addEventListener("mousedown", (e) => {
    if (!inputEnabled) return;
    e.preventDefault();
    const p = normalize(e.clientX, e.clientY);
    if (p) queuePointer(p.x, p.y);
    send({ type: "button", button: e.button, down: true });
  });

  window.addEventListener("mouseup", (e) => {
    if (!inputEnabled) return;
    send({ type: "button", button: e.button, down: false });
  });

  canvas.addEventListener("contextmenu", (e) => e.preventDefault());

  canvas.addEventListener("wheel", (e) => {
    if (!inputEnabled) return;
    e.preventDefault();
    send({ type: "wheel", dx: e.deltaX, dy: e.deltaY });
  }, { passive: false });

  canvas.addEventListener("touchstart", (e) => {
    if (!inputEnabled) return;
    e.preventDefault();
    const t = e.changedTouches[0];
    const p = normalize(t.clientX, t.clientY);
    if (p) queuePointer(p.x, p.y);
    send({ type: "button", button: 0, down: true });
  }, { passive: false });

  canvas.addEventListener("touchmove", (e) => {
    if (!inputEnabled) return;
    e.preventDefault();
    const t = e.changedTouches[0];
    const p = normalize(t.clientX, t.clientY);
    if (p) queuePointer(p.x, p.y);
  }, { passive: false });

  canvas.addEventListener("touchend", (e) => {
    if (!inputEnabled) return;
    e.preventDefault();
    send({ type: "button", button: 0, down: false });
  }, { passive: false });

  window.addEventListener("keydown", (e) => {
    if (!inputEnabled) return;
    send({ type: "key", code: e.code, down: true });
    if (shouldPreventDefault(e.code)) e.preventDefault();
  });

  window.addEventListener("keyup", (e) => {
    if (!inputEnabled) return;
    send({ type: "key", code: e.code, down: false });
    if (shouldPreventDefault(e.code)) e.preventDefault();
  });
}

function shouldPreventDefault(code) {
  if (code === "Space" || code === "Tab") return true;
  return code.startsWith("Arrow") ||
    code === "PageUp" || code === "PageDown" ||
    code === "Home" || code === "End";
}

canvas.addEventListener("click", () => {
  requestWakeLock();
  if (!document.fullscreenElement) {
    document.documentElement.requestFullscreen?.().catch(() => {});
  }
});

async function requestWakeLock() {
  try {
    if ("wakeLock" in navigator) {
      await navigator.wakeLock.request("screen");
    }
  } catch (_) {}
}

window.addEventListener("resize", layoutCanvas);

if (!token) {
  showStatus("missing token in URL", true);
} else {
  bindInput();
  connect();
  requestWakeLock();
  requestAnimationFrame(updateStats);
}