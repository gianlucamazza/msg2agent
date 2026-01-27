package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/gianluca/msg2agent/pkg/transport"
)

// mockTransport is a test transport that simulates network communication.
type mockTransport struct {
	addr       string
	connected  bool
	closed     bool
	incoming   chan []byte
	outgoing   chan []byte
	connectErr error
	sendErr    error
	receiveErr error
	mu         sync.RWMutex
}

func newMockTransport(addr string) *mockTransport {
	return &mockTransport{
		addr:     addr,
		incoming: make(chan []byte, 100),
		outgoing: make(chan []byte, 100),
	}
}

func (m *mockTransport) Connect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	return nil
}

func (m *mockTransport) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	m.closed = true
	close(m.incoming)
	return nil
}

func (m *mockTransport) Send(ctx context.Context, data []byte) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	if !m.connected {
		return transport.ErrNotConnected
	}
	select {
	case m.outgoing <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *mockTransport) Receive(ctx context.Context) ([]byte, error) {
	m.mu.RLock()
	recvErr := m.receiveErr
	m.mu.RUnlock()

	if recvErr != nil {
		return nil, recvErr
	}

	select {
	case data, ok := <-m.incoming:
		if !ok {
			return nil, transport.ErrClosed
		}
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *mockTransport) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

func (m *mockTransport) RemoteAddr() string {
	return m.addr
}

// inject simulates receiving data from remote.
func (m *mockTransport) inject(data []byte) {
	m.incoming <- data
}

// getOutgoing returns the next message that was sent.
func (m *mockTransport) getOutgoing(ctx context.Context) ([]byte, error) {
	select {
	case data := <-m.outgoing:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// mockListener is a test listener for accepting mock connections.
type mockListener struct {
	addr      string
	conns     chan transport.Transport
	closed    bool
	acceptErr error
	mu        sync.RWMutex
}

func newMockListener(addr string) *mockListener {
	return &mockListener{
		addr:  addr,
		conns: make(chan transport.Transport, 10),
	}
}

func (m *mockListener) Accept(ctx context.Context) (transport.Transport, error) {
	m.mu.RLock()
	acceptErr := m.acceptErr
	m.mu.RUnlock()

	if acceptErr != nil {
		return nil, acceptErr
	}

	select {
	case conn, ok := <-m.conns:
		if !ok {
			return nil, transport.ErrClosed
		}
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *mockListener) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.conns)
	}
	return nil
}

func (m *mockListener) Addr() string {
	return m.addr
}

// injectConnection simulates an incoming connection.
func (m *mockListener) injectConnection(t transport.Transport) {
	m.conns <- t
}

// channelPair creates a pair of connected mock transports.
func channelPair(addr1, addr2 string) (*mockTransport, *mockTransport) {
	t1 := &mockTransport{
		addr:      addr1,
		connected: true,
		incoming:  make(chan []byte, 100),
		outgoing:  make(chan []byte, 100),
	}
	t2 := &mockTransport{
		addr:      addr2,
		connected: true,
		incoming:  make(chan []byte, 100),
		outgoing:  make(chan []byte, 100),
	}

	// Cross-connect: t1's outgoing goes to t2's incoming and vice versa
	go func() {
		for data := range t1.outgoing {
			select {
			case t2.incoming <- data:
			default:
			}
		}
	}()
	go func() {
		for data := range t2.outgoing {
			select {
			case t1.incoming <- data:
			default:
			}
		}
	}()

	return t1, t2
}

// Errors for testing.
var (
	errMockConnect = errors.New("mock connect error")
	errMockSend    = errors.New("mock send error")
	errMockReceive = errors.New("mock receive error")
)
