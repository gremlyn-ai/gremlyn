// Package proxy implements the core MCP proxy engine that intercepts
// JSON-RPC traffic between MCP clients and servers.
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

// Parser handles parsing and serialization of JSON-RPC 2.0 messages.
// It uses a two-phase approach: Phase 1 determines message type (fast),
// Phase 2 parses method-specific params/results (lazy, on demand).
type Parser struct{}

// NewParser creates a new JSON-RPC parser.
func NewParser() *Parser {
	return &Parser{}
}

// Parse takes raw JSON bytes and returns a slice of Messages.
// Returns a single-element slice for normal messages, multiple elements for batched requests.
func (p *Parser) Parse(data []byte) ([]*protocol.Message, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, &models.ErrInvalidMessage{Reason: "empty input", RawBytes: data}
	}

	if p.IsBatch(data) {
		return p.parseBatch(data)
	}

	msg, err := p.parseSingle(data)
	if err != nil {
		return nil, err
	}
	return []*protocol.Message{msg}, nil
}

// IsBatch returns true if the raw data starts with '[', indicating a JSON-RPC batch.
func (p *Parser) IsBatch(data []byte) bool {
	data = bytes.TrimSpace(data)
	return len(data) > 0 && data[0] == '['
}

// Serialize converts a single Message back to JSON bytes.
func (p *Parser) Serialize(msg *protocol.Message) ([]byte, error) {
	switch msg.Type {
	case protocol.MessageTypeRequest:
		if msg.Request == nil {
			return nil, fmt.Errorf("request message has nil request")
		}
		return json.Marshal(msg.Request)

	case protocol.MessageTypeResponse:
		if msg.Response == nil {
			return nil, fmt.Errorf("response message has nil response")
		}
		return json.Marshal(msg.Response)

	case protocol.MessageTypeNotification:
		if msg.Notification == nil {
			return nil, fmt.Errorf("notification message has nil notification")
		}
		return json.Marshal(msg.Notification)

	default:
		return nil, fmt.Errorf("unknown message type: %s", msg.Type)
	}
}

// SerializeBatch serializes multiple messages as a JSON array.
func (p *Parser) SerializeBatch(msgs []*protocol.Message) ([]byte, error) {
	items := make([]json.RawMessage, 0, len(msgs))
	for _, msg := range msgs {
		data, err := p.Serialize(msg)
		if err != nil {
			return nil, fmt.Errorf("serializing batch item: %w", err)
		}
		items = append(items, data)
	}
	return json.Marshal(items)
}

// ParseToolsCallParams extracts typed ToolsCallParams from raw JSON params.
func (p *Parser) ParseToolsCallParams(raw json.RawMessage) (*protocol.ToolsCallParams, error) {
	var params protocol.ToolsCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("parsing tools/call params: %w", err)
	}
	return &params, nil
}

// ParseToolsCallResult extracts typed ToolsCallResult from raw JSON result.
func (p *Parser) ParseToolsCallResult(raw json.RawMessage) (*protocol.ToolsCallResult, error) {
	var result protocol.ToolsCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parsing tools/call result: %w", err)
	}
	return &result, nil
}

// ParseToolsListResult extracts typed ToolsListResult from raw JSON result.
func (p *Parser) ParseToolsListResult(raw json.RawMessage) (*protocol.ToolsListResult, error) {
	var result protocol.ToolsListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parsing tools/list result: %w", err)
	}
	return &result, nil
}

// parseSingle parses a single JSON-RPC message from raw bytes.
func (p *Parser) parseSingle(data []byte) (*protocol.Message, error) {
	// Phase 1: detect message type by checking which fields are present.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, &models.ErrInvalidMessage{
			Reason:   fmt.Sprintf("invalid JSON: %v", err),
			RawBytes: data,
		}
	}

	_, hasID := raw["id"]
	_, hasMethod := raw["method"]
	_, hasResult := raw["result"]
	_, hasError := raw["error"]

	msg := &protocol.Message{Raw: data}

	switch {
	case hasMethod && hasID:
		// Could be request or notification with null id.
		if string(raw["id"]) == "null" {
			return p.parseAsNotification(data, msg)
		}
		return p.parseAsRequest(data, msg)

	case hasMethod && !hasID:
		return p.parseAsNotification(data, msg)

	case (hasResult || hasError) && hasID:
		return p.parseAsResponse(data, msg)

	default:
		return nil, &models.ErrInvalidMessage{
			Reason:   "cannot determine JSON-RPC message type",
			RawBytes: data,
		}
	}
}

func (p *Parser) parseAsRequest(data []byte, msg *protocol.Message) (*protocol.Message, error) {
	var req protocol.JSONRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, &models.ErrInvalidMessage{
			Reason:   fmt.Sprintf("parse request: %v", err),
			RawBytes: data,
		}
	}
	msg.Type = protocol.MessageTypeRequest
	msg.Request = &req
	return msg, nil
}

func (p *Parser) parseAsResponse(data []byte, msg *protocol.Message) (*protocol.Message, error) {
	var resp protocol.JSONRPCResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, &models.ErrInvalidMessage{
			Reason:   fmt.Sprintf("parse response: %v", err),
			RawBytes: data,
		}
	}
	msg.Type = protocol.MessageTypeResponse
	msg.Response = &resp
	return msg, nil
}

func (p *Parser) parseAsNotification(data []byte, msg *protocol.Message) (*protocol.Message, error) {
	var notif protocol.JSONRPCNotification
	if err := json.Unmarshal(data, &notif); err != nil {
		return nil, &models.ErrInvalidMessage{
			Reason:   fmt.Sprintf("parse notification: %v", err),
			RawBytes: data,
		}
	}
	msg.Type = protocol.MessageTypeNotification
	msg.Notification = &notif
	return msg, nil
}

func (p *Parser) parseBatch(data []byte) ([]*protocol.Message, error) {
	var rawMsgs []json.RawMessage
	if err := json.Unmarshal(data, &rawMsgs); err != nil {
		return nil, &models.ErrInvalidMessage{
			Reason:   fmt.Sprintf("invalid batch JSON: %v", err),
			RawBytes: data,
		}
	}

	if len(rawMsgs) == 0 {
		return []*protocol.Message{}, nil
	}

	msgs := make([]*protocol.Message, 0, len(rawMsgs))
	for i, raw := range rawMsgs {
		msg, err := p.parseSingle(raw)
		if err != nil {
			return nil, fmt.Errorf("batch item %d: %w", i, err)
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}
