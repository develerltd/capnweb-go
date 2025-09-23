package capnweb

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Session represents an RPC session with import/export management
type Session struct {
	// Transport for sending/receiving messages
	transport Transport

	// Protocol handler for message encoding/decoding
	protocol *Protocol

	// Reference tables
	exports map[ExportID]*exportEntry
	imports map[ImportID]*importEntry

	// ID generators
	nextExportID int64
	nextImportID int64

	// Synchronization
	mu       sync.RWMutex
	closed   bool
	closeErr error

	// Context and cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// Message handling
	messageQueue chan Message
	responses    map[ExportID]chan responseInfo

	// Statistics
	stats *SessionStats

	// Options
	options SessionOptions
}

// SessionOptions configures session behavior
type SessionOptions struct {
	// MessageQueueSize sets the size of the message processing queue
	MessageQueueSize int

	// ResponseTimeout sets how long to wait for responses
	ResponseTimeout time.Duration

	// EnableStats enables statistics collection
	EnableStats bool

	// MaxConcurrentRequests limits concurrent RPC calls
	MaxConcurrentRequests int

	// KeepAliveInterval sets the keep-alive ping interval
	KeepAliveInterval time.Duration
}

// DefaultSessionOptions returns reasonable defaults
func DefaultSessionOptions() SessionOptions {
	return SessionOptions{
		MessageQueueSize:      1000,
		ResponseTimeout:       30 * time.Second,
		EnableStats:           true,
		MaxConcurrentRequests: 100,
		KeepAliveInterval:     30 * time.Second,
	}
}

// exportEntry represents an exported object or promise
type exportEntry struct {
	// Value is the exported object or function
	Value interface{}

	// RefCount tracks how many remote references exist
	RefCount int32

	// IsPromise indicates if this is a promise export
	IsPromise bool

	// Created timestamp
	Created time.Time

	// LastAccessed timestamp for cleanup
	LastAccessed time.Time

	// Mutex for thread safety
	mu sync.RWMutex
}

// importEntry represents an imported remote reference
type importEntry struct {
	// ImportID is the remote reference ID
	ImportID ImportID

	// RefCount tracks local usage
	RefCount int32

	// IsPromise indicates if this represents a promise
	IsPromise bool

	// Path represents property access path for chained calls
	Path PropertyPath

	// Session reference for making calls
	session *Session

	// Created timestamp
	Created time.Time

	// LastAccessed timestamp for cleanup
	LastAccessed time.Time

	// Mutex for thread safety
	mu sync.RWMutex
}

// responseInfo holds response data for async operations
type responseInfo struct {
	Value interface{}
	Error error
}

// SessionStats tracks session-level statistics
type SessionStats struct {
	// Messages sent/received
	MessagesSent     int64
	MessagesReceived int64

	// Exports/imports
	ExportsCreated int64
	ImportsCreated int64
	ExportsActive  int64
	ImportsActive  int64

	// RPC calls
	CallsStarted   int64
	CallsCompleted int64
	CallsFailed    int64

	// Timing
	SessionStarted time.Time
	LastActivity   time.Time

	// Errors
	ProtocolErrors int64
	TransportErrors int64
}

// NewSession creates a new RPC session
func NewSession(transport Transport, options ...SessionOptions) (*Session, error) {
	if transport == nil {
		return nil, fmt.Errorf("transport cannot be nil")
	}

	opts := DefaultSessionOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create serializer and deserializer with session callbacks
	var session *Session
	serializer := NewSerializer(func(value interface{}) (ExportID, error) {
		return session.exportValue(value)
	})
	deserializer := NewDeserializer(func(id ImportID, isPromise bool) (interface{}, error) {
		return session.createImportStub(id, isPromise)
	})

	session = &Session{
		transport:    transport,
		protocol:     NewProtocol(serializer, deserializer),
		exports:      make(map[ExportID]*exportEntry),
		imports:      make(map[ImportID]*importEntry),
		ctx:          ctx,
		cancel:       cancel,
		messageQueue: make(chan Message, opts.MessageQueueSize),
		responses:    make(map[ExportID]chan responseInfo),
		options:      opts,
	}

	if opts.EnableStats {
		session.stats = &SessionStats{
			SessionStarted: time.Now(),
			LastActivity:   time.Now(),
		}
	}

	// Start message processing goroutine
	go session.messageLoop()

	return session, nil
}

// Close closes the session and cleans up resources
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return s.closeErr
	}

	s.closed = true
	s.cancel()

	// Send abort message if possible
	if s.transport != nil {
		abortMsg := s.protocol.NewAbortMessage("session closed", nil)
		data, err := s.protocol.EncodeMessage(abortMsg)
		if err == nil {
			s.transport.Send(s.ctx, data) // Best effort
		}
	}

	// Close transport
	if s.transport != nil {
		s.closeErr = s.transport.Close()
	}

	// Close message queue
	close(s.messageQueue)

	// Clean up pending responses
	for _, ch := range s.responses {
		close(ch)
	}

	// Update statistics
	if s.stats != nil {
		s.stats.LastActivity = time.Now()
	}

	return s.closeErr
}

// Send sends a message through the session
func (s *Session) Send(ctx context.Context, msg Message) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return fmt.Errorf("session is closed")
	}

	// Encode message
	data, err := s.protocol.EncodeMessage(msg)
	if err != nil {
		if s.stats != nil {
			atomic.AddInt64(&s.stats.ProtocolErrors, 1)
		}
		return fmt.Errorf("failed to encode message: %w", err)
	}

	// Send through transport
	if err := s.transport.Send(ctx, data); err != nil {
		if s.stats != nil {
			atomic.AddInt64(&s.stats.TransportErrors, 1)
		}
		return fmt.Errorf("failed to send message: %w", err)
	}

	// Update statistics
	if s.stats != nil {
		atomic.AddInt64(&s.stats.MessagesSent, 1)
		s.stats.LastActivity = time.Now()
	}

	return nil
}

// messageLoop processes incoming messages
func (s *Session) messageLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return

		case msg, ok := <-s.messageQueue:
			if !ok {
				return
			}
			s.handleMessage(msg)

		default:
			// Try to receive from transport
			data, err := s.transport.Receive(s.ctx)
			if err != nil {
				if s.ctx.Err() != nil {
					return // Context cancelled
				}
				if s.stats != nil {
					atomic.AddInt64(&s.stats.TransportErrors, 1)
				}
				continue
			}

			// Decode message
			msg, err := s.protocol.DecodeMessage(data)
			if err != nil {
				if s.stats != nil {
					atomic.AddInt64(&s.stats.ProtocolErrors, 1)
				}
				continue
			}

			// Update statistics
			if s.stats != nil {
				atomic.AddInt64(&s.stats.MessagesReceived, 1)
				s.stats.LastActivity = time.Now()
			}

			// Handle message
			s.handleMessage(msg)
		}
	}
}

// handleMessage processes a decoded message
func (s *Session) handleMessage(msg Message) {
	if len(msg) == 0 {
		return
	}

	msgType := MessageType(msg[0].(string))

	switch msgType {
	case MessageTypePush:
		s.handlePush(msg)
	case MessageTypePull:
		s.handlePull(msg)
	case MessageTypeResolve:
		s.handleResolve(msg)
	case MessageTypeReject:
		s.handleReject(msg)
	case MessageTypeRelease:
		s.handleRelease(msg)
	case MessageTypeAbort:
		s.handleAbort(msg)
	default:
		// Unknown message type, ignore
	}
}

// Message handler implementations

// handlePush processes a push message (new RPC call or expression)
func (s *Session) handlePush(msg Message) {
	if len(msg) < 2 {
		return
	}

	// TODO: Parse expression and execute it
	// This would involve:
	// 1. Deserializing the expression
	// 2. Looking up the target object in exports table
	// 3. Executing the method/property access
	// 4. Sending resolve/reject response
}

// handlePull processes a pull message (request for promise resolution)
func (s *Session) handlePull(msg Message) {
	if len(msg) < 2 {
		return
	}

	importID, ok := msg[1].(float64)
	if !ok {
		return
	}

	// Look up the export entry
	s.mu.RLock()
	entry, exists := s.exports[ExportID(importID)]
	s.mu.RUnlock()

	if !exists {
		// Send reject message
		rejectMsg := s.protocol.NewRejectMessage(ExportID(importID),
			fmt.Errorf("export not found: %v", importID))
		s.Send(context.Background(), rejectMsg)
		return
	}

	// Send resolve message with the value
	resolveMsg := s.protocol.NewResolveMessage(ExportID(importID), entry.Value)
	s.Send(context.Background(), resolveMsg)
}

// handleResolve processes a resolve message (successful RPC result)
func (s *Session) handleResolve(msg Message) {
	if len(msg) < 3 {
		return
	}

	exportID, ok := msg[1].(float64)
	if !ok {
		return
	}

	value := msg[2]

	// Notify any waiting promises
	s.mu.RLock()
	if ch, exists := s.responses[ExportID(exportID)]; exists {
		select {
		case ch <- responseInfo{Value: value}:
		default:
		}
		delete(s.responses, ExportID(exportID))
	}
	s.mu.RUnlock()
}

// handleReject processes a reject message (RPC error result)
func (s *Session) handleReject(msg Message) {
	if len(msg) < 3 {
		return
	}

	exportID, ok := msg[1].(float64)
	if !ok {
		return
	}

	errorValue := msg[2]

	// Convert error value to Go error
	var err error
	if errStr, ok := errorValue.(string); ok {
		err = fmt.Errorf("%s", errStr)
	} else {
		err = fmt.Errorf("RPC error: %v", errorValue)
	}

	// Notify any waiting promises
	s.mu.RLock()
	if ch, exists := s.responses[ExportID(exportID)]; exists {
		select {
		case ch <- responseInfo{Error: err}:
		default:
		}
		delete(s.responses, ExportID(exportID))
	}
	s.mu.RUnlock()
}

// handleRelease processes a release message (reference count decrement)
func (s *Session) handleRelease(msg Message) {
	if len(msg) < 3 {
		return
	}

	importID, ok := msg[1].(float64)
	if !ok {
		return
	}

	refCount, ok := msg[2].(float64)
	if !ok {
		refCount = 1
	}

	// Decrement reference count for export
	s.mu.Lock()
	if entry, exists := s.exports[ExportID(importID)]; exists {
		entry.RefCount -= int32(refCount)
		if entry.RefCount <= 0 {
			delete(s.exports, ExportID(importID))
			if s.stats != nil {
				atomic.AddInt64(&s.stats.ExportsActive, -1)
			}
		}
	}
	s.mu.Unlock()
}

// handleAbort processes an abort message (session termination)
func (s *Session) handleAbort(msg Message) {
	if len(msg) < 2 {
		return
	}

	reason := msg[1]

	// Close the session with the abort reason
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		s.closeErr = fmt.Errorf("session aborted: %v", reason)
		s.cancel()
	}
	s.mu.Unlock()
}

// exportValue exports a local value and returns its export ID
func (s *Session) exportValue(value interface{}) (ExportID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	exportID := ExportID(atomic.AddInt64(&s.nextExportID, 1))

	entry := &exportEntry{
		Value:        value,
		RefCount:     1,
		IsPromise:    false, // TODO: Detect promise types
		Created:      time.Now(),
		LastAccessed: time.Now(),
	}

	s.exports[exportID] = entry

	if s.stats != nil {
		atomic.AddInt64(&s.stats.ExportsCreated, 1)
		atomic.AddInt64(&s.stats.ExportsActive, 1)
	}

	return exportID, nil
}

// createImportStub creates a stub for an imported reference
func (s *Session) createImportStub(id ImportID, isPromise bool) (interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := &importEntry{
		ImportID:     id,
		RefCount:     1,
		IsPromise:    isPromise,
		session:      s,
		Created:      time.Now(),
		LastAccessed: time.Now(),
	}

	s.imports[id] = entry

	if s.stats != nil {
		atomic.AddInt64(&s.stats.ImportsCreated, 1)
		atomic.AddInt64(&s.stats.ImportsActive, 1)
	}

	// Return a stub interface
	return &stubImpl{
		session:  s,
		importID: &id,
		path:     PropertyPath{},
	}, nil
}

// GetStats returns session statistics (if enabled)
func (s *Session) GetStats() *SessionStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.stats == nil {
		return nil
	}

	// Return a copy to avoid race conditions
	statsCopy := *s.stats
	return &statsCopy
}

// Context returns the session context
func (s *Session) Context() context.Context {
	return s.ctx
}

// IsClosed returns true if the session is closed
func (s *Session) IsClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}