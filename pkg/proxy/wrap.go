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

type WrapProxy struct {
	cfg             Config
	pipeline        *Pipeline
	logger          zerolog.Logger
	clientIn        io.Reader
	clientOut       io.Writer
	maxPayload      int64
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

const drainGracePeriod = 10 * time.Second

const childExitGrace = 2 * time.Second

func NewWrapProxy(cfg Config, opts ...Option) *WrapProxy {
	o := applyOptions(opts)
	return &WrapProxy{
		cfg:        cfg,
		pipeline:   o.pipeline,
		logger:     o.logger.With().Str("component", "wrap-proxy").Str("server", cfg.ServerName).Logger(),
		clientIn:   o.clientIn,
		clientOut:  o.clientOut,
		maxPayload: o.maxPayload,
		done:       make(chan struct{}),
	}
}

func (w *WrapProxy) Start(ctx context.Context) error {
	if err := w.spawnChild(ctx); err != nil {
		return fmt.Errorf("spawning child process: %w", err)
	}

	frameLimit := protocol.WithFrameLimit(w.maxPayload)
	w.clientTransport = protocol.NewStdioTransport(w.clientIn, w.clientOut, frameLimit)

	w.serverTransport = protocol.NewStdioTransport(w.childStdout, w.childStdin, frameLimit)

	w.logger.Info().
		Str("command", w.cfg.Command).
		Strs("args", w.cfg.Args).
		Msg("wrap proxy started")

	clientDone := make(chan error, 1)
	serverDone := make(chan error, 1)
	go func() { clientDone <- w.clientToServerLoop(ctx) }()
	go func() { serverDone <- w.serverToClientLoop(ctx) }()
	go func() { _ = w.stderrLoop() }()
	var drainDeadline <-chan time.Time

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
			clientDone = nil
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

func (w *WrapProxy) Stop() error {
	w.stopOnce.Do(func() {
		w.logger.Info().Msg("stopping wrap proxy")

		w.closeChildStdin()

		reaped := w.reapChild()

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

func (w *WrapProxy) spawnChild(ctx context.Context) error {
	w.cmd = exec.CommandContext(ctx, w.cfg.Command, w.cfg.Args...)

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
			if id, ok := msg.GetID(); ok {
				errResp := NewBlockErrorResponse(id, decision.Reason)
				if writeErr := w.clientTransport.WriteMessage(ctx, errResp); writeErr != nil {
					w.logger.Error().Err(writeErr).Msg("failed to send block error response")
				}
			}
			continue
		}

		if processedMsg != nil {
			if err := w.serverTransport.WriteMessage(ctx, processedMsg); err != nil {
				return fmt.Errorf("writing to server: %w", err)
			}
		}
	}
}

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
			if id, ok := msg.GetID(); ok {
				errResp := NewBlockErrorResponse(id, decision.Reason)
				if writeErr := w.clientTransport.WriteMessage(ctx, errResp); writeErr != nil {
					w.logger.Error().Err(writeErr).Msg("failed to send block error response for server message")
				}
			}
			continue
		}

		if processedMsg != nil {
			if err := w.clientTransport.WriteMessage(ctx, processedMsg); err != nil {
				return fmt.Errorf("writing to client: %w", err)
			}
		}
	}
}

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
