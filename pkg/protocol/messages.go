package protocol

import (
	"encoding/json"
	"fmt"
	"strconv"
)

const JSONRPCVersion = "2.0"

type MessageType string

const (
	MessageTypeRequest      MessageType = "request"
	MessageTypeResponse     MessageType = "response"
	MessageTypeNotification MessageType = "notification"
)

type MCPMethod string

const (
	MCPMethodToolsList               MCPMethod = "tools/list"
	MCPMethodToolsCall               MCPMethod = "tools/call"
	MCPMethodSamplingCreateMessage   MCPMethod = "sampling/createMessage"
	MCPMethodInitialize              MCPMethod = "initialize"
	MCPMethodPing                    MCPMethod = "ping"
	MCPMethodResourcesList           MCPMethod = "resources/list"
	MCPMethodResourcesRead           MCPMethod = "resources/read"
	MCPMethodPromptsList             MCPMethod = "prompts/list"
	MCPMethodPromptsGet              MCPMethod = "prompts/get"
	MCPMethodNotificationInitialized MCPMethod = "notifications/initialized"
	MCPMethodNotificationProgress    MCPMethod = "notifications/progress"
	MCPMethodNotificationCancelled   MCPMethod = "notifications/cancelled"
	MCPMethodUnknown                 MCPMethod = ""
)

type JSONRPCID struct {
	IsString  bool
	StringVal string
	IntVal    int64
	isSet     bool
}

func NewStringID(s string) JSONRPCID {
	return JSONRPCID{IsString: true, StringVal: s, isSet: true}
}

func NewIntID(n int64) JSONRPCID {
	return JSONRPCID{IsString: false, IntVal: n, isSet: true}
}

func (id JSONRPCID) IsSet() bool {
	return id.isSet
}

func (id JSONRPCID) String() string {
	if !id.isSet {
		return "<unset>"
	}
	if id.IsString {
		return id.StringVal
	}
	return strconv.FormatInt(id.IntVal, 10)
}

func (id JSONRPCID) MarshalJSON() ([]byte, error) {
	if !id.isSet {
		return []byte("null"), nil
	}
	if id.IsString {
		return json.Marshal(id.StringVal)
	}
	return json.Marshal(id.IntVal)
}

func (id *JSONRPCID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		id.isSet = false
		return nil
	}

	var intVal int64
	if err := json.Unmarshal(data, &intVal); err == nil {
		id.IsString = false
		id.IntVal = intVal
		id.isSet = true
		return nil
	}

	var strVal string
	if err := json.Unmarshal(data, &strVal); err == nil {
		id.IsString = true
		id.StringVal = strVal
		id.isSet = true
		return nil
	}

	return fmt.Errorf("jsonrpc id must be a string or integer, got: %s", string(data))
}

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      JSONRPCID       `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      JSONRPCID       `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type JSONRPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Message struct {
	Type         MessageType          `json:"type"`
	Request      *JSONRPCRequest      `json:"request,omitempty"`
	Response     *JSONRPCResponse     `json:"response,omitempty"`
	Notification *JSONRPCNotification `json:"notification,omitempty"`
	Raw          json.RawMessage      `json:"-"`
}

func (m *Message) GetMethod() string {
	switch m.Type {
	case MessageTypeRequest:
		if m.Request != nil {
			return m.Request.Method
		}
	case MessageTypeResponse:
	case MessageTypeNotification:
		if m.Notification != nil {
			return m.Notification.Method
		}
	}
	return ""
}

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
	}
	return JSONRPCID{}, false
}

type ToolDefinition struct {
	Name        string          `json:"name" yaml:"name"`
	Description string          `json:"description" yaml:"description"`
	InputSchema json.RawMessage `json:"inputSchema" yaml:"inputSchema"`
}

type ToolsListParams struct {
	Cursor string `json:"cursor,omitempty"`
}

type ToolsListResult struct {
	Tools      []ToolDefinition `json:"tools"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

type ToolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type ToolsCallResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
	URI      string `json:"uri,omitempty"`
}

type SamplingCreateMessageParams struct {
	Messages         []SamplingMessage `json:"messages"`
	ModelPreferences *ModelPreferences `json:"modelPreferences,omitempty"`
	SystemPrompt     string            `json:"systemPrompt,omitempty"`
	MaxTokens        int               `json:"maxTokens"`
}

type SamplingMessage struct {
	Role    string       `json:"role"`
	Content ContentBlock `json:"content"`
}

type ModelPreferences struct {
	Hints                []ModelHint `json:"hints,omitempty"`
	CostPriority         float64     `json:"costPriority,omitempty"`
	SpeedPriority        float64     `json:"speedPriority,omitempty"`
	IntelligencePriority float64     `json:"intelligencePriority,omitempty"`
}

type ModelHint struct {
	Name string `json:"name,omitempty"`
}

type SamplingCreateMessageResult struct {
	Role       string       `json:"role"`
	Content    ContentBlock `json:"content"`
	Model      string       `json:"model"`
	StopReason string       `json:"stopReason,omitempty"`
}

type ProgressNotificationParams struct {
	ProgressToken string  `json:"progressToken"`
	Progress      float64 `json:"progress"`
	Total         float64 `json:"total,omitempty"`
}

type CancelledNotificationParams struct {
	RequestID JSONRPCID `json:"requestId"`
	Reason    string    `json:"reason,omitempty"`
}

type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      Implementation     `json:"clientInfo"`
}

type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      Implementation     `json:"serverInfo"`
}

type ClientCapabilities struct {
	Roots    *RootsCapability    `json:"roots,omitempty"`
	Sampling *SamplingCapability `json:"sampling,omitempty"`
}

type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
}

type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type SamplingCapability struct{}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
