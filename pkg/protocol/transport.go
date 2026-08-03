package protocol

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Transport abstracts how JSON-RPC messages are physically read and written,
// so the proxy logic does not care whether it talks over stdio pipes or HTTP connections.
type Transport interface {
	// ReadMessage blocks until a message is available and returns it parsed.
	// Returns io.EOF on clean shutdown.
	ReadMessage(ctx context.Context) (*Message, error)

	// WriteMessage sends a message to the other side.
	WriteMessage(ctx context.Context, msg *Message) error

	// Close performs a clean shutdown of the transport.
	Close() error
}

// --- Content-Length Framing (for stdio JSON-RPC) ---

// ContentLengthFramer handles content-length framed JSON-RPC messages
// as used by MCP over stdio. Format: Content-Length: N\r\n\r\n{json}
type ContentLengthFramer struct{}

// ReadFrame reads one content-length-framed message from the reader.
func (f *ContentLengthFramer) ReadFrame(reader *bufio.Reader) ([]byte, error) {
	// Read headers until we find a blank line.
	var contentLength int
	foundContentLength := false

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("reading frame header: %w", err)
		}

		line = strings.TrimRight(line, "\r\n")

		// Blank line signals end of headers.
		if line == "" {
			break
		}

		// Parse Content-Length header.
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			valStr := strings.TrimSpace(line[len("content-length:"):])
			val, err := strconv.Atoi(valStr)
			if err != nil {
				return nil, fmt.Errorf("invalid content-length value %q: %w", valStr, err)
			}
			contentLength = val
			foundContentLength = true
		}
	}

	if !foundContentLength {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	if contentLength <= 0 {
		return nil, fmt.Errorf("invalid Content-Length: %d", contentLength)
	}

	// Read exactly contentLength bytes.
	body := make([]byte, contentLength)
	_, err := io.ReadFull(reader, body)
	if err != nil {
		return nil, fmt.Errorf("reading frame body (%d bytes): %w", contentLength, err)
	}

	return body, nil
}

// WriteFrame writes one content-length-framed message to the writer.
func (f *ContentLengthFramer) WriteFrame(writer io.Writer, data []byte) error {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	_, err := io.WriteString(writer, header)
	if err != nil {
		return fmt.Errorf("writing frame header: %w", err)
	}
	_, err = writer.Write(data)
	if err != nil {
		return fmt.Errorf("writing frame body: %w", err)
	}
	return nil
}

// --- Stdio Transport ---

// StdioTransport implements Transport for stdio-based MCP communication.
// Messages are content-length framed JSON-RPC over stdin/stdout.
type StdioTransport struct {
	reader  *bufio.Reader
	writer  io.Writer
	framer  ContentLengthFramer
	writeMu sync.Mutex
	closed  bool
	closeMu sync.Mutex
}

// NewStdioTransport creates a new StdioTransport from the given reader and writer.
func NewStdioTransport(r io.Reader, w io.Writer) *StdioTransport {
	return &StdioTransport{
		reader: bufio.NewReaderSize(r, 64*1024),
		writer: w,
	}
}

// ReadMessage reads one content-length-framed JSON-RPC message from the reader.
func (t *StdioTransport) ReadMessage(_ context.Context) (*Message, error) {
	t.closeMu.Lock()
	if t.closed {
		t.closeMu.Unlock()
		return nil, io.EOF
	}
	t.closeMu.Unlock()

	data, err := t.framer.ReadFrame(t.reader)
	if err != nil {
		return nil, fmt.Errorf("stdio read: %w", err)
	}

	msg, err := parseRawMessage(data)
	if err != nil {
		return nil, fmt.Errorf("stdio parse: %w", err)
	}

	return msg, nil
}

// WriteMessage writes one content-length-framed JSON-RPC message to the writer.
func (t *StdioTransport) WriteMessage(_ context.Context, msg *Message) error {
	t.closeMu.Lock()
	if t.closed {
		t.closeMu.Unlock()
		return fmt.Errorf("transport closed")
	}
	t.closeMu.Unlock()

	data, err := serializeMessage(msg)
	if err != nil {
		return fmt.Errorf("stdio serialize: %w", err)
	}

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	return t.framer.WriteFrame(t.writer, data)
}

// Close shuts down the transport.
func (t *StdioTransport) Close() error {
	t.closeMu.Lock()
	defer t.closeMu.Unlock()
	t.closed = true
	return nil
}

// --- HTTP Transport ---

// HTTPTransport implements Transport for HTTP/SSE-based MCP communication.
// Sends requests via POST and reads SSE streams for server-initiated messages.
type HTTPTransport struct {
	baseURL string
	client  *http.Client
	sseBody io.ReadCloser // SSE response body to close on shutdown
	msgCh   chan *Message // buffered channel for received SSE messages
	errCh   chan error    // channel for SSE read errors
	writeMu sync.Mutex
	closed  bool
	closeMu sync.Mutex
}

// NewHTTPTransport creates a new HTTPTransport that sends requests to the given base URL.
func NewHTTPTransport(baseURL string, client *http.Client) *HTTPTransport {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPTransport{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  client,
		msgCh:   make(chan *Message, 64),
		errCh:   make(chan error, 1),
	}
}

// ReadMessage reads the next message from the SSE stream or message channel.
func (t *HTTPTransport) ReadMessage(ctx context.Context) (*Message, error) {
	t.closeMu.Lock()
	if t.closed {
		t.closeMu.Unlock()
		return nil, io.EOF
	}
	t.closeMu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg := <-t.msgCh:
		return msg, nil
	case err := <-t.errCh:
		return nil, err
	}
}

// WriteMessage sends a JSON-RPC message as an HTTP POST to the upstream server.
// The response is parsed and enqueued for ReadMessage.
func (t *HTTPTransport) WriteMessage(ctx context.Context, msg *Message) error {
	t.closeMu.Lock()
	if t.closed {
		t.closeMu.Unlock()
		return fmt.Errorf("transport closed")
	}
	t.closeMu.Unlock()

	data, err := serializeMessage(msg)
	if err != nil {
		return fmt.Errorf("http serialize: %w", err)
	}

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL, strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("http create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("http send: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "text/event-stream") {
		// SSE response — read events in a goroutine.
		t.sseBody = resp.Body
		go t.readSSEStream(resp.Body)
		return nil
	}

	// Regular JSON response.
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("http read response: %w", err)
	}

	if len(body) == 0 {
		return nil
	}

	respMsg, err := parseRawMessage(body)
	if err != nil {
		return fmt.Errorf("http parse response: %w", err)
	}

	t.msgCh <- respMsg
	return nil
}

// Close shuts down the transport.
func (t *HTTPTransport) Close() error {
	t.closeMu.Lock()
	defer t.closeMu.Unlock()
	t.closed = true

	if t.sseBody != nil {
		return t.sseBody.Close()
	}
	return nil
}

// readSSEStream reads Server-Sent Events from the given reader and enqueues parsed messages.
func (t *HTTPTransport) readSSEStream(body io.ReadCloser) {
	defer func() { _ = body.Close() }()
	scanner := bufio.NewScanner(body)
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
			continue
		}

		// Blank line = end of event.
		if line == "" && len(dataLines) > 0 {
			data := strings.Join(dataLines, "\n")
			dataLines = nil

			msg, err := parseRawMessage([]byte(data))
			if err != nil {
				// Skip unparseable events.
				continue
			}

			t.closeMu.Lock()
			closed := t.closed
			t.closeMu.Unlock()
			if closed {
				return
			}

			t.msgCh <- msg
		}
	}

	if err := scanner.Err(); err != nil {
		t.closeMu.Lock()
		closed := t.closed
		t.closeMu.Unlock()
		if !closed {
			t.errCh <- fmt.Errorf("sse stream: %w", err)
		}
	}
}

// --- Shared helpers ---

// parseRawMessage parses raw JSON bytes into a Message by detecting the message type.
func parseRawMessage(data []byte) (*Message, error) {
	// Use a map to detect which fields are present.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	_, hasID := raw["id"]
	_, hasMethod := raw["method"]
	_, hasResult := raw["result"]
	_, hasError := raw["error"]

	msg := &Message{Raw: data}

	switch {
	case hasMethod && hasID:
		// Check if id is null — null id means it is actually a notification.
		if string(raw["id"]) == "null" {
			var notif JSONRPCNotification
			if err := json.Unmarshal(data, &notif); err != nil {
				return nil, fmt.Errorf("parse notification: %w", err)
			}
			msg.Type = MessageTypeNotification
			msg.Notification = &notif
		} else {
			var req JSONRPCRequest
			if err := json.Unmarshal(data, &req); err != nil {
				return nil, fmt.Errorf("parse request: %w", err)
			}
			msg.Type = MessageTypeRequest
			msg.Request = &req
		}

	case hasMethod && !hasID:
		var notif JSONRPCNotification
		if err := json.Unmarshal(data, &notif); err != nil {
			return nil, fmt.Errorf("parse notification: %w", err)
		}
		msg.Type = MessageTypeNotification
		msg.Notification = &notif

	case (hasResult || hasError) && hasID:
		var resp JSONRPCResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		msg.Type = MessageTypeResponse
		msg.Response = &resp

	default:
		return nil, fmt.Errorf("cannot determine JSON-RPC message type from fields: %v", keysOf(raw))
	}

	return msg, nil
}

// serializeMessage converts a Message back to JSON bytes.
func serializeMessage(msg *Message) ([]byte, error) {
	switch msg.Type {
	case MessageTypeRequest:
		if msg.Request == nil {
			return nil, fmt.Errorf("request message has nil request")
		}
		return json.Marshal(msg.Request)

	case MessageTypeResponse:
		if msg.Response == nil {
			return nil, fmt.Errorf("response message has nil response")
		}
		return json.Marshal(msg.Response)

	case MessageTypeNotification:
		if msg.Notification == nil {
			return nil, fmt.Errorf("notification message has nil notification")
		}
		return json.Marshal(msg.Notification)

	default:
		return nil, fmt.Errorf("unknown message type: %s", msg.Type)
	}
}

// keysOf returns the keys of a map for error messages.
func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
