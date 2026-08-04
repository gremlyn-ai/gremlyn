package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/rs/zerolog"
)

// WrapProxy implements the Proxy interface for stdio-based MCP servers.
// It spawns the real MCP server as a child process and intercepts all
// JSON-RPC traffic flowing between the MCP client and server.
type WrapProxy struct {
	cfg             Config
	pipeline        *Pipeline
	logger          zerolog.Logger
	timeout         time.Duration
	clientIn        io.Reader
	clientOut       io.Writer
	cmd             *exec.Cmd
	childStdin      io.WriteCloser
	childStdout     io.ReadCloser
	childStderr     io.ReadCloser
	clientTransport *protocol.StdioTransport
	serverTransport *protocol.StdioTransport
	done            chan struct{}
	stopOnce        sync.Once
	stdinCloseOnce  sync.Once
}

// drainGracePeriod bounds how long we keep reading the child's responses after
// the client has stopped sending. A well-behaved MCP server exits once its stdin
// closes; this stops a misbehaving one from hanging the proxy forever.
const drainGracePeriod = 10 * time.Second

// childExitGrace is how long a child gets to exit on its own after its stdin is
// closed, before it is killed.
const childExitGrace = 2 * time.Second

// NewWrapProxy creates a new WrapProxy with the given configuration.
func NewWrapProxy(cfg Config, opts ...Option) *WrapProxy {
	o := applyOptions(opts)
	return &WrapProxy{
		cfg:       cfg,
		pipeline:  o.pipeline,
		logger:    o.logger.With().Str("component", "wrap-proxy").Str("server", cfg.ServerName).Logger(),
		timeout:   o.timeout,
		clientIn:  o.clientIn,
		clientOut: o.clientOut,
		done:      make(chan struct{}),
	}
}

// Start spawns the child MCP server process and begins intercepting traffic.
// It blocks until the context is cancelled, Stop is called, or the child exits.
func (w *WrapProxy) Start(ctx context.Context) error {
	if err := w.spawnChild(ctx); err != nil {
		return fmt.Errorf("spawning child process: %w", err)
	}

	// Create transports for both sides.
	// Client side: our stdin/stdout (MCP client talks to us).
	w.clientTransport = protocol.NewStdioTransport(w.clientIn, w.clientOut)
	// Server side: child's stdout/stdin (we talk to the real MCP server).
	w.serverTransport = protocol.NewStdioTransport(w.childStdout, w.childStdin)

	w.logger.Info().
		Str("command", w.cfg.Command).
		Strs("args", w.cfg.Args).
		Msg("wrap proxy started")

	// The two data loops are NOT interchangeable, and must not share a channel.
	//
	// The client reaching EOF means "no more requests", not "session over": the
	// server may still owe us responses for requests already in flight. Tearing
	// down here would drop them, which is what made `gremlyn wrap` forward a
	// request and never return its answer. So we half-close instead — close the
	// child's stdin to let it finish, then keep draining until it closes its own
	// output.
	//
	// The server closing IS the end of the session.
	//
	// stderr is diagnostic only and must never trigger shutdown.
	clientDone := make(chan error, 1)
	serverDone := make(chan error, 1)

	go func() { clientDone <- w.clientToServerLoop(ctx) }()
	go func() { serverDone <- w.serverToClientLoop(ctx) }()
	go func() { _ = w.stderrLoop() }()

	var drainDeadline <-chan time.Time // nil until the client half-closes

	for {
		select {
		case <-ctx.Done():
			w.logger.Info().Msg("context cancelled, shutting down")
			return w.Stop()

		case err := <-clientDone:
			if err != nil && !errors.Is(err, io.EOF) {
				w.logger.Error().Err(err).Msg("client loop error")
				return w.Stop()
			}
			w.logger.Debug().Msg("client stopped sending, draining server responses")
			w.closeChildStdin()
			clientDone = nil // a nil channel never fires again
			drainDeadline = time.After(drainGracePeriod)

		case err := <-serverDone:
			if err != nil && !errors.Is(err, io.EOF) {
				w.logger.Error().Err(err).Msg("server loop error")
			}
			w.logger.Debug().Msg("server closed connection, shutting down")
			return w.Stop()

		case <-drainDeadline:
			w.logger.Warn().
				Dur("grace", drainGracePeriod).
				Msg("server did not close after client EOF, forcing shutdown")
			return w.Stop()
		}
	}
}

// closeChildStdin signals end-of-input to the child MCP server. Safe to call
// more than once; Stop calls it too.
func (w *WrapProxy) closeChildStdin() {
	w.stdinCloseOnce.Do(func() {
		if w.childStdin == nil {
			return
		}
		if err := w.childStdin.Close(); err != nil {
			w.logger.Debug().Err(err).Msg("closing child stdin")
		}
	})
}

// Stop performs graceful shutdown of the proxy and child process.
//
// Failures during shutdown are logged rather than returned: by this point the
// session is over, and there is nothing a caller could usefully do about a pipe
// that refused to close.
func (w *WrapProxy) Stop() error {
	w.stopOnce.Do(func() {
		w.logger.Info().Msg("stopping wrap proxy")

		// Close the child's stdin first: a well-behaved MCP server exits on its
		// own once its input closes, which lets us avoid killing it outright.
		w.closeChildStdin()

		reaped := w.reapChild()

		// Close transports to unblock any read loop still parked on a read.
		if w.clientTransport != nil {
			if err := w.clientTransport.Close(); err != nil {
				w.logger.Debug().Err(err).Msg("closing client transport")
			}
		}
		if w.serverTransport != nil {
			if err := w.serverTransport.Close(); err != nil {
				w.logger.Debug().Err(err).Msg("closing server transport")
			}
		}

		// cmd.Wait closes the pipes it created, so only close them ourselves if
		// the child was never reaped — otherwise we race Wait's own cleanup.
		if !reaped {
			if w.childStdout != nil {
				if err := w.childStdout.Close(); err != nil {
					w.logger.Debug().Err(err).Msg("closing child stdout")
				}
			}
			if w.childStderr != nil {
				if err := w.childStderr.Close(); err != nil {
					w.logger.Debug().Err(err).Msg("closing child stderr")
				}
			}
		}

		close(w.done)
		w.logger.Info().Msg("wrap proxy stopped")
	})
	return nil
}

// reapChild waits for the child to exit on its own, killing it if it overstays.
// It reports whether cmd.Wait returned, which tells Stop whether the pipes have
// already been cleaned up.
func (w *WrapProxy) reapChild() bool {
	if w.cmd == nil || w.cmd.Process == nil {
		return false
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- w.cmd.Wait() }()

	select {
	case err := <-waitDone:
		if err != nil {
			w.logger.Debug().Err(err).Msg("child process exited")
		}
		return true
	case <-time.After(childExitGrace):
	}

	// Kill on all platforms — Windows has no SIGTERM.
	if err := w.cmd.Process.Kill(); err != nil {
		w.logger.Debug().Err(err).Msg("killing child process")
	}
	select {
	case <-waitDone:
		return true
	case <-time.After(5 * time.Second):
		w.logger.Warn().Msg("child process did not exit within timeout")
		return false
	}
}

// Pipeline returns the analysis pipeline.
func (w *WrapProxy) Pipeline() *Pipeline {
	return w.pipeline
}

// ServerName returns the name of the MCP server.
func (w *WrapProxy) ServerName() string {
	return w.cfg.ServerName
}

// spawnChild creates and starts the child MCP server process.
func (w *WrapProxy) spawnChild(ctx context.Context) error {
	w.cmd = exec.CommandContext(ctx, w.cfg.Command, w.cfg.Args...)

	// Merge environment variables.
	w.cmd.Env = os.Environ()
	for k, v := range w.cfg.Env {
		w.cmd.Env = append(w.cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	var err error
	w.childStdin, err = w.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating stdin pipe: %w", err)
	}

	w.childStdout, err = w.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	w.childStderr, err = w.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := w.cmd.Start(); err != nil {
		return fmt.Errorf("starting child process %q: %w", w.cfg.Command, err)
	}

	w.logger.Info().Int("pid", w.cmd.Process.Pid).Msg("child process started")
	return nil
}

// clientToServerLoop reads messages from the MCP client (our stdin),
// processes them through the pipeline, and forwards them to the child server.
func (w *WrapProxy) clientToServerLoop(ctx context.Context) error {
	for {
		msg, err := w.clientTransport.ReadMessage(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				w.logger.Debug().Msg("client closed connection")
				return io.EOF
			}
			return fmt.Errorf("reading from client: %w", err)
		}

		mctx := &MessageContext{
			ServerName: w.cfg.ServerName,
			Direction:  models.DirectionOutgoing,
			Timestamp:  time.Now(),
		}

		decision, processedMsg, err := w.pipeline.Process(ctx, msg, mctx)
		if err != nil {
			w.logger.Error().Err(err).Msg("pipeline error on outgoing message")
			continue
		}

		if decision.Action == DecisionBlock {
			// Send error response back to client.
			if id, ok := msg.GetID(); ok {
				errResp := NewBlockErrorResponse(id, decision.Reason)
				if writeErr := w.clientTransport.WriteMessage(ctx, errResp); writeErr != nil {
					w.logger.Error().Err(writeErr).Msg("failed to send block error response")
				}
			}
			continue
		}

		// Forward (possibly modified) message to server.
		if processedMsg != nil {
			if err := w.serverTransport.WriteMessage(ctx, processedMsg); err != nil {
				return fmt.Errorf("writing to server: %w", err)
			}
		}
	}
}

// serverToClientLoop reads messages from the child MCP server (child stdout),
// processes them through the pipeline, and forwards them to the MCP client (our stdout).
func (w *WrapProxy) serverToClientLoop(ctx context.Context) error {
	for {
		msg, err := w.serverTransport.ReadMessage(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				w.logger.Debug().Msg("server closed connection")
				return io.EOF
			}
			return fmt.Errorf("reading from server: %w", err)
		}

		mctx := &MessageContext{
			ServerName: w.cfg.ServerName,
			Direction:  models.DirectionIncoming,
			Timestamp:  time.Now(),
		}

		decision, processedMsg, err := w.pipeline.Process(ctx, msg, mctx)
		if err != nil {
			w.logger.Error().Err(err).Msg("pipeline error on incoming message")
			continue
		}

		if decision.Action == DecisionBlock {
			// For blocked responses, send an error response to the client.
			if id, ok := msg.GetID(); ok {
				errResp := NewBlockErrorResponse(id, decision.Reason)
				if writeErr := w.clientTransport.WriteMessage(ctx, errResp); writeErr != nil {
					w.logger.Error().Err(writeErr).Msg("failed to send block error response for server message")
				}
			}
			continue
		}

		// Forward (possibly modified) message to client.
		if processedMsg != nil {
			if err := w.clientTransport.WriteMessage(ctx, processedMsg); err != nil {
				return fmt.Errorf("writing to client: %w", err)
			}
		}
	}
}

// stderrLoop reads stderr from the child process and logs it.
func (w *WrapProxy) stderrLoop() error {
	buf := make([]byte, 4096)
	for {
		n, err := w.childStderr.Read(buf)
		if n > 0 {
			w.logger.Debug().Str("source", "child-stderr").Msg(string(buf[:n]))
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("reading child stderr: %w", err)
		}
	}
}
