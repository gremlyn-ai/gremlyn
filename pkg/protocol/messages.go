// Package protocol defines the MCP (Model Context Protocol) message types
// and JSON-RPC 2.0 envelope structures used throughout the Gremlyn proxy engine.
package protocol

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// JSONRPCVersion is the JSON-RPC protocol version used by MCP.
const JSONRPCVersion = "2.0"

// MessageType identifies whether a JSON-RPC message is a request, response, or notification.
type MessageType string

const (
	// MessageTypeRequest is a JSON-RPC request (has id and method).
	MessageTypeRequest MessageType = "request"
	// MessageTypeResponse is a JSON-RPC response (has id and result or error).
	MessageTypeResponse MessageType = "response"
	// MessageTypeNotification is a JSON-RPC notification (has method but no id).
	MessageTypeNotification MessageType = "notification"
)

// MCPMethod represents known MCP protocol method names.
type MCPMethod string

const (
	// MCPMethodToolsList is the method for listing available tools.
	MCPMethodToolsList MCPMethod = "tools/list"
	// MCPMethodToolsCall is the method for calling a tool.
	MCPMethodToolsCall MCPMethod = "tools/call"
	// MCPMethodSamplingCreateMessage is the method for server-initiated LLM sampling.
	MCPMethodSamplingCreateMessage MCPMethod = "sampling/createMessage"
	// MCPMethodInitialize is the initialization handshake method.
	MCPMethodInitialize MCPMethod = "initialize"
	// MCPMethodPing is the ping method.
	MCPMethodPing MCPMethod = "ping"
	// MCPMethodResourcesList is the method for listing resources.
	MCPMethodResourcesList MCPMethod = "resources/list"
	// MCPMethodResourcesRead is the method for reading a resource.
	MCPMethodResourcesRead MCPMethod = "resources/read"
	// MCPMethodPromptsList is the method for listing prompts.
	MCPMethodPromptsList MCPMethod = "prompts/list"
	// MCPMethodPromptsGet is the method for getting a prompt.
	MCPMethodPromptsGet MCPMethod = "prompts/get"
	// MCPMethodNotificationInitialized signals that initialization is complete.
	MCPMethodNotificationInitialized MCPMethod = "notifications/initialized"
	// MCPMethodNotificationProgress reports progress on a long-running operation.
	MCPMethodNotificationProgress MCPMethod = "notifications/progress"
	// MCPMethodNotificationCancelled signals that a request was cancelled.
	MCPMethodNotificationCancelled MCPMethod = "notifications/cancelled"
	// MCPMethodUnknown is used for unrecognized methods.
	MCPMethodUnknown MCPMethod = ""
)

// ParseMethod converts a raw method string into a typed MCPMethod.
// Returns MCPMethodUnknown for unrecognized methods.
func ParseMethod(method string) MCPMethod {
	switch MCPMethod(method) {
	case MCPMethodToolsList, MCPMethodToolsCall, MCPMethodSamplingCreateMessage,
		MCPMethodInitialize, MCPMethodPing,
		MCPMethodResourcesList, MCPMethodResourcesRead,
		MCPMethodPromptsList, MCPMethodPromptsGet,
		MCPMethodNotificationInitialized, MCPMethodNotificationProgress, MCPMethodNotificationCancelled:
		return MCPMethod(method)
	default:
		return MCPMethodUnknown
	}
}

// JSONRPCID represents a JSON-RPC request identifier which can be either a string or an integer.
// The proxy must preserve the original type exactly for response correlation.
type JSONRPCID struct {
	IsString  bool
	StringVal string
	IntVal    int64
	isSet     bool
}

// NewStringID creates a JSONRPCID with a string value.
func NewStringID(s string) JSONRPCID {
	return JSONRPCID{IsString: true, StringVal: s, isSet: true}
}

// NewIntID creates a JSONRPCID with an integer value.
func NewIntID(n int64) JSONRPCID {
	return JSONRPCID{IsString: false, IntVal: n, isSet: true}
}

// IsSet returns true if the ID has been explicitly set.
func (id JSONRPCID) IsSet() bool {
	return id.isSet
}

// String returns a human-readable representation of the ID.
func (id JSONRPCID) String() string {
	if !id.isSet {
		return "<unset>"
	}
	if id.IsString {
		return id.StringVal
	}
	return strconv.FormatInt(id.IntVal, 10)
}

// MarshalJSON implements json.Marshaler for JSONRPCID.
func (id JSONRPCID) MarshalJSON() ([]byte, error) {
	if !id.isSet {
		return []byte("null"), nil
	}
	if id.IsString {
		return json.Marshal(id.StringVal)
	}
	return json.Marshal(id.IntVal)
}

// UnmarshalJSON implements json.Unmarshaler for JSONRPCID.
func (id *JSONRPCID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		id.isSet = false
		return nil
	}

	// Try integer first.
	var intVal int64
	if err := json.Unmarshal(data, &intVal); err == nil {
		id.IsString = false
		id.IntVal = intVal
		id.isSet = true
		return nil
	}

	// Try string.
	var strVal string
	if err := json.Unmarshal(data, &strVal); err == nil {
		id.IsString = true
		id.StringVal = strVal
		id.isSet = true
		return nil
	}

	return fmt.Errorf("jsonrpc id must be a string or integer, got: %s", string(data))
}

// JSONRPCRequest represents a JSON-RPC 2.0 request message.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      JSONRPCID       `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response message.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      JSONRPCID       `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// JSONRPCNotification represents a JSON-RPC 2.0 notification (request without an id).
type JSONRPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Message is a discriminated union wrapper that holds exactly one JSON-RPC message type.
type Message struct {
	Type         MessageType          `json:"type"`
	Request      *JSONRPCRequest      `json:"request,omitempty"`
	Response     *JSONRPCResponse     `json:"response,omitempty"`
	Notification *JSONRPCNotification `json:"notification,omitempty"`
	// Raw preserves the original bytes for zero-loss round-tripping of unknown fields.
	Raw json.RawMessage `json:"-"`
}

// GetMethod returns the method name from the message, if applicable.
func (m *Message) GetMethod() string {
	switch m.Type {
	case MessageTypeRequest:
		if m.Request != nil {
			return m.Request.Method
		}
	case MessageTypeResponse:
		// Responses do not carry a method name.
	case MessageTypeNotification:
		if m.Notification != nil {
			return m.Notification.Method
		}
	}
	return ""
}

// GetID returns the JSONRPCID from the message, if applicable.
func (m *Message) GetID() (JSONRPCID, bool) {
	switch m.Type {
	case MessageTypeRequest:
		if m.Request != nil {
			return m.Request.ID, true
		}
	case MessageTypeResponse:
		if m.Response != nil {
			return m.Response.ID, true
		}
	case MessageTypeNotification:
		// Notifications do not carry an ID.
	}
	return JSONRPCID{}, false
}

// --- MCP-specific param/result types ---

// ToolDefinition describes a single tool exposed by an MCP server.
type ToolDefinition struct {
	Name        string          `json:"name" yaml:"name"`
	Description string          `json:"description" yaml:"description"`
	InputSchema json.RawMessage `json:"inputSchema" yaml:"inputSchema"`
}

// ToolsListParams represents the parameters for a tools/list request.
type ToolsListParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// ToolsListResult represents the result of a tools/list response.
type ToolsListResult struct {
	Tools      []ToolDefinition `json:"tools"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

// ToolsCallParams represents the parameters for a tools/call request.
type ToolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolsCallResult represents the result of a tools/call response.
type ToolsCallResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock represents a content block in MCP tool results or sampling messages.
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
	URI      string `json:"uri,omitempty"`
}

// SamplingCreateMessageParams represents the parameters for a sampling/createMessage request.
type SamplingCreateMessageParams struct {
	Messages         []SamplingMessage `json:"messages"`
	ModelPreferences *ModelPreferences `json:"modelPreferences,omitempty"`
	SystemPrompt     string            `json:"systemPrompt,omitempty"`
	MaxTokens        int               `json:"maxTokens"`
}

// SamplingMessage represents a single message in a sampling request.
type SamplingMessage struct {
	Role    string       `json:"role"`
	Content ContentBlock `json:"content"`
}

// ModelPreferences describes model selection preferences for sampling.
type ModelPreferences struct {
	Hints                []ModelHint `json:"hints,omitempty"`
	CostPriority         float64     `json:"costPriority,omitempty"`
	SpeedPriority        float64     `json:"speedPriority,omitempty"`
	IntelligencePriority float64     `json:"intelligencePriority,omitempty"`
}

// ModelHint provides a hint about which model to use for sampling.
type ModelHint struct {
	Name string `json:"name,omitempty"`
}

// SamplingCreateMessageResult represents the result of a sampling/createMessage response.
type SamplingCreateMessageResult struct {
	Role       string       `json:"role"`
	Content    ContentBlock `json:"content"`
	Model      string       `json:"model"`
	StopReason string       `json:"stopReason,omitempty"`
}

// --- Notification param types ---

// ProgressNotificationParams represents the parameters for a progress notification.
type ProgressNotificationParams struct {
	ProgressToken string  `json:"progressToken"`
	Progress      float64 `json:"progress"`
	Total         float64 `json:"total,omitempty"`
}

// CancelledNotificationParams represents the parameters for a cancellation notification.
type CancelledNotificationParams struct {
	RequestID JSONRPCID `json:"requestId"`
	Reason    string    `json:"reason,omitempty"`
}

// InitializeParams represents the parameters for an initialize request.
type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      Implementation     `json:"clientInfo"`
}

// InitializeResult represents the result of an initialize response.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      Implementation     `json:"serverInfo"`
}

// ClientCapabilities describes what the MCP client supports.
type ClientCapabilities struct {
	Roots    *RootsCapability    `json:"roots,omitempty"`
	Sampling *SamplingCapability `json:"sampling,omitempty"`
}

// ServerCapabilities describes what the MCP server supports.
type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
}

// RootsCapability indicates support for root listing.
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// SamplingCapability indicates support for sampling.
type SamplingCapability struct{}

// ToolsCapability indicates support for tools.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability indicates support for resources.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability indicates support for prompts.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// Implementation identifies an MCP client or server.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
