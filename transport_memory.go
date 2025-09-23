package capnweb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// MemoryTransport is an in-memory transport implementation useful for testing
// and local communication. It consists of two connected transports that
// communicate through channels.
type MemoryTransport struct {
	sendCh   chan []byte
	recvCh   chan []byte
	closeCh  chan struct{}
	closeErr error
	closed   int32
	stats    TransportStats
	mu       sync.RWMutex
}

// NewMemoryTransportPair creates a pair of connected MemoryTransports.
// Messages sent on one transport are received by the other.
func NewMemoryTransportPair() (*MemoryTransport, *MemoryTransport) {
	// Create channels for bidirectional communication
	ch1 := make(chan []byte, 16) // Buffer for async sending
	ch2 := make(chan []byte, 16)
	close1 := make(chan struct{})
	close2 := make(chan struct{})

	t1 := &MemoryTransport{
		sendCh:  ch1,
		recvCh:  ch2,
		closeCh: close1,
	}

	t2 := &MemoryTransport{
		sendCh:  ch2,
		recvCh:  ch1,
		closeCh: close2,
	}

	return t1, t2
}

// Send implements the Transport interface.
func (t *MemoryTransport) Send(ctx context.Context, message []byte) error {
	if atomic.LoadInt32(&t.closed) != 0 {
		return ErrTransportClosed
	}

	if len(message) > MessageSizeLimit {
		atomic.AddUint64(&t.stats.Errors, 1)
		return ErrMessageTooLarge
	}

	// Copy message to avoid data races
	msg := make([]byte, len(message))
	copy(msg, message)

	select {
	case t.sendCh <- msg:
		atomic.AddUint64(&t.stats.BytesSent, uint64(len(message)))
		atomic.AddUint64(&t.stats.MessagesSent, 1)
		return nil
	case <-t.closeCh:
		return t.getCloseError()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Receive implements the Transport interface.
func (t *MemoryTransport) Receive(ctx context.Context) ([]byte, error) {
	if atomic.LoadInt32(&t.closed) != 0 {
		return nil, t.getCloseError()
	}

	select {
	case msg := <-t.recvCh:
		atomic.AddUint64(&t.stats.BytesReceived, uint64(len(msg)))
		atomic.AddUint64(&t.stats.MessagesReceived, 1)
		return msg, nil
	case <-t.closeCh:
		return nil, t.getCloseError()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close implements the Transport interface.
func (t *MemoryTransport) Close() error {
	if !atomic.CompareAndSwapInt32(&t.closed, 0, 1) {
		// Already closed
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closeErr == nil {
		t.closeErr = io.EOF
	}

	// Only close the channel if it exists and hasn't been closed
	if t.closeCh != nil {
		select {
		case <-t.closeCh:
			// Already closed
		default:
			close(t.closeCh)
		}
	}
	return nil
}

// Abort implements the TransportCloser interface.
func (t *MemoryTransport) Abort(reason error) {
	if !atomic.CompareAndSwapInt32(&t.closed, 0, 1) {
		// Already closed
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if reason == nil {
		reason = errors.New("transport aborted")
	}
	t.closeErr = fmt.Errorf("transport aborted: %w", reason)

	close(t.closeCh)
}

// Stats implements the StatsProvider interface.
func (t *MemoryTransport) Stats() TransportStats {
	return TransportStats{
		BytesSent:        atomic.LoadUint64(&t.stats.BytesSent),
		BytesReceived:    atomic.LoadUint64(&t.stats.BytesReceived),
		MessagesSent:     atomic.LoadUint64(&t.stats.MessagesSent),
		MessagesReceived: atomic.LoadUint64(&t.stats.MessagesReceived),
		Errors:          atomic.LoadUint64(&t.stats.Errors),
	}
}

func (t *MemoryTransport) getCloseError() error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.closeErr != nil {
		return t.closeErr
	}
	return ErrTransportClosed
}

// Ensure MemoryTransport implements all expected interfaces
var (
	_ Transport        = (*MemoryTransport)(nil)
	_ TransportCloser  = (*MemoryTransport)(nil)
	_ StatsProvider    = (*MemoryTransport)(nil)
)