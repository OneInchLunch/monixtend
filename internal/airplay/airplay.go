// Package airplay supervises an AirPlay Display receiver (UxPlay) so that a
// macOS machine can extend its desktop onto this host, and pins the receiver's
// window fullscreen onto the chosen monitor.
package airplay

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/lunch/monixtend/internal/compositor"
)

// Options configures an airplay session.
type Options struct {
	// Binary is the path to the uxplay executable.
	Binary string
	// Name is the AirPlay server name advertised over Bonjour.
	Name string
	// Resolution is "WxH" or "WxH@R" requested from the client.
	Resolution string
	// Output is the compositor monitor to pin the window to ("" = current).
	Output string
	// Audio enables audio streaming to the host.
	Audio bool
	// Pin is an optional 4-digit access code for client connections.
	Pin string
	// VideoSync enables A/V timestamp synchronization. For a passive display it
	// is usually left off ("-vsync no") for lower latency.
	VideoSync bool
	// SmoothingMS is the bounded smoothing pre-roll in milliseconds added to
	// the video pipeline (0 disables it).
	SmoothingMS int
	// Leaky drops old frames when the smoothing buffer overflows instead of
	// stalling. Only meaningful when SmoothingMS > 0.
	Leaky bool
	// VideoDecoder, VideoConverter and VideoSink select UxPlay's GStreamer
	// "-vd"/"-vc"/"-vs" elements. Empty decoder/converter let UxPlay choose
	// (software fallback); an empty sink defaults to waylandsink.
	VideoDecoder   string
	VideoConverter string
	VideoSink      string
	// ExtraArgs are appended to the uxplay command line.
	ExtraArgs []string
}

// Session runs and supervises the UxPlay subprocess.
type Session struct {
	opts Options
	log  *slog.Logger
	comp compositor.Compositor

	mu    sync.Mutex
	cmd   *exec.Cmd
	class string
}

// NewSession prepares a session. comp may be nil, in which case kiosk pinning
// is skipped.
func NewSession(opts Options, comp compositor.Compositor, log *slog.Logger) *Session {
	bin := opts.Binary
	if bin == "" {
		bin = "uxplay"
	}
	opts.Binary = bin
	return &Session{opts: opts, comp: comp, log: log, class: filepath.Base(bin)}
}

// Class returns the window class used for kiosk pinning.
func (s *Session) Class() string { return s.class }

// Run supervises the receiver, restarting it if it exits unexpectedly, until
// ctx is cancelled.
func (s *Session) Run(ctx context.Context) error {
	if km, ok := s.comp.(compositor.KioskManager); ok {
		if err := km.KioskPrepare(ctx, s.opts.Output, s.class); err != nil {
			s.log.Warn("kiosk pinning unavailable", "err", err)
		} else {
			s.log.Info("kiosk window rule applied", "class", s.class, "output", s.opts.Output)
			defer s.teardownKiosk(km)
		}
	}

	for {
		if ctx.Err() != nil {
			return nil
		}
		err := s.runOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			s.log.Warn("uxplay exited", "err", err)
		} else {
			s.log.Info("uxplay exited")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *Session) teardownKiosk(km compositor.KioskManager) {
	cctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := km.KioskTeardown(cctx); err != nil {
		s.log.Debug("kiosk teardown", "err", err)
	} else {
		s.log.Info("kiosk window rule removed")
	}
}

// runOnce starts one uxplay process and waits until it exits or ctx ends.
func (s *Session) runOnce(ctx context.Context) error {
	cmd := exec.Command(s.opts.Binary, BuildArgs(s.opts)...)
	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", s.opts.Binary, err)
	}
	s.log.Info("uxplay started", "binary", s.opts.Binary, "args", cmd.Args[1:])

	scanDone := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			s.log.Info("uxplay: " + sc.Text())
		}
		close(scanDone)
	}()

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-waitCh:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-waitCh
		}
		<-scanDone
		s.mu.Lock()
		s.cmd = nil
		s.mu.Unlock()
		return nil
	case err := <-waitCh:
		<-scanDone
		s.mu.Lock()
		s.cmd = nil
		s.mu.Unlock()
		return err
	}
}

// Stop signals a running subprocess, if any.
func (s *Session) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	return s.cmd.Process.Signal(syscall.SIGTERM)
}

// ErrUnsupportedResolution is returned for malformed resolution strings.
var ErrUnsupportedResolution = errors.New("invalid resolution (want WxH or WxH@R)")
