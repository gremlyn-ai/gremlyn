package protocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

type Transport interface {
	ReadMessage(ctx context.Context) (*Message, error)
	WriteMessage(ctx context.Context, msg *Message) error
	Close() error
}

const MaxLineBytes = 16 << 20

type LineFramer struct {
	Limit int64
}

func (f *LineFramer) limit() int64 {
	if f.Limit > 0 && f.Limit <= MaxLineBytes {
		return f.Limit
	}
	return MaxLineBytes
}

func (f *LineFramer) ReadFrame(reader *bufio.Reader) ([]byte, error) {
	for {
		line, err := readLimitedLine(reader, int(f.limit()))
		if err != nil {
			return nil, err
		}

		line = bytes.TrimRight(line, "\r\n")
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		if hasPrefixFold(line, []byte("content-length:")) {
			return nil, fmt.Errorf(
				"peer is using LSP Content-Length framing, but MCP over stdio is newline-delimited JSON")
		}

		return line, nil
	}
}

func (f *LineFramer) WriteFrame(writer io.Writer, data []byte) error {
	if bytes.ContainsAny(data, "\n\r") {
		return fmt.Errorf("message contains an embedded newline, which MCP stdio framing forbids")
	}
	if int64(len(data))+1 > f.limit() {
		return fmt.Errorf("message is %d bytes, over the %d byte frame limit", len(data), f.limit())
	}
	if _, err := writer.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing frame: %w", err)
	}
	return nil
}

func readLimitedLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var out []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		out = append(out, chunk...)
		if len(out) > limit {
			return nil, fmt.Errorf("message exceeds the %d byte frame limit", limit)
		}
		if err == nil {
			return out, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) && len(out) > 0 {
			return out, nil
		}
		return nil, err
	}
}

func hasPrefixFold(b, prefix []byte) bool {
	if len(b) < len(prefix) {
		return false
	}
	return bytes.EqualFold(b[:len(prefix)], prefix)
}

type StdioTransport struct {
	reader  *bufio.Reader
	writer  io.Writer
	framer  LineFramer
	writeMu sync.Mutex
	closed  bool
	closeMu sync.Mutex
}

func WithFrameLimit(limit int64) StdioOption {
	return func(t *StdioTransport) {
		t.framer.Limit = limit
	}
}

type StdioOption func(*StdioTransport)

func NewStdioTransport(r io.Reader, w io.Writer, opts ...StdioOption) *StdioTransport {
	t := &StdioTransport{
		reader: bufio.NewReaderSize(r, 64*1024),
		writer: w,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

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

func (t *StdioTransport) Close() error {
	t.closeMu.Lock()
	defer t.closeMu.Unlock()
	t.closed = true
	return nil
}

func parseRawMessage(data []byte) (*Message, error) {
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

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
