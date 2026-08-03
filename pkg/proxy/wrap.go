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
	cmd             *exec.Cmd
	childStdin      io.WriteCloser
	childStdout     io.ReadCloser
	childStderr     io.ReadCloser
	clientTransport *protocol.StdioTransport
	serverTransport *protocol.StdioTransport
	done            chan struct{}
	stopOnce        sync.Once
}

// NewWrapProxy creates a new WrapProxy with the given configuration.
func NewWrapProxy(cfg Config, opts ...Option) *WrapProxy {
	o := applyOptions(opts)
	return &WrapProxy{
		cfg:      cfg,
		pipeline: o.pipeline,
		logger:   o.logger.With().Str("component", "wrap-proxy").Str("server", cfg.ServerName).Logger(),
		timeout:  o.timeout,
		done:     make(chan struct{}),
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
	w.clientTransport = protocol.NewStdioTransport(os.Stdin, os.Stdout)
	// Server side: child's stdout/stdin (we talk to the real MCP server).
	w.serverTransport = protocol.NewStdioTransport(w.childStdout, w.childStdin)

	w.logger.Info().
		Str("command", w.cfg.Command).
		Strs("args", w.cfg.Args).
		Msg("wrap proxy started")

	// Launch goroutines for bidirectional traffic.
	errCh := make(chan error, 3)

	go func() {
		errCh <- w.clientToServerLoop(ctx)
	}()
	go func() {
		errCh <- w.serverToClientLoop(ctx)
	}()
	go func() {
		errCh <- w.stderrLoop()
	}()

	// Wait for context cancellation, child exit, or error.
	select {
	case <-ctx.Done():
		w.logger.Info().Msg("context cancelled, shutting down")
	case err := <-errCh:
		if err != nil && !errors.Is(err, io.EOF) {
			w.logger.Error().Err(err).Msg("proxy loop error")
		}
	}

	return w.Stop()
}

// Stop performs graceful shutdown of the proxy and child process.
func (w *WrapProxy) Stop() error {
	var stopErr error
	w.stopOnce.Do(func() {
		w.logger.Info().Msg("stopping wrap proxy")

		// Close transports to unblock read loops.
		if w.clientTransport != nil {
			if err := w.clientTransport.Close(); err != nil {
				w.logger.Warn().Err(err).Msg("failed to close client transport")
			}
		}
		if w.serverTransport != nil {
			if err := w.serverTransport.Close(); err != nil {
				w.logger.Warn().Err(err).Msg("failed to close server transport")
			}
		}

		// Terminate the child process.
		if w.cmd != nil && w.cmd.Process != nil {
			// On all platforms, use Kill (Windows has no SIGTERM).
			if err := w.cmd.Process.Kill(); err != nil {
				w.logger.Warn().Err(err).Msg("failed to kill child process")
			}

			// Wait for the child to exit with timeout.
			waitDone := make(chan error, 1)
			go func() {
				waitDone <- w.cmd.Wait()
			}()

			select {
			case err := <-waitDone:
				if err != nil {
					w.logger.Debug().Err(err).Msg("child process exited")
				}
			case <-time.After(5 * time.Second):
				w.logger.Warn().Msg("child process did not exit within timeout")
			}
		}

		// Close pipes.
		if w.childStdin != nil {
			if err := w.childStdin.Close(); err != nil {
				w.logger.Debug().Err(err).Msg("closing child stdin")
			}
		}
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

		close(w.done)
		w.logger.Info().Msg("wrap proxy stopped")
	})
	return stopErr
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
