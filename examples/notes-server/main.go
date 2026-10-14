package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
)

const (
	latestProtocol = "2025-06-18"
	maxBody        = 1 << 20
)

func supportedProtocols() []string {
	return []string{"2025-06-18", "2025-03-26"}
}

type note struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

type annotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
	IdempotentHint  bool `json:"idempotentHint"`
	OpenWorldHint   bool `json:"openWorldHint"`
}

type property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type inputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type tool struct {
	Name        string      `json:"name"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	InputSchema inputSchema `json:"inputSchema"`
	Annotations annotations `json:"annotations"`
}

func toolList() []tool {
	return []tool{
		{
			Name:        "list_notes",
			Title:       "List notes",
			Description: "Lists every note with its id, title and text.",
			InputSchema: inputSchema{Type: "object", Properties: map[string]property{}},
			Annotations: annotations{ReadOnlyHint: true},
		},
		{
			Name:        "create_note",
			Title:       "Create a note",
			Description: "Creates a new note and returns its id. Each call creates another note.",
			InputSchema: inputSchema{Type: "object", Properties: map[string]property{
				"title": {Type: "string", Description: "The note's title"},
				"body":  {Type: "string", Description: "The note's text"},
			}, Required: []string{"title", "body"}},
			Annotations: annotations{IdempotentHint: false},
		},
		{
			Name:        "delete_note",
			Title:       "Delete a note",
			Description: "Deletes the note with this id, permanently.",
			InputSchema: inputSchema{Type: "object", Properties: map[string]property{
				"id": {Type: "integer", Description: "The id of the note to delete"},
			}, Required: []string{"id"}},
			Annotations: annotations{DestructiveHint: true, IdempotentHint: true},
		},
	}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type server struct {
	mu       sync.Mutex
	notes    map[int]note
	nextID   int
	sessions map[string]bool
	log      zerolog.Logger
}

func newServer(log zerolog.Logger) *server {
	s := &server{notes: map[int]note{}, nextID: 1, sessions: map[string]bool{}, log: log}
	for _, n := range []note{
		{Title: "groceries", Body: "milk, eggs, coffee"},
		{Title: "standup", Body: "demo the chaos report, ask about the release date"},
		{Title: "scratch", Body: "half-written idea about retry budgets"},
	} {
		s.add(n.Title, n.Body)
	}
	return s
}

func (s *server) add(title, body string) int {
	id := s.nextID
	s.nextID++
	s.notes[id] = note{ID: id, Title: title, Body: body}
	return id
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/mcp" {
		http.NotFound(w, r)
		return
	}
	if !loopbackOrigin(r.Header.Get("Origin")) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.post(w, r)
	case http.MethodDelete:
		s.mu.Lock()
		delete(s.sessions, r.Header.Get("Mcp-Session-Id"))
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	default:
		w.Header().Set("Allow", "POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) post(w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&req); err != nil {
		http.Error(w, "the body must be one JSON-RPC message", http.StatusBadRequest)
		return
	}
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	if req.Method == "initialize" {
		session, err := newSessionID()
		if err != nil {
			http.Error(w, "could not start a session", http.StatusInternalServerError)
			return
		}
		s.mu.Lock()
		s.sessions[session] = true
		s.mu.Unlock()
		w.Header().Set("Mcp-Session-Id", session)
		writeJSON(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: initializeResult(req.Params)})
		return
	}

	s.mu.Lock()
	known := s.sessions[r.Header.Get("Mcp-Session-Id")]
	s.mu.Unlock()
	if !known {
		http.Error(w, "unknown or missing Mcp-Session-Id", http.StatusNotFound)
		return
	}

	writeJSON(w, s.handle(req))
}

func (s *server) handle(req rpcRequest) rpcResponse {
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "ping":
		resp.Result = struct{}{}
	case "tools/list":
		resp.Result = struct {
			Tools []tool `json:"tools"`
		}{toolList()}
	case "tools/call":
		result, rerr := s.call(req.Params)
		resp.Result, resp.Error = result, rerr
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp
}

func (s *server) call(raw json.RawMessage) (*toolResult, *rpcError) {
	var params struct {
		Name      string `json:"name"`
		Arguments struct {
			Title string `json:"title"`
			Body  string `json:"body"`
			ID    int    `json:"id"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid tools/call params"}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	switch params.Name {
	case "list_notes":
		s.log.Info().Str("tool", params.Name).Int("notes", len(s.notes)).Msg("listed")
		return text(s.listing()), nil
	case "create_note":
		if strings.TrimSpace(params.Arguments.Title) == "" {
			return &toolResult{Content: []textContent{{Type: "text", Text: "a note needs a title"}}, IsError: true}, nil
		}
		id := s.add(params.Arguments.Title, params.Arguments.Body)
		s.log.Info().Str("tool", params.Name).Int("id", id).Msg("created")
		return text(fmt.Sprintf("Created note #%d, %q.", id, params.Arguments.Title)), nil
	case "delete_note":
		if _, ok := s.notes[params.Arguments.ID]; !ok {
			return text(fmt.Sprintf("There is no note #%d.", params.Arguments.ID)), nil
		}
		delete(s.notes, params.Arguments.ID)
		s.log.Warn().Str("tool", params.Name).Int("id", params.Arguments.ID).Msg("deleted")
		return text(fmt.Sprintf("Deleted note #%d.", params.Arguments.ID)), nil
	default:
		return nil, &rpcError{Code: -32602, Message: "unknown tool: " + params.Name}
	}
}

func (s *server) listing() string {
	if len(s.notes) == 0 {
		return "There are no notes."
	}
	ids := make([]int, 0, len(s.notes))
	for id := range s.notes {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	var b strings.Builder
	for _, id := range ids {
		n := s.notes[id]
		fmt.Fprintf(&b, "#%d %s: %s\n", n.ID, n.Title, n.Body)
	}
	return b.String()
}

func text(s string) *toolResult {
	return &toolResult{Content: []textContent{{Type: "text", Text: s}}}
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type capabilities struct {
	Tools struct{} `json:"tools"`
}

type initialized struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    capabilities `json:"capabilities"`
	ServerInfo      serverInfo   `json:"serverInfo"`
}

func initializeResult(params json.RawMessage) initialized {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	version := latestProtocol
	if json.Unmarshal(params, &p) == nil {
		for _, v := range supportedProtocols() {
			if v == p.ProtocolVersion {
				version = v
			}
		}
	}
	return initialized{ProtocolVersion: version, ServerInfo: serverInfo{Name: "gremlyn-notes-example", Version: "1"}}
}

func writeJSON(w http.ResponseWriter, v rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "encoding the response", http.StatusInternalServerError)
	}
}

func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func loopbackOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return isLoopbackHost(u.Hostname())
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func checkLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("--addr %q: %w", addr, err)
	}
	if !isLoopbackHost(host) {
		return fmt.Errorf("--addr %q: this example listens on a loopback address only", addr)
	}
	return nil
}

func run(ctx context.Context, addr string, log zerolog.Logger) error {
	if err := checkLoopback(addr); err != nil {
		return err
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	srv := &http.Server{Handler: newServer(log), ReadHeaderTimeout: 10 * time.Second}
	log.Info().Str("url", "http://"+ln.Addr().String()+"/mcp").Msg("listening")

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return fmt.Errorf("serving: %w", err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serving: %w", err)
	}
	return nil
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8931", "loopback address to listen on")
	flag.Parse()

	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.TimeOnly}).With().Timestamp().Logger()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, *addr, log)
	stop()
	if err != nil {
		log.Error().Err(err).Msg("notes-server stopped")
		os.Exit(1)
	}
}
