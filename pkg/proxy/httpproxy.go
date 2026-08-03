package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/rs/zerolog"
)

// HTTPProxy implements the Proxy interface for HTTP/SSE-based MCP servers.
// It listens on a local port and reverse-proxies requests to the upstream server.
type HTTPProxy struct {
	cfg      Config
	pipeline *Pipeline
	logger   zerolog.Logger
	timeout  time.Duration
	server   *http.Server
	upstream *url.URL
	client   *http.Client
	parser   *Parser
	done     chan struct{}
	stopOnce sync.Once
}

// NewHTTPProxy creates a new HTTPProxy with the given configuration.
func NewHTTPProxy(cfg Config, opts ...Option) *HTTPProxy {
	o := applyOptions(opts)
	upstream, _ := url.Parse(cfg.UpstreamURL)
	return &HTTPProxy{
		cfg:      cfg,
		pipeline: o.pipeline,
		logger:   o.logger.With().Str("component", "http-proxy").Str("server", cfg.ServerName).Logger(),
		timeout:  o.timeout,
		client:   &http.Client{Timeout: o.timeout},
		parser:   NewParser(),
		upstream: upstream,
		done:     make(chan struct{}),
	}
}

// Start begins listening for requests and proxying to the upstream server.
// Blocks until ctx is cancelled or Stop is called.
func (h *HTTPProxy) Start(ctx context.Context) error {
	if h.upstream == nil {
		return fmt.Errorf("invalid upstream URL %q", h.cfg.UpstreamURL)
	}

	mux := http.NewServeMux()
	pattern := fmt.Sprintf("/mcp/%s", h.cfg.ServerName)
	mux.HandleFunc(pattern, h.handleRequest)
	// Also handle the root path for single-server setups.
	mux.HandleFunc("/", h.handleRequest)

	h.server = &http.Server{
		Addr:    h.cfg.ListenAddr,
		Handler: mux,
	}

	h.logger.Info().
		Str("listen", h.cfg.ListenAddr).
		Str("upstream", h.cfg.UpstreamURL).
		Str("pattern", pattern).
		Msg("http proxy started")

	// Start server in a goroutine.
	errCh := make(chan error, 1)
	go func() {
		if err := h.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		h.logger.Info().Msg("context cancelled, shutting down http proxy")
	case err := <-errCh:
		return fmt.Errorf("http server error: %w", err)
	}

	return h.Stop()
}

// Stop gracefully shuts down the HTTP server.
func (h *HTTPProxy) Stop() error {
	var stopErr error
	h.stopOnce.Do(func() {
		h.logger.Info().Msg("stopping http proxy")
		if h.server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			stopErr = h.server.Shutdown(ctx)
		}
		close(h.done)
		h.logger.Info().Msg("http proxy stopped")
	})
	return stopErr
}

// Pipeline returns the analysis pipeline.
func (h *HTTPProxy) Pipeline() *Pipeline {
	return h.pipeline
}

// ServerName returns the name of the MCP server.
func (h *HTTPProxy) ServerName() string {
	return h.cfg.ServerName
}

// handleRequest processes incoming HTTP requests from MCP clients.
func (h *HTTPProxy) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	defer func() { _ = r.Body.Close() }()
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to read request body")
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}

	// Parse the JSON-RPC message(s).
	msgs, err := h.parser.Parse(body)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to parse JSON-RPC request")
		http.Error(w, "Invalid JSON-RPC", http.StatusBadRequest)
		return
	}

	// Process each message through the pipeline (outgoing direction).
	var forwardMsgs []*protocol.Message
	var blockedResponses []*protocol.Message

	for _, msg := range msgs {
		mctx := &MessageContext{
			ServerName: h.cfg.ServerName,
			Direction:  models.DirectionOutgoing,
			Timestamp:  time.Now(),
		}

		decision, processedMsg, pErr := h.pipeline.Process(r.Context(), msg, mctx)
		if pErr != nil {
			h.logger.Error().Err(pErr).Msg("pipeline error")
			continue
		}

		if decision.Action == DecisionBlock {
			if id, ok := msg.GetID(); ok {
				blockedResponses = append(blockedResponses, NewBlockErrorResponse(id, decision.Reason))
			}
			continue
		}

		if processedMsg != nil {
			forwardMsgs = append(forwardMsgs, processedMsg)
		}
	}

	// If all messages were blocked, return error responses immediately.
	if len(forwardMsgs) == 0 {
		h.writeJSONRPCResponse(w, blockedResponses)
		return
	}

	// Serialize for forwarding.
	var forwardBody []byte
	if len(forwardMsgs) == 1 {
		forwardBody, err = h.parser.Serialize(forwardMsgs[0])
	} else {
		forwardBody, err = h.parser.SerializeBatch(forwardMsgs)
	}
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to serialize forwarded message")
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Forward to upstream.
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.upstream.String(), strings.NewReader(string(forwardBody)))
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to create upstream request")
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Accept", r.Header.Get("Accept"))

	upstreamResp, err := h.client.Do(upstreamReq)
	if err != nil {
		h.logger.Error().Err(err).Msg("upstream request failed")
		http.Error(w, "Upstream error", http.StatusBadGateway)
		return
	}

	contentType := upstreamResp.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "text/event-stream") {
		// handleSSEStream takes ownership of the body and closes it.
		h.handleSSEStream(w, upstreamResp)
		return
	}

	// Regular JSON response.
	defer func() { _ = upstreamResp.Body.Close() }()
	respBody, err := io.ReadAll(upstreamResp.Body)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to read upstream response")
		http.Error(w, "Upstream read error", http.StatusBadGateway)
		return
	}

	if len(respBody) == 0 {
		w.WriteHeader(upstreamResp.StatusCode)
		return
	}

	// Parse and process through pipeline (incoming direction).
	respMsgs, err := h.parser.Parse(respBody)
	if err != nil {
		// Can't parse — forward raw.
		h.logger.Warn().Err(err).Msg("failed to parse upstream response, forwarding raw")
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(upstreamResp.StatusCode)
		if _, wErr := w.Write(respBody); wErr != nil {
			h.logger.Error().Err(wErr).Msg("failed to write raw response")
		}
		return
	}

	var finalMsgs []*protocol.Message
	for _, msg := range respMsgs {
		mctx := &MessageContext{
			ServerName: h.cfg.ServerName,
			Direction:  models.DirectionIncoming,
			Timestamp:  time.Now(),
		}

		decision, processedMsg, pErr := h.pipeline.Process(r.Context(), msg, mctx)
		if pErr != nil {
			h.logger.Error().Err(pErr).Msg("pipeline error on response")
			continue
		}

		if decision.Action == DecisionBlock {
			if id, ok := msg.GetID(); ok {
				finalMsgs = append(finalMsgs, NewBlockErrorResponse(id, decision.Reason))
			}
			continue
		}

		if processedMsg != nil {
			finalMsgs = append(finalMsgs, processedMsg)
		}
	}

	// Merge blocked responses from the request phase.
	finalMsgs = append(finalMsgs, blockedResponses...)
	h.writeJSONRPCResponse(w, finalMsgs)
}

// handleSSEStream proxies Server-Sent Events from upstream to the client,
// processing each event through the pipeline.
func (h *HTTPProxy) handleSSEStream(w http.ResponseWriter, upstreamResp *http.Response) {
	defer func() { _ = upstreamResp.Body.Close() }()

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.logger.Error().Msg("response writer does not support flushing for SSE")
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	scanner := bufio.NewScanner(upstreamResp.Body)
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
			continue
		}

		if strings.HasPrefix(line, "event: ") || strings.HasPrefix(line, "id: ") || strings.HasPrefix(line, "retry: ") {
			// Forward non-data SSE fields directly.
			_, _ = fmt.Fprintf(w, "%s\n", line)
			continue
		}

		// Blank line = end of event.
		if line == "" && len(dataLines) > 0 {
			data := strings.Join(dataLines, "\n")
			dataLines = nil

			// Try to parse and process through pipeline.
			msgs, err := h.parser.Parse([]byte(data))
			if err != nil {
				// Forward unparseable events as-is.
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
				continue
			}

			for _, msg := range msgs {
				mctx := &MessageContext{
					ServerName: h.cfg.ServerName,
					Direction:  models.DirectionIncoming,
					Timestamp:  time.Now(),
				}

				decision, processedMsg, pErr := h.pipeline.Process(context.Background(), msg, mctx)
				if pErr != nil {
					h.logger.Error().Err(pErr).Msg("pipeline error on SSE event")
					continue
				}

				if decision.Action == DecisionBlock {
					// For blocked SSE events, send error response.
					if id, ok := msg.GetID(); ok {
						errResp := NewBlockErrorResponse(id, decision.Reason)
						errData, _ := h.parser.Serialize(errResp)
						_, _ = fmt.Fprintf(w, "data: %s\n\n", string(errData))
						flusher.Flush()
					}
					continue
				}

				if processedMsg != nil {
					eventData, sErr := h.parser.Serialize(processedMsg)
					if sErr != nil {
						h.logger.Error().Err(sErr).Msg("failed to serialize SSE event")
						continue
					}
					_, _ = fmt.Fprintf(w, "data: %s\n\n", string(eventData))
					flusher.Flush()
				}
			}
		} else if line == "" {
			// Empty event separator.
			_, _ = fmt.Fprintf(w, "\n")
			flusher.Flush()
		}
	}

	if err := scanner.Err(); err != nil {
		h.logger.Error().Err(err).Msg("SSE stream error")
	}
}

// writeJSONRPCResponse writes JSON-RPC response messages to the HTTP response.
func (h *HTTPProxy) writeJSONRPCResponse(w http.ResponseWriter, msgs []*protocol.Message) {
	w.Header().Set("Content-Type", "application/json")

	if len(msgs) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var data []byte
	var err error
	if len(msgs) == 1 {
		data, err = h.parser.Serialize(msgs[0])
	} else {
		data, err = h.parser.SerializeBatch(msgs)
	}

	if err != nil {
		h.logger.Error().Err(err).Msg("failed to serialize response")
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, wErr := w.Write(data); wErr != nil {
		h.logger.Error().Err(wErr).Msg("failed to write JSON-RPC response")
	}
}
