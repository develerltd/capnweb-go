// Package capnweb provides a Go implementation of the Cap'n Web RPC system.
package capnweb

import (
	"context"
	"errors"
)

// Transport defines the interface for RPC message transports.
// A transport is a bidirectional message stream that can send and receive
// messages between RPC peers. Implementations must be safe for concurrent use.
type Transport interface {
	// Send transmits a message to the remote peer.
	// The message should be delivered reliably and in order.
	// Returns an error if the transport is closed or the send fails.
	Send(ctx context.Context, message []byte) error

	// Receive waits for and returns the next message from the remote peer.
	// Returns io.EOF when the transport is cleanly closed.
	// Returns other errors for transport failures or if the context is canceled.
	Receive(ctx context.Context) ([]byte, error)

	// Close closes the transport and releases any associated resources.
	// After Close is called, Send and Receive should return errors.
	// Close should be safe to call multiple times.
	Close() error
}

// TransportCloser is an optional interface that transports can implement
// to be notified when the RPC session encounters an unrecoverable error.
type TransportCloser interface {
	Transport

	// Abort indicates that the RPC system has encountered an error that
	// prevents the session from continuing. The transport should attempt
	// to send any queued messages if possible, then close the connection.
	//
	// The reason parameter contains the error that caused the abort.
	// This method should not block and should be safe to call concurrently
	// with other transport methods.
	Abort(reason error)
}

// Common transport errors
var (
	// ErrTransportClosed indicates the transport has been closed
	ErrTransportClosed = errors.New("transport is closed")

	// ErrMessageTooLarge indicates a message exceeds size limits
	ErrMessageTooLarge = errors.New("message too large")

	// ErrInvalidMessage indicates a malformed message was received
	ErrInvalidMessage = errors.New("invalid message format")
)

// TransportStats provides statistics about transport usage.
type TransportStats struct {
	// BytesSent is the total number of bytes sent
	BytesSent uint64

	// BytesReceived is the total number of bytes received
	BytesReceived uint64

	// MessagesSent is the total number of messages sent
	MessagesSent uint64

	// MessagesReceived is the total number of messages received
	MessagesReceived uint64

	// Errors is the number of transport errors encountered
	Errors uint64
}

// StatsProvider is an optional interface that transports can implement
// to provide usage statistics.
type StatsProvider interface {
	// Stats returns current transport statistics
	Stats() TransportStats
}

// MessageSizeLimit defines the maximum message size for a transport.
// Transports should reject messages larger than this limit.
const MessageSizeLimit = 64 * 1024 * 1024 // 64MB