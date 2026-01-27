package transport

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
)

// TestNewStdioTransport tests transport creation.
func TestNewStdioTransport(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}

	transport := NewStdioTransport(r, w)
	if transport == nil {
		t.Fatal("NewStdioTransport returned nil")
	}
	if transport.reader == nil {
		t.Error("reader should be set")
	}
	if transport.writer == nil {
		t.Error("writer should be set")
	}
}

// TestStdioTransportConnect tests connection.
func TestStdioTransportConnect(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewStdioTransport(r, w)

	if transport.IsConnected() {
		t.Error("should not be connected initially")
	}

	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if !transport.IsConnected() {
		t.Error("should be connected after Connect")
	}
}

// TestStdioTransportClose tests closing.
func TestStdioTransportClose(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewStdioTransport(r, w)

	transport.Connect(context.Background())

	if err := transport.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if transport.IsConnected() {
		t.Error("should not be connected after Close")
	}

	// Double close should be safe
	if err := transport.Close(); err != nil {
		t.Errorf("double Close should not error: %v", err)
	}
}

// TestStdioTransportSend tests sending messages.
func TestStdioTransportSend(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewStdioTransport(r, w)
	transport.Connect(context.Background())

	msg := []byte(`{"method":"test","params":{}}`)
	if err := transport.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	expected := "Content-Length: 29\r\n\r\n" + string(msg)
	if w.String() != expected {
		t.Errorf("output = %q, want %q", w.String(), expected)
	}
}

// TestStdioTransportSendClosed tests sending on closed transport.
func TestStdioTransportSendClosed(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewStdioTransport(r, w)
	transport.Close()

	err := transport.Send(context.Background(), []byte("test"))
	if err != ErrStdioClosed {
		t.Errorf("expected ErrStdioClosed, got %v", err)
	}
}

// TestStdioTransportReceive tests receiving messages.
func TestStdioTransportReceive(t *testing.T) {
	msg := `{"method":"test"}`
	input := "Content-Length: 17\r\n\r\n" + msg

	r := strings.NewReader(input)
	w := &bytes.Buffer{}
	transport := NewStdioTransport(r, w)
	transport.Connect(context.Background())

	data, err := transport.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	if string(data) != msg {
		t.Errorf("received = %q, want %q", string(data), msg)
	}
}

// TestStdioTransportReceiveMultiple tests receiving multiple messages.
func TestStdioTransportReceiveMultiple(t *testing.T) {
	msg1 := `{"id":1}`
	msg2 := `{"id":2}`
	input := "Content-Length: 8\r\n\r\n" + msg1 +
		"Content-Length: 8\r\n\r\n" + msg2

	r := strings.NewReader(input)
	w := &bytes.Buffer{}
	transport := NewStdioTransport(r, w)
	transport.Connect(context.Background())

	// Receive first message
	data1, err := transport.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive 1 failed: %v", err)
	}
	if string(data1) != msg1 {
		t.Errorf("received 1 = %q, want %q", string(data1), msg1)
	}

	// Receive second message
	data2, err := transport.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive 2 failed: %v", err)
	}
	if string(data2) != msg2 {
		t.Errorf("received 2 = %q, want %q", string(data2), msg2)
	}
}

// TestStdioTransportReceiveClosed tests receiving on closed transport.
func TestStdioTransportReceiveClosed(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewStdioTransport(r, w)
	transport.Close()

	_, err := transport.Receive(context.Background())
	if err != ErrStdioClosed {
		t.Errorf("expected ErrStdioClosed, got %v", err)
	}
}

// TestStdioTransportReceiveEOF tests receiving when EOF is reached.
func TestStdioTransportReceiveEOF(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewStdioTransport(r, w)
	transport.Connect(context.Background())

	_, err := transport.Receive(context.Background())
	if err != ErrStdioClosed {
		t.Errorf("expected ErrStdioClosed on EOF, got %v", err)
	}
}

// TestStdioTransportReceiveMissingHeader tests error on missing Content-Length.
func TestStdioTransportReceiveMissingHeader(t *testing.T) {
	input := "Invalid-Header: value\r\n\r\nsome body"
	r := strings.NewReader(input)
	w := &bytes.Buffer{}
	transport := NewStdioTransport(r, w)
	transport.Connect(context.Background())

	_, err := transport.Receive(context.Background())
	if err == nil {
		t.Error("expected error for missing Content-Length")
	}
}

// TestStdioTransportRemoteAddr tests remote address.
func TestStdioTransportRemoteAddr(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewStdioTransport(r, w)

	if transport.RemoteAddr() != "stdio" {
		t.Errorf("RemoteAddr = %q, want %q", transport.RemoteAddr(), "stdio")
	}
}

// TestStdioTransportConcurrent tests concurrent send operations.
func TestStdioTransportConcurrent(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewStdioTransport(r, w)
	transport.Connect(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			msg := []byte(`{"n":` + itoa(n) + `}`)
			transport.Send(context.Background(), msg)
		}(i)
	}
	wg.Wait()

	// Just verify no panics occurred
	if w.Len() == 0 {
		t.Error("expected some output")
	}
}

// TestNewLineTransport tests line transport creation.
func TestNewLineTransport(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}

	transport := NewLineTransport(r, w)
	if transport == nil {
		t.Fatal("NewLineTransport returned nil")
	}
}

// TestLineTransportConnect tests connection.
func TestLineTransportConnect(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewLineTransport(r, w)

	if transport.IsConnected() {
		t.Error("should not be connected initially")
	}

	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if !transport.IsConnected() {
		t.Error("should be connected after Connect")
	}
}

// TestLineTransportClose tests closing.
func TestLineTransportClose(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewLineTransport(r, w)

	transport.Connect(context.Background())
	transport.Close()

	if transport.IsConnected() {
		t.Error("should not be connected after Close")
	}
}

// TestLineTransportSend tests sending messages.
func TestLineTransportSend(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewLineTransport(r, w)
	transport.Connect(context.Background())

	msg := []byte(`{"method":"test"}`)
	if err := transport.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Should have newline at end
	output := w.String()
	if !strings.HasSuffix(output, "\n") {
		t.Error("output should end with newline")
	}
}

// TestLineTransportSendClosed tests sending on closed transport.
func TestLineTransportSendClosed(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewLineTransport(r, w)
	transport.Close()

	err := transport.Send(context.Background(), []byte("test"))
	if err != ErrStdioClosed {
		t.Errorf("expected ErrStdioClosed, got %v", err)
	}
}

// TestLineTransportReceive tests receiving messages.
func TestLineTransportReceive(t *testing.T) {
	input := `{"method":"test"}` + "\n"
	r := strings.NewReader(input)
	w := &bytes.Buffer{}
	transport := NewLineTransport(r, w)
	transport.Connect(context.Background())

	data, err := transport.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	expected := `{"method":"test"}`
	if string(data) != expected {
		t.Errorf("received = %q, want %q", string(data), expected)
	}
}

// TestLineTransportReceiveMultiple tests receiving multiple lines.
func TestLineTransportReceiveMultiple(t *testing.T) {
	input := `{"id":1}` + "\n" + `{"id":2}` + "\n"
	r := strings.NewReader(input)
	w := &bytes.Buffer{}
	transport := NewLineTransport(r, w)
	transport.Connect(context.Background())

	data1, _ := transport.Receive(context.Background())
	data2, _ := transport.Receive(context.Background())

	if string(data1) != `{"id":1}` {
		t.Errorf("data1 = %q, want %q", string(data1), `{"id":1}`)
	}
	if string(data2) != `{"id":2}` {
		t.Errorf("data2 = %q, want %q", string(data2), `{"id":2}`)
	}
}

// TestLineTransportReceiveCRLF tests receiving with CRLF endings.
func TestLineTransportReceiveCRLF(t *testing.T) {
	input := `{"test":true}` + "\r\n"
	r := strings.NewReader(input)
	w := &bytes.Buffer{}
	transport := NewLineTransport(r, w)
	transport.Connect(context.Background())

	data, err := transport.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	expected := `{"test":true}`
	if string(data) != expected {
		t.Errorf("received = %q, want %q", string(data), expected)
	}
}

// TestLineTransportReceiveEOF tests receiving at EOF.
func TestLineTransportReceiveEOF(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewLineTransport(r, w)
	transport.Connect(context.Background())

	_, err := transport.Receive(context.Background())
	if err != ErrStdioClosed {
		t.Errorf("expected ErrStdioClosed on EOF, got %v", err)
	}
}

// TestLineTransportRemoteAddr tests remote address.
func TestLineTransportRemoteAddr(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewLineTransport(r, w)

	if transport.RemoteAddr() != "stdio" {
		t.Errorf("RemoteAddr = %q, want %q", transport.RemoteAddr(), "stdio")
	}
}

// TestItoaAtoi tests integer conversion helpers.
func TestItoaAtoi(t *testing.T) {
	tests := []struct {
		n int
		s string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{100, "100"},
		{12345, "12345"},
	}

	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			// Test itoa
			s := itoa(tt.n)
			if s != tt.s {
				t.Errorf("itoa(%d) = %q, want %q", tt.n, s, tt.s)
			}

			// Test atoi
			n := atoi(tt.s)
			if n != tt.n {
				t.Errorf("atoi(%q) = %d, want %d", tt.s, n, tt.n)
			}
		})
	}
}

// TestTrimCRLF tests CRLF trimming.
func TestTrimCRLF(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello\r\n", "hello"},
		{"hello\n", "hello"},
		{"hello\r", "hello"},
		{"hello", "hello"},
		{"\r\n", ""},
		{"", ""},
	}

	for _, tt := range tests {
		result := trimCRLF(tt.input)
		if result != tt.expected {
			t.Errorf("trimCRLF(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestStdioTransportRoundtrip tests sending and receiving.
func TestStdioTransportRoundtrip(t *testing.T) {
	// Create a pipe for bidirectional communication
	pr, pw := io.Pipe()

	// Writer transport writes to pipe
	buf := &bytes.Buffer{}
	sender := NewStdioTransport(strings.NewReader(""), pw)
	sender.Connect(context.Background())

	// Receiver transport reads from pipe
	receiver := NewStdioTransport(pr, buf)
	receiver.Connect(context.Background())

	// Send in goroutine
	msg := []byte(`{"hello":"world"}`)
	go func() {
		sender.Send(context.Background(), msg)
	}()

	// Receive
	data, err := receiver.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	if string(data) != string(msg) {
		t.Errorf("roundtrip: got %q, want %q", string(data), string(msg))
	}
}

// TestStdioConnectClosed tests connecting after close.
func TestStdioConnectClosed(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewStdioTransport(r, w)
	transport.Close()

	err := transport.Connect(context.Background())
	if err != ErrStdioClosed {
		t.Errorf("expected ErrStdioClosed, got %v", err)
	}
}

// TestLineConnectClosed tests connecting after close.
func TestLineConnectClosed(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	transport := NewLineTransport(r, w)
	transport.Close()

	err := transport.Connect(context.Background())
	if err != ErrStdioClosed {
		t.Errorf("expected ErrStdioClosed, got %v", err)
	}
}
