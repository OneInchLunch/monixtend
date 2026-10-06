// Package server exposes the HTTP and WebSocket endpoints that browser clients
// connect to. Each client drives a virtual output and receives an MJPEG stream.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/lunch/monixtend/internal/compositor"
	"github.com/lunch/monixtend/internal/config"
	"github.com/lunch/monixtend/internal/web"
)

// Server owns the HTTP listener and the set of active sessions.
type Server struct {
	cfg   config.Config
	log   *slog.Logger
	comp  compositor.Compositor
	token string

	mu       sync.Mutex
	sessions map[string]*session
}

// New detects the compositor and prepares a server.
func New(cfg config.Config, log *slog.Logger) (*Server, error) {
	comp, err := compositor.Detect()
	if err != nil {
		return nil, err
	}
	token := cfg.Token
	if token == "" {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, err
		}
		token = hex.EncodeToString(b[:])
	}
	return &Server{cfg: cfg, log: log, comp: comp, token: token, sessions: map[string]*session{}}, nil
}

// Token returns the session token clients must present.
func (s *Server) Token() string { return s.token }

// Handler returns the HTTP routing tree.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	mux.Handle("/", web.Handler())
	return mux
}

// Run starts the HTTP server and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	s.cleanupStaleOutputs(ctx)

	addr := net.JoinHostPort(s.cfg.Bind, strconv.Itoa(s.cfg.Port))
	httpSrv := &http.Server{Addr: addr, Handler: s.Handler()}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	s.log.Info("listening", "addr", addr, "token", s.token)
	err := httpSrv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// cleanupStaleOutputs removes leftover virtual outputs from a previous run.
func (s *Server) cleanupStaleOutputs(ctx context.Context) {
	mons, err := s.comp.Monitors(ctx)
	if err != nil {
		return
	}
	prefix := s.cfg.OutputName + "-"
	for _, m := range mons {
		if len(m.Name) >= len(prefix) && m.Name[:len(prefix)] == prefix {
			s.log.Info("removing stale output", "name", m.Name)
			_ = s.comp.RemoveOutput(ctx, m.Name)
		}
	}
}

type helloMessage struct {
	Type   string  `json:"type"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	DPR    float64 `json:"dpr"`
	H264   bool    `json:"h264"`
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("token") != s.token {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		s.log.Warn("websocket accept failed", "err", err)
		return
	}
	defer conn.CloseNow()

	ctx := r.Context()

	typ, data, err := conn.Read(ctx)
	if err != nil {
		return
	}
	if typ != websocket.MessageText {
		_ = conn.Close(websocket.StatusPolicyViolation, "expected hello")
		return
	}
	var hello helloMessage
	if err := json.Unmarshal(data, &hello); err != nil {
		_ = writeText(ctx, conn, map[string]any{"type": "error", "message": "bad hello"})
		_ = conn.Close(websocket.StatusPolicyViolation, "bad hello")
		return
	}

	width, height := s.clampSize(hello.Width, hello.Height)
	codec := s.chooseCodec(hello)
	sess, err := s.newSession(ctx, width, height, codec)
	if err != nil {
		s.log.Error("session setup failed", "err", err)
		_ = writeText(ctx, conn, map[string]any{"type": "error", "message": err.Error()})
		_ = conn.Close(websocket.StatusInternalError, "setup failed")
		return
	}
	defer sess.Close()

	_ = writeText(ctx, conn, map[string]any{
		"type":   "ready",
		"width":  width,
		"height": height,
		"output": sess.outputName,
		"input":  sess.injector != nil,
		"codec":  sess.codec,
	})

	sctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sub := newSubscriber(conn)
	defer sub.close()
	go sub.writeLoop(sctx)

	// The request context is not reliably cancelled when a hijacked websocket
	// peer disconnects, so watch for reads returning an error.
	go func() {
		for {
			typ, data, err := conn.Read(sctx)
			if err != nil {
				cancel()
				return
			}
			sess.handleClientMessage(sctx, typ, data)
		}
	}()

	sess.stream(sctx, sub)
}

// chooseCodec picks the stream codec based on configuration and what the
// client can decode. H.264 requires a WebCodecs-capable client.
func (s *Server) chooseCodec(h helloMessage) string {
	switch s.cfg.Codec {
	case "h264", "auto":
		if h.H264 {
			return "h264"
		}
		return "mjpeg"
	default:
		return "mjpeg"
	}
}

func (s *Server) clampSize(w, h int) (int, int) {
	if w <= 0 || h <= 0 {
		w, h = 1280, 720
	}
	if w > s.cfg.MaxWidth {
		w = s.cfg.MaxWidth
	}
	if h > s.cfg.MaxHeight {
		h = s.cfg.MaxHeight
	}
	if w < 320 {
		w = 320
	}
	if h < 240 {
		h = 240
	}
	return w &^ 1, h &^ 1
}

func writeText(ctx context.Context, conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, data)
}
