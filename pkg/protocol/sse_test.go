package protocol

import (
	"bytes"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readAllEvents(t *testing.T, stream string, limit int64) ([]*SSEEvent, error) {
	t.Helper()
	r := NewSSEReader(strings.NewReader(stream), limit)
	var out []*SSEEvent
	for {
		ev, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, ev)
	}
}

func TestSSEReader(t *testing.T) {
	tests := []struct {
		name      string
		stream    string
		limit     int64
		wantData  []string
		wantTypes []string
		wantErr   error
	}{
		{
			name:      "one event",
			stream:    "data: {\"a\":1}\n\n",
			wantData:  []string{`{"a":1}`},
			wantTypes: []string{"message"},
		},
		{
			name:      "multi-line data is joined with a newline",
			stream:    "data: {\"a\":\ndata: 1}\n\n",
			wantData:  []string{"{\"a\":\n1}"},
			wantTypes: []string{"message"},
		},
		{
			name:      "named event with an id",
			stream:    "id: 7\nevent: endpoint\ndata: /messages?s=1\n\n",
			wantData:  []string{"/messages?s=1"},
			wantTypes: []string{"endpoint"},
		},
		{
			name:      "CRLF line endings",
			stream:    "data: x\r\n\r\ndata: y\r\n\r\n",
			wantData:  []string{"x", "y"},
			wantTypes: []string{"message", "message"},
		},
		{
			name:      "bare CR line endings",
			stream:    "data: x\r\rdata: y\rdata: z\r\r",
			wantData:  []string{"x", "y\nz"},
			wantTypes: []string{"message", "message"},
		},
		{
			name:      "mixed CR, LF and CRLF",
			stream:    "id: 1\rdata: a\r\ndata: b\n\r\n",
			wantData:  []string{"a\nb"},
			wantTypes: []string{"message"},
		},
		{
			name:      "a CR at the end of the stream",
			stream:    "data: x\r",
			wantData:  []string{"x"},
			wantTypes: []string{"message"},
		},
		{
			name:      "leading blank lines and a BOM are skipped",
			stream:    "\xef\xbb\xbf\n\ndata: x\n\n",
			wantData:  []string{"x"},
			wantTypes: []string{"message"},
		},
		{
			name:      "a comment-only event is still an event",
			stream:    ": keepalive\n\ndata: x\n\n",
			wantData:  []string{"", "x"},
			wantTypes: []string{"message", "message"},
		},
		{
			name:      "a partial event at EOF is delivered, not dropped",
			stream:    "data: x\n\ndata: tail",
			wantData:  []string{"x", "tail"},
			wantTypes: []string{"message", "message"},
		},
		{
			name:     "an event at the limit passes",
			stream:   "data: 12345678\n\n",
			limit:    8,
			wantData: []string{"12345678"},
		},
		{
			name:    "an event over the limit is refused",
			stream:  "data: 123456789\n\n",
			limit:   8,
			wantErr: ErrSSEEventTooLarge,
		},
		{
			name:    "many data lines over the limit are refused",
			stream:  "data: 1234\ndata: 1234\n\n",
			limit:   8,
			wantErr: ErrSSEEventTooLarge,
		},
		{
			name:    "a comment flood is bounded too",
			stream:  strings.Repeat(":\n", MaxSSEFieldsPerEvent+1) + "\n",
			limit:   1 << 20,
			wantErr: ErrSSEEventTooLarge,
		},
		{
			name:    "bare data lines count against the event budget",
			stream:  strings.Repeat("data\n", 1<<20) + "\n",
			wantErr: ErrSSEEventTooLarge,
		},
		{
			name:     "fields up to the per-event cap pass",
			stream:   strings.Repeat("data\n", MaxSSEFieldsPerEvent) + "\n",
			wantData: []string{strings.Repeat("\n", MaxSSEFieldsPerEvent-1)},
		},
		{
			name:    "one line far over the limit is refused without reading it whole",
			stream:  "data: " + strings.Repeat("x", 8192) + "\n\n",
			limit:   16,
			wantErr: ErrSSEEventTooLarge,
		},
		{
			name:   "an empty stream ends cleanly",
			stream: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := readAllEvents(t, tt.stream, tt.limit)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, events, len(tt.wantData))
			for i, ev := range events {
				data, _ := ev.Data()
				assert.Equal(t, tt.wantData[i], string(data))
				if tt.wantTypes != nil {
					assert.Equal(t, tt.wantTypes[i], ev.Type())
				}
			}
		})
	}
}

func TestSSE_RoundTripIsByteExact(t *testing.T) {
	stream := "id: 42\nevent: message\nretry: 1000\n: note\ndata: {\"jsonrpc\":\"2.0\",  \"id\":1}\ndata:  second\n\n"
	events, err := readAllEvents(t, stream, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)

	var buf bytes.Buffer
	require.NoError(t, WriteSSEEvent(&buf, events[0]))
	assert.Equal(t, stream, buf.String())
}

func TestSSEEvent_WithData(t *testing.T) {
	ev := &SSEEvent{Fields: []SSEField{
		{Name: "id", Value: "9"},
		{Name: "data", Value: "old"},
		{Name: "data", Value: "old2"},
		{Name: "", Value: " c"},
	}}
	replaced := ev.WithData([]byte("a\nb"))

	var buf bytes.Buffer
	require.NoError(t, WriteSSEEvent(&buf, &replaced))
	assert.Equal(t, "id: 9\ndata: a\ndata: b\n: c\n\n", buf.String())

	noData := &SSEEvent{Fields: []SSEField{{Name: "id", Value: "1"}}}
	added := noData.WithData([]byte("x"))
	got, ok := added.Data()
	require.True(t, ok)
	assert.Equal(t, "x", string(got))
}

func TestWriteSSEEvent_RefusesLineBreaks(t *testing.T) {
	tests := []SSEEvent{
		{Fields: []SSEField{{Name: "data", Value: "a\nb"}}},
		{Fields: []SSEField{{Name: "da\nta", Value: "a"}}},
		{Fields: []SSEField{{Name: "", Value: "a\rb"}}},
	}
	for i := range tests {
		require.Error(t, WriteSSEEvent(io.Discard, &tests[i]))
	}
}

func TestParseMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType MessageType
		wantErr  error
		anyErr   bool
	}{
		{"request", `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`, MessageTypeRequest, nil, false},
		{"notification", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, MessageTypeNotification, nil, false},
		{"response", `{"jsonrpc":"2.0","id":"a","result":{}}`, MessageTypeResponse, nil, false},
		{"error response with a null id", `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"x"}}`, MessageTypeResponse, nil, false},
		{"batch", `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, "", ErrBatch, true},
		{"batch after whitespace", " \n [ ]", "", ErrBatch, true},
		{"missing id and method", `{"jsonrpc":"2.0","result":{}}`, "", nil, true},
		{"truncated", `{"jsonrpc":"2.0","id":1,"meth`, "", nil, true},
		{"empty", ``, "", nil, true},
		{"non-UTF8 bytes inside a string", "{\"jsonrpc\":\"2.0\",\"method\":\"x\xff\"}", MessageTypeNotification, nil, false},
		{"wrong version is parsed, not judged", `{"jsonrpc":"1.0","id":1,"method":"ping"}`, MessageTypeRequest, nil, false},
		{"huge payload", `{"jsonrpc":"2.0","id":1,"result":{"t":"` + strings.Repeat("x", 1<<20) + `"}}`, MessageTypeResponse, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := ParseMessage([]byte(tt.input))
			if tt.anyErr {
				require.Error(t, err)
				if tt.wantErr != nil {
					assert.ErrorIs(t, err, tt.wantErr)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantType, msg.Type)
			assert.Equal(t, tt.input, string(msg.Raw), "the raw bytes must be kept for a byte-exact forward")
		})
	}
}

func TestEncodeMessage_PreservesID(t *testing.T) {
	msg, err := ParseMessage([]byte(`{"jsonrpc":"2.0","id":"abc","result":{"ok":true}}`))
	require.NoError(t, err)
	out, err := EncodeMessage(msg)
	require.NoError(t, err)
	assert.JSONEq(t, `{"jsonrpc":"2.0","id":"abc","result":{"ok":true}}`, string(out))
}

func TestSSEReader_BareFieldFloodStaysSmall(t *testing.T) {
	stream := strings.Repeat("data\n", 4<<20) + "\n"
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := NewSSEReader(strings.NewReader(stream), 0).Next()
	runtime.ReadMemStats(&after)
	require.ErrorIs(t, err, ErrSSEEventTooLarge)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(8<<20),
		"a flood of empty fields must be refused before it is held in memory")
}

func TestParseMessage_ErrorNeverEchoesThePeersKeys(t *testing.T) {
	_, err := ParseMessage([]byte(`{"jsonrpc":"2.0","attacker_chosen_key":1}`))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "attacker_chosen_key")
}
