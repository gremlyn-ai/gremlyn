package protocol

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrSSEEventTooLarge = errors.New("SSE event exceeds the size limit")

const (
	sseLineOverhead      = 1024
	sseFieldCost         = 64
	MaxSSEFieldsPerEvent = 4096
	sseEventSlack        = MaxSSEFieldsPerEvent*sseFieldCost + sseLineOverhead
)

type SSEField struct {
	Name  string
	Value string
}

type SSEEvent struct {
	Fields []SSEField
}

func (e *SSEEvent) Data() ([]byte, bool) {
	var (
		buf   bytes.Buffer
		found bool
	)
	for _, f := range e.Fields {
		if f.Name != "data" {
			continue
		}
		if found {
			buf.WriteByte('\n')
		}
		buf.WriteString(f.Value)
		found = true
	}
	return buf.Bytes(), found
}

func (e *SSEEvent) Type() string {
	typ := ""
	for _, f := range e.Fields {
		if f.Name == "event" {
			typ = f.Value
		}
	}
	if typ == "" {
		return "message"
	}
	return typ
}

func (e *SSEEvent) WithData(data []byte) SSEEvent {
	out := SSEEvent{Fields: make([]SSEField, 0, len(e.Fields)+1)}
	written := false
	for _, f := range e.Fields {
		if f.Name != "data" {
			out.Fields = append(out.Fields, f)
			continue
		}
		if written {
			continue
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			out.Fields = append(out.Fields, SSEField{Name: "data", Value: line})
		}
		written = true
	}
	if !written {
		for line := range strings.SplitSeq(string(data), "\n") {
			out.Fields = append(out.Fields, SSEField{Name: "data", Value: line})
		}
	}
	return out
}

type SSEReader struct {
	reader  *bufio.Reader
	limit   int64
	started bool
}

func NewSSEReader(r io.Reader, limit int64) *SSEReader {
	if limit <= 0 || limit > MaxLineBytes {
		limit = MaxLineBytes
	}
	return &SSEReader{reader: bufio.NewReaderSize(r, 64*1024), limit: limit}
}

func (s *SSEReader) Next() (*SSEEvent, error) {
	ev := &SSEEvent{}
	var dataBytes, eventBytes int64
	dataLines := 0
	for {
		line, err := readLimitedLine(s.reader, int(s.limit)+sseLineOverhead)
		if err != nil {
			if errors.Is(err, io.EOF) && len(ev.Fields) > 0 {
				return ev, nil
			}
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			if errors.Is(err, ErrFrameTooLarge) {
				return nil, fmt.Errorf("%w: a line is over %d bytes", ErrSSEEventTooLarge, s.limit)
			}
			return nil, fmt.Errorf("reading SSE stream: %w", err)
		}

		line = bytes.TrimSuffix(line, []byte("\n"))
		line = bytes.TrimSuffix(line, []byte("\r"))
		if !s.started {
			line = bytes.TrimPrefix(line, []byte("\xef\xbb\xbf"))
			s.started = true
		}

		if len(line) == 0 {
			if len(ev.Fields) == 0 {
				continue
			}
			return ev, nil
		}

		if len(ev.Fields) >= MaxSSEFieldsPerEvent {
			return nil, fmt.Errorf("%w: over %d fields", ErrSSEEventTooLarge, MaxSSEFieldsPerEvent)
		}
		field := parseSSEField(line)
		eventBytes += int64(len(line)) + 1 + sseFieldCost
		if field.Name == "data" {
			if dataLines > 0 {
				dataBytes++
			}
			dataLines++
			dataBytes += int64(len(field.Value))
		}
		if dataBytes > s.limit || eventBytes > s.limit+sseEventSlack {
			return nil, fmt.Errorf("%w: over %d bytes", ErrSSEEventTooLarge, s.limit)
		}
		ev.Fields = append(ev.Fields, field)
	}
}

func parseSSEField(line []byte) SSEField {
	if line[0] == ':' {
		return SSEField{Name: "", Value: string(line[1:])}
	}
	name, value, found := bytes.Cut(line, []byte(":"))
	if !found {
		return SSEField{Name: string(line)}
	}
	value = bytes.TrimPrefix(value, []byte(" "))
	return SSEField{Name: string(name), Value: string(value)}
}

func WriteSSEEvent(w io.Writer, ev *SSEEvent) error {
	var buf bytes.Buffer
	for _, f := range ev.Fields {
		if f.Name == "" {
			if strings.ContainsAny(f.Value, "\r\n") {
				return fmt.Errorf("SSE comment contains a line break")
			}
			buf.WriteByte(':')
			buf.WriteString(f.Value)
			buf.WriteByte('\n')
			continue
		}
		if strings.ContainsAny(f.Name, "\r\n:") || strings.ContainsAny(f.Value, "\r\n") {
			return fmt.Errorf("SSE field %q contains a line break or a colon", f.Name)
		}
		buf.WriteString(f.Name)
		buf.WriteString(": ")
		buf.WriteString(f.Value)
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("writing SSE event: %w", err)
	}
	return nil
}
