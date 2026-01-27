package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

var (
	ErrStdioClosed = errors.New("stdio transport closed")
)

// StdioTransport implements Transport over stdin/stdout.
// Messages are JSON-RPC style with Content-Length headers.
type StdioTransport struct {
	reader    *bufio.Reader
	writer    io.Writer
	mu        sync.Mutex
	closed    bool
	closeCh   chan struct{}
	connected bool
}

// NewStdioTransport creates a new stdio transport.
func NewStdioTransport(r io.Reader, w io.Writer) *StdioTransport {
	return &StdioTransport{
		reader:  bufio.NewReader(r),
		writer:  w,
		closeCh: make(chan struct{}),
	}
}

// Connect marks the transport as connected.
func (t *StdioTransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return ErrStdioClosed
	}

	t.connected = true
	return nil
}

// Close closes the transport.
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}

	t.closed = true
	t.connected = false
	close(t.closeCh)
	return nil
}

// Send sends a message over stdout using LSP-style framing.
func (t *StdioTransport) Send(ctx context.Context, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return ErrStdioClosed
	}

	// Write Content-Length header followed by message
	header := []byte("Content-Length: ")
	length := []byte(itoa(len(data)))
	separator := []byte("\r\n\r\n")

	if _, err := t.writer.Write(header); err != nil {
		return err
	}
	if _, err := t.writer.Write(length); err != nil {
		return err
	}
	if _, err := t.writer.Write(separator); err != nil {
		return err
	}
	if _, err := t.writer.Write(data); err != nil {
		return err
	}

	return nil
}

// Receive reads a message from stdin using LSP-style framing.
func (t *StdioTransport) Receive(ctx context.Context) ([]byte, error) {
	// Check for cancellation or close
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.closeCh:
		return nil, ErrStdioClosed
	default:
	}

	// Read Content-Length header
	contentLength := -1
	for {
		line, err := t.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil, ErrStdioClosed
			}
			return nil, err
		}

		// Trim CRLF
		line = trimCRLF(line)

		// Empty line marks end of headers
		if line == "" {
			break
		}

		// Parse Content-Length header
		if len(line) > 16 && line[:16] == "Content-Length: " {
			contentLength = atoi(line[16:])
		}
	}

	if contentLength < 0 {
		return nil, errors.New("missing Content-Length header")
	}

	// Read message body
	body := make([]byte, contentLength)
	_, err := io.ReadFull(t.reader, body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

// IsConnected returns true if the transport is connected.
func (t *StdioTransport) IsConnected() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.connected && !t.closed
}

// RemoteAddr returns "stdio" as the address.
func (t *StdioTransport) RemoteAddr() string {
	return "stdio"
}

// LineTransport implements Transport over stdin/stdout with newline framing.
// Each message is a single JSON line.
type LineTransport struct {
	reader    *bufio.Reader
	writer    io.Writer
	mu        sync.Mutex
	closed    bool
	closeCh   chan struct{}
	connected bool
}

// NewLineTransport creates a new line-delimited transport.
func NewLineTransport(r io.Reader, w io.Writer) *LineTransport {
	return &LineTransport{
		reader:  bufio.NewReader(r),
		writer:  w,
		closeCh: make(chan struct{}),
	}
}

// Connect marks the transport as connected.
func (t *LineTransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return ErrStdioClosed
	}

	t.connected = true
	return nil
}

// Close closes the transport.
func (t *LineTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}

	t.closed = true
	t.connected = false
	close(t.closeCh)
	return nil
}

// Send sends a JSON message as a single line.
func (t *LineTransport) Send(ctx context.Context, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return ErrStdioClosed
	}

	// Compact JSON to single line and add newline
	var compact []byte
	if json.Valid(data) {
		buf := make([]byte, 0, len(data))
		for i := 0; i < len(data); i++ {
			c := data[i]
			// Skip whitespace outside strings
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				continue
			}
			buf = append(buf, c)
		}
		compact = buf
	} else {
		compact = data
	}

	if _, err := t.writer.Write(compact); err != nil {
		return err
	}
	if _, err := t.writer.Write([]byte("\n")); err != nil {
		return err
	}

	return nil
}

// Receive reads a JSON message from a single line.
func (t *LineTransport) Receive(ctx context.Context) ([]byte, error) {
	// Check for cancellation or close
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.closeCh:
		return nil, ErrStdioClosed
	default:
	}

	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		if err == io.EOF {
			return nil, ErrStdioClosed
		}
		return nil, err
	}

	// Trim trailing newline
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}

	return line, nil
}

// IsConnected returns true if the transport is connected.
func (t *LineTransport) IsConnected() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.connected && !t.closed
}

// RemoteAddr returns "stdio" as the address.
func (t *LineTransport) RemoteAddr() string {
	return "stdio"
}

// Helper functions

func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	negative := n < 0
	if negative {
		n = -n
	}

	buf := make([]byte, 20)
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte(n%10) + '0'
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func atoi(s string) int {
	result := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			result = result*10 + int(c-'0')
		}
	}
	return result
}

func trimCRLF(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\r' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}
