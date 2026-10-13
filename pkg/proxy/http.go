package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/rs/zerolog"
)

const (
	DefaultRequestTimeout = 60 * time.Second
	DefaultListenAddr     = "127.0.0.1:0"
	EndpointPath          = "/mcp"
	UpstreamErrorCode     = -32603
	legacyProbeTimeout    = 10 * time.Second
	legacyProbeLimit      = 64 << 10
	readHeaderTimeout     = 10 * time.Second
	headerSessionID       = "Mcp-Session-Id"
	headerProtocolVersion = "Mcp-Protocol-Version"
	contentTypeJSON       = "application/json"
	contentTypeSSE        = "text/event-stream"
)

var (
	ErrLegacyTransport = errors.New(
		"the upstream speaks the legacy HTTP+SSE transport of MCP 2024-11-05, which gremlyn does not proxy: " +
			"point the url at the server's Streamable HTTP endpoint")
	ErrUpstreamUnauthorized = errors.New(
		"the upstream refused the request's credentials: check the configured headers. " +
			"gremlyn sends static headers only and does not run the OAuth flow")
	ErrUpstreamRedirect = errors.New(
		"the upstream answered with a redirect, which gremlyn does not follow so that credentials never leave " +
			"for another URL: configure the final URL")
)

type HTTPConfig struct {
	ServerName     string
	URL            string
	Headers        map[string]string
	ListenAddr     string
	RequestTimeout time.Duration
}

type HTTPStats struct {
	Timeouts int `json:"timeouts"`
	Failures int `json:"failures"`
}

type HTTPProxy struct {
	cfg        HTTPConfig
	upstream   *url.URL
	pipeline   *Pipeline
	logger     zerolog.Logger
	maxPayload int64
	timeout    time.Duration
	transport  *http.Transport
	client     *http.Client
	server     *http.Server
	listener   net.Listener
	closing    chan struct{}
	closeOnce  sync.Once
	serveDone  chan struct{}
	mu         sync.Mutex
	diagnostic error
	stats      HTTPStats
}

func NewHTTPProxy(cfg HTTPConfig, opts ...Option) (*HTTPProxy, error) {
	upstream, err := ValidateUpstreamURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = DefaultListenAddr
	}
	if err := ValidateListenAddr(cfg.ListenAddr); err != nil {
		return nil, err
	}
	for name := range cfg.Headers {
		if ReservedUpstreamHeader(name) {
			return nil, fmt.Errorf("header %q is managed by the proxy and cannot be configured", name)
		}
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}

	o := applyOptions(opts)
	transport := http.DefaultTransport.(*http.Transport).Clone()

	h := &HTTPProxy{
		cfg:        cfg,
		upstream:   upstream,
		pipeline:   o.pipeline,
		logger:     o.logger.With().Str("component", "http-proxy").Str("server", cfg.ServerName).Logger(),
		maxPayload: o.maxPayload,
		timeout:    timeout,
		transport:  transport,
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		closing:   make(chan struct{}),
		serveDone: make(chan struct{}),
	}
	return h, nil
}

func ValidateUpstreamURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("url is not a valid URL: %w", err)
	}
	if u.User != nil {
		return nil, errors.New("url must not carry credentials: put them in headers")
	}
	if u.Host == "" {
		return nil, errors.New("url needs a host")
	}
	if u.Fragment != "" {
		return nil, errors.New("url must not have a fragment")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return nil, fmt.Errorf("url %s uses plain http to a non-loopback host: "+
				"headers would cross the network in clear, use https", RedactURL(u))
		}
	default:
		return nil, fmt.Errorf("url scheme %q is not supported: use https, or http on a loopback address", u.Scheme)
	}
	return u, nil
}

func ValidateListenAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen address %q is not host:port: %w", addr, err)
	}
	if !isLoopbackHost(host) {
		return fmt.Errorf("listen address %q is not a loopback address: the proxy never listens on the network", addr)
	}
	return nil
}

func RedactURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String()
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Proxy-Connection",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

var reservedUpstreamHeaders = append([]string{
	"Host",
	"Content-Length",
	"Content-Type",
	"Accept",
	"Accept-Encoding",
	"Origin",
	headerSessionID,
	headerProtocolVersion,
	"Last-Event-Id",
}, hopByHopHeaders...)

func ReservedUpstreamHeader(name string) bool {
	canonical := http.CanonicalHeaderKey(name)
	for _, h := range reservedUpstreamHeaders {
		if canonical == h {
			return true
		}
	}
	return false
}

func copyHeaders(dst, src http.Header, drop ...string) {
	skip := make(map[string]bool, len(hopByHopHeaders)+len(drop))
	for _, h := range hopByHopHeaders {
		skip[h] = true
	}
	for _, h := range drop {
		skip[http.CanonicalHeaderKey(h)] = true
	}
	for _, v := range src.Values("Connection") {
		for name := range strings.SplitSeq(v, ",") {
			if name = strings.TrimSpace(name); name != "" {
				skip[http.CanonicalHeaderKey(name)] = true
			}
		}
	}
	for name, values := range src {
		if skip[http.CanonicalHeaderKey(name)] {
			continue
		}
		for _, v := range values {
			dst.Add(name, v)
		}
	}
}

func (h *HTTPProxy) Start(ctx context.Context) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", h.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", h.cfg.ListenAddr, err)
	}
	h.listener = ln
	h.server = &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ErrorLog:          log.New(h.logger, "", 0),
	}
	go func() {
		defer close(h.serveDone)
		if serveErr := h.server.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			h.logger.Error().Err(serveErr).Msg("http proxy stopped serving")
		}
	}()
	h.logger.Info().
		Str("listen", h.URL()).
		Str("upstream", RedactURL(h.upstream)).
		Msg("http proxy started")
	return nil
}

func (h *HTTPProxy) URL() string {
	if h.listener == nil {
		return ""
	}
	return "http://" + h.listener.Addr().String() + EndpointPath
}

func (h *HTTPProxy) Shutdown(ctx context.Context) error {
	h.closeOnce.Do(func() { close(h.closing) })
	if h.server == nil {
		return nil
	}
	err := h.server.Shutdown(ctx)
	if err != nil {
		h.logger.Warn().Err(err).Msg("in-flight exchanges did not drain in time, closing them")
		if cerr := h.server.Close(); cerr != nil {
			h.logger.Debug().Err(cerr).Msg("closing http proxy")
		}
	}
	<-h.serveDone
	h.transport.CloseIdleConnections()
	h.logger.Info().Msg("http proxy stopped")
	if err != nil {
		return fmt.Errorf("draining http proxy: %w", err)
	}
	return nil
}

func (h *HTTPProxy) Diagnostic() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.diagnostic
}

func (h *HTTPProxy) Stats() HTTPStats {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stats
}

func (h *HTTPProxy) setDiagnostic(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.diagnostic == nil {
		h.diagnostic = err
	}
}

func (h *HTTPProxy) countTimeout() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stats.Timeouts++
}

func (h *HTTPProxy) countFailure() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stats.Failures++
}

func (h *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if hostname, _, err := net.SplitHostPort(host); err == nil {
		host = hostname
	}
	if !isLoopbackHost(strings.Trim(host, "[]")) {
		http.Error(w, "gremlyn proxy: the Host header must name a loopback address", http.StatusForbidden)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !loopbackOrigin(origin) {
		http.Error(w, "gremlyn proxy: cross-origin requests are refused", http.StatusForbidden)
		return
	}
	if r.URL.Path != EndpointPath {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodPost:
		h.handlePost(w, r)
	case http.MethodGet:
		h.handleGet(w, r)
	case http.MethodDelete:
		h.handleDelete(w, r)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "gremlyn proxy: method not allowed", http.StatusMethodNotAllowed)
	}
}

func loopbackOrigin(origin string) bool {
	if origin == "null" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return isLoopbackHost(u.Hostname())
}

type watchdog struct {
	timer *time.Timer
	fired atomic.Bool
	limit time.Duration
}

func newWatchdog(limit time.Duration, cancel context.CancelFunc) *watchdog {
	w := &watchdog{limit: limit}
	w.timer = time.AfterFunc(time.Hour, func() {
		w.fired.Store(true)
		cancel()
	})
	w.timer.Stop()
	return w
}

func (w *watchdog) arm()          { w.timer.Reset(w.limit) }
func (w *watchdog) disarm()       { w.timer.Stop() }
func (w *watchdog) expired() bool { return w.fired.Load() }

func (h *HTTPProxy) handlePost(w http.ResponseWriter, r *http.Request) {
	session := r.Header.Get(headerSessionID)

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxPayload))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeRPCError(w, http.StatusRequestEntityTooLarge, protocol.JSONRPCID{}, -32600,
				fmt.Sprintf("gremlyn: the message is over the %d byte limit", h.maxPayload))
			return
		}
		h.logger.Debug().Err(err).Msg("reading the agent's request body")
		return
	}

	if protocol.IsBatch(body) {
		writeRPCError(w, http.StatusBadRequest, protocol.JSONRPCID{}, -32600, "gremlyn: "+protocol.ErrBatch.Error())
		return
	}

	msg, err := protocol.ParseMessage(body)
	if err != nil {
		writeRPCError(w, http.StatusBadRequest, protocol.JSONRPCID{}, -32700,
			"gremlyn: the body is not a JSON-RPC message: "+err.Error())
		return
	}

	mctx := &MessageContext{
		ServerName: h.cfg.ServerName,
		Session:    session,
		Direction:  models.DirectionOutgoing,
		Timestamp:  time.Now(),
	}
	decision, processed, err := h.pipeline.Process(r.Context(), msg, mctx)
	if err != nil {
		h.logger.Error().Err(err).Msg("pipeline error on outgoing message")
		processed = msg
		decision = &Decision{Action: DecisionAllow}
	}
	if decision.Action == DecisionBlock {
		if id, ok := msg.GetID(); ok {
			writeMessage(w, http.StatusOK, NewBlockErrorResponse(id, decision.Reason))
			return
		}
		writeRPCError(w, http.StatusBadRequest, protocol.JSONRPCID{}, -32600, "Blocked by Gremlyn: "+decision.Reason)
		return
	}

	payload, err := wireBytes(msg, processed)
	if err != nil {
		h.logger.Error().Err(err).Msg("encoding the outgoing message")
		writeRPCError(w, http.StatusInternalServerError, requestID(msg), UpstreamErrorCode, "gremlyn: "+err.Error())
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	dog := newWatchdog(h.timeout, cancel)
	defer dog.disarm()

	dog.arm()
	resp, err := h.forward(ctx, r, http.MethodPost, payload)
	dog.disarm()
	if err != nil {
		h.upstreamFailed(w, r, msg, err, dog.expired())
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if h.legacyUpstream(r.Context(), msg, resp, session) {
		h.setDiagnostic(ErrLegacyTransport)
		h.logger.Error().Str("upstream", RedactURL(h.upstream)).Msg(ErrLegacyTransport.Error())
		writeRPCError(w, http.StatusBadGateway, requestID(msg), UpstreamErrorCode, "gremlyn: "+ErrLegacyTransport.Error())
		return
	}

	h.relayResponse(w, r, resp, msg, session, dog)
}

func (h *HTTPProxy) handleGet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-h.closing:
			cancel()
		case <-ctx.Done():
		}
	}()

	dog := newWatchdog(h.timeout, cancel)
	defer dog.disarm()
	dog.arm()
	resp, err := h.forward(ctx, r, http.MethodGet, nil)
	dog.disarm()
	if err != nil {
		h.upstreamFailed(w, r, nil, err, dog.expired())
		return
	}
	defer func() { _ = resp.Body.Close() }()

	h.relayResponse(w, r, resp, nil, r.Header.Get(headerSessionID), nil)
}

func (h *HTTPProxy) handleDelete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	dog := newWatchdog(h.timeout, cancel)
	defer dog.disarm()
	dog.arm()
	resp, err := h.forward(ctx, r, http.MethodDelete, nil)
	if err != nil {
		h.upstreamFailed(w, r, nil, err, dog.expired())
		return
	}
	defer func() { _ = resp.Body.Close() }()
	h.passThrough(w, resp, nil)
}

func (h *HTTPProxy) forward(ctx context.Context, r *http.Request, method string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.upstream.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("building the upstream request: %w", err)
	}
	copyHeaders(req.Header, r.Header, "Content-Length", "Accept-Encoding", "Origin", "Host")
	if method == http.MethodPost {
		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", contentTypeJSON)
		}
		if req.Header.Get("Accept") == "" {
			req.Header.Set("Accept", contentTypeJSON+", "+contentTypeSSE)
		}
	}
	if method == http.MethodGet && req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", contentTypeSSE)
	}
	for name, value := range h.cfg.Headers {
		req.Header.Set(name, value)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("%s %s: %w", method, RedactURL(h.upstream), err)
	}
	return resp, nil
}

func (h *HTTPProxy) upstreamFailed(w http.ResponseWriter, r *http.Request, msg *protocol.Message, err error, timedOut bool) {
	if !timedOut && r.Context().Err() != nil {
		h.logger.Debug().Msg("the agent went away before the upstream answered")
		return
	}
	if timedOut {
		h.countTimeout()
		h.logger.Warn().
			Dur("request_timeout", h.timeout).
			Str("method", methodOf(msg)).
			Msg("the upstream did not answer in time")
		writeRPCError(w, http.StatusGatewayTimeout, requestID(msg), UpstreamErrorCode,
			fmt.Sprintf("gremlyn: the upstream MCP server did not answer within %s", h.timeout))
		return
	}
	h.countFailure()
	h.logger.Warn().Err(err).Msg("forwarding to the upstream failed")
	writeRPCError(w, http.StatusBadGateway, requestID(msg), UpstreamErrorCode,
		"gremlyn: the upstream MCP server could not be reached")
}

func (h *HTTPProxy) legacyUpstream(ctx context.Context, msg *protocol.Message, resp *http.Response, session string) bool {
	if session != "" || methodOf(msg) != string(protocol.MCPMethodInitialize) {
		return false
	}
	if resp.StatusCode < 400 || resp.StatusCode >= 500 ||
		resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return false
	}

	probeCtx, cancel := context.WithTimeout(ctx, min(h.timeout, legacyProbeTimeout))
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, h.upstream.String(), http.NoBody)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", contentTypeSSE)
	for name, value := range h.cfg.Headers {
		req.Header.Set(name, value)
	}
	probe, err := h.client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = probe.Body.Close() }()
	if probe.StatusCode != http.StatusOK || mediaType(probe.Header) != contentTypeSSE {
		return false
	}
	ev, err := protocol.NewSSEReader(probe.Body, legacyProbeLimit).Next()
	return err == nil && ev.Type() == "endpoint"
}

func (h *HTTPProxy) relayResponse(w http.ResponseWriter, r *http.Request, resp *http.Response, reqMsg *protocol.Message, session string, dog *watchdog) {
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		h.setDiagnostic(fmt.Errorf("%w (HTTP %d)", ErrUpstreamUnauthorized, resp.StatusCode))
		h.logger.Error().Int("status", resp.StatusCode).Msg(ErrUpstreamUnauthorized.Error())
		h.passThrough(w, resp, dog)
		return
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		h.setDiagnostic(ErrUpstreamRedirect)
		h.logger.Error().Int("status", resp.StatusCode).Msg(ErrUpstreamRedirect.Error())
		writeRPCError(w, http.StatusBadGateway, requestID(reqMsg), UpstreamErrorCode, "gremlyn: "+ErrUpstreamRedirect.Error())
		return
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		h.passThrough(w, resp, dog)
		return
	}

	switch mediaType(resp.Header) {
	case contentTypeSSE:
		h.relaySSE(w, r, resp, reqMsg, session, dog)
	case contentTypeJSON:
		h.relayJSON(w, r, resp, reqMsg, session, dog)
	default:
		h.passThrough(w, resp, dog)
	}
}

func (h *HTTPProxy) relayJSON(w http.ResponseWriter, r *http.Request, resp *http.Response, reqMsg *protocol.Message, session string, dog *watchdog) {
	if dog != nil {
		dog.arm()
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, h.maxPayload+1))
	if dog != nil {
		dog.disarm()
	}
	if err != nil {
		h.upstreamFailed(w, r, reqMsg, err, dog != nil && dog.expired())
		return
	}
	if int64(len(body)) > h.maxPayload {
		h.countFailure()
		h.logger.Warn().Int64("limit", h.maxPayload).Msg("the upstream response is over the size limit")
		writeRPCError(w, http.StatusBadGateway, requestID(reqMsg), UpstreamErrorCode,
			fmt.Sprintf("gremlyn: the upstream response is over the %d byte limit", h.maxPayload))
		return
	}

	out := body
	if len(bytes.TrimSpace(body)) > 0 {
		out = h.processIncoming(r.Context(), body, session)
	}
	if out == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	copyHeaders(w.Header(), resp.Header, "Content-Length", "WWW-Authenticate")
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	w.WriteHeader(resp.StatusCode)
	if _, err := w.Write(out); err != nil {
		h.logger.Debug().Err(err).Msg("writing the response to the agent")
	}
}

func (h *HTTPProxy) processIncoming(ctx context.Context, data []byte, session string) []byte {
	msg, err := protocol.ParseMessage(data)
	if err != nil {
		h.logger.Warn().Int("bytes", len(data)).Msg("the upstream sent something that is not a JSON-RPC message, forwarding it untouched")
		return data
	}
	mctx := &MessageContext{
		ServerName: h.cfg.ServerName,
		Session:    session,
		Direction:  models.DirectionIncoming,
		Timestamp:  time.Now(),
	}
	decision, processed, err := h.pipeline.Process(ctx, msg, mctx)
	if err != nil {
		h.logger.Error().Err(err).Msg("pipeline error on incoming message")
		return data
	}
	if decision.Action == DecisionBlock {
		id, ok := msg.GetID()
		if !ok {
			h.logger.Info().Str("method", msg.GetMethod()).Msg("incoming message blocked")
			return nil
		}
		processed = NewBlockErrorResponse(id, decision.Reason)
	}
	out, err := wireBytes(msg, processed)
	if err != nil {
		h.logger.Error().Err(err).Msg("encoding the incoming message, forwarding the original")
		return data
	}
	return out
}

func (h *HTTPProxy) relaySSE(w http.ResponseWriter, r *http.Request, resp *http.Response, reqMsg *protocol.Message, session string, dog *watchdog) {
	rc := http.NewResponseController(w)
	copyHeaders(w.Header(), resp.Header, "Content-Length", "WWW-Authenticate")
	w.Header().Set("Content-Type", contentTypeSSE)
	w.WriteHeader(resp.StatusCode)
	if err := rc.Flush(); err != nil {
		h.logger.Debug().Err(err).Msg("flushing SSE headers")
		return
	}

	wantID, waiting := "", false
	if isRequest(reqMsg) {
		wantID, waiting = reqMsg.Request.ID.String(), true
	}

	reader := protocol.NewSSEReader(resp.Body, h.maxPayload)
	first := true
	for {
		if dog != nil {
			dog.arm()
		}
		ev, err := reader.Next()
		if dog != nil {
			dog.disarm()
		}
		if err != nil {
			h.endSSE(w, rc, r, reqMsg, waiting, err, dog != nil && dog.expired())
			return
		}

		if first && reqMsg == nil && ev.Type() == "endpoint" {
			h.setDiagnostic(ErrLegacyTransport)
			h.logger.Error().Str("upstream", RedactURL(h.upstream)).Msg(ErrLegacyTransport.Error())
			return
		}
		first = false

		out, answered := h.processEvent(r.Context(), ev, session, wantID)
		if answered {
			waiting = false
		}
		if out == nil {
			continue
		}
		if err := protocol.WriteSSEEvent(w, out); err != nil {
			h.logger.Debug().Err(err).Msg("writing an SSE event to the agent")
			return
		}
		if err := rc.Flush(); err != nil {
			h.logger.Debug().Err(err).Msg("flushing an SSE event to the agent")
			return
		}
	}
}

func (h *HTTPProxy) processEvent(ctx context.Context, ev *protocol.SSEEvent, session, wantID string) (*protocol.SSEEvent, bool) {
	data, ok := ev.Data()
	if !ok || ev.Type() != "message" || len(bytes.TrimSpace(data)) == 0 {
		return ev, false
	}
	answered := false
	if wantID != "" {
		if msg, err := protocol.ParseMessage(data); err == nil && msg.Type == protocol.MessageTypeResponse {
			if id, ok := msg.GetID(); ok && id.String() == wantID {
				answered = true
			}
		}
	}
	out := h.processIncoming(ctx, data, session)
	if out == nil {
		return nil, answered
	}
	if bytes.Equal(out, data) {
		return ev, answered
	}
	replaced := ev.WithData(out)
	return &replaced, answered
}

func (h *HTTPProxy) endSSE(w http.ResponseWriter, rc *http.ResponseController, r *http.Request, reqMsg *protocol.Message, waiting bool, err error, timedOut bool) {
	if errors.Is(err, io.EOF) {
		return
	}
	if !timedOut && r.Context().Err() != nil {
		return
	}
	if !waiting {
		if !timedOut {
			h.logger.Debug().Err(err).Msg("upstream SSE stream ended")
		}
		return
	}

	message := "gremlyn: the upstream SSE stream broke before the response"
	switch {
	case timedOut:
		h.countTimeout()
		h.logger.Warn().Dur("request_timeout", h.timeout).Msg("the upstream stream went silent before the response")
		message = fmt.Sprintf("gremlyn: the upstream MCP server did not answer within %s", h.timeout)
	case errors.Is(err, protocol.ErrSSEEventTooLarge):
		h.countFailure()
		h.logger.Warn().Int64("limit", h.maxPayload).Msg("an upstream SSE event is over the size limit")
		message = fmt.Sprintf("gremlyn: an upstream event is over the %d byte limit", h.maxPayload)
	default:
		h.countFailure()
		h.logger.Warn().Err(err).Msg("the upstream SSE stream broke")
	}

	data, encErr := protocol.EncodeMessage(rpcError(requestID(reqMsg), UpstreamErrorCode, message))
	if encErr != nil {
		return
	}
	ev := &protocol.SSEEvent{Fields: []protocol.SSEField{{Name: "event", Value: "message"}, {Name: "data", Value: string(data)}}}
	if werr := protocol.WriteSSEEvent(w, ev); werr == nil {
		_ = rc.Flush()
	}
}

func (h *HTTPProxy) passThrough(w http.ResponseWriter, resp *http.Response, dog *watchdog) {
	if dog != nil {
		dog.arm()
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, h.maxPayload+1))
	if dog != nil {
		dog.disarm()
	}
	if err != nil {
		h.countFailure()
		h.logger.Warn().Err(err).Msg("reading the upstream response")
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	if int64(len(body)) > h.maxPayload {
		h.countFailure()
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	copyHeaders(w.Header(), resp.Header, "Content-Length", "WWW-Authenticate")
	if len(body) > 0 {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	}
	w.WriteHeader(resp.StatusCode)
	if len(body) > 0 {
		if _, err := w.Write(body); err != nil {
			h.logger.Debug().Err(err).Msg("writing the response to the agent")
		}
	}
}

func mediaType(header http.Header) string {
	mt, _, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil {
		return ""
	}
	return strings.ToLower(mt)
}

func wireBytes(original, processed *protocol.Message) ([]byte, error) {
	if processed == original && len(original.Raw) > 0 {
		return original.Raw, nil
	}
	data, err := protocol.EncodeMessage(processed)
	if err != nil {
		return nil, fmt.Errorf("encoding message: %w", err)
	}
	return data, nil
}

func isRequest(msg *protocol.Message) bool {
	return msg != nil && msg.Type == protocol.MessageTypeRequest && msg.Request != nil
}

func requestID(msg *protocol.Message) protocol.JSONRPCID {
	if !isRequest(msg) {
		return protocol.JSONRPCID{}
	}
	return msg.Request.ID
}

func methodOf(msg *protocol.Message) string {
	if msg == nil {
		return ""
	}
	return msg.GetMethod()
}

func rpcError(id protocol.JSONRPCID, code int, message string) *protocol.Message {
	return &protocol.Message{
		Type: protocol.MessageTypeResponse,
		Response: &protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      id,
			Error:   &protocol.JSONRPCError{Code: code, Message: message},
		},
	}
}

func writeRPCError(w http.ResponseWriter, status int, id protocol.JSONRPCID, code int, message string) {
	writeMessage(w, status, rpcError(id, code, message))
}

func writeMessage(w http.ResponseWriter, status int, msg *protocol.Message) {
	data, err := protocol.EncodeMessage(msg)
	if err != nil {
		data = []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"gremlyn: encoding failed"}}`)
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
