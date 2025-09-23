package capnweb

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// WebSocketTransport implements WebSocket transport for Cap'n Web RPC
type WebSocketTransport struct {
	// Connection management
	conn    WebSocketConn
	url     string
	options WebSocketOptions

	// Communication channels
	sendCh    chan []byte
	receiveCh chan []byte
	closeCh   chan struct{}

	// State management
	connected bool
	closed    bool
	closeMux  sync.RWMutex
	closeErr  error

	// Keep-alive
	pingTicker   *time.Ticker
	pongReceived chan struct{}

	// Statistics
	stats WebSocketStats
}

// WebSocketConn abstracts WebSocket connection interface
// In production, this would wrap gorilla/websocket or nhooyr.io/websocket
type WebSocketConn interface {
	WriteMessage(messageType int, data []byte) error
	ReadMessage() (messageType int, p []byte, err error)
	Close() error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	SetPongHandler(h func(appData string) error)
}

// WebSocketOptions configures WebSocket transport behavior
type WebSocketOptions struct {
	// Connection settings
	HandshakeTimeout time.Duration
	ReadBufferSize   int
	WriteBufferSize  int

	// Keep-alive settings
	PingInterval time.Duration
	PongTimeout  time.Duration

	// Reconnection settings
	EnableReconnect   bool
	ReconnectInterval time.Duration
	MaxReconnectAttempts int

	// Message settings
	MaxMessageSize  int64
	EnableTextMode  bool
	Compression     bool

	// Headers for initial connection
	Headers http.Header

	// Subprotocols
	Subprotocols []string

	// TLS settings
	SkipTLSVerify bool
}

// DefaultWebSocketOptions returns reasonable defaults
func DefaultWebSocketOptions() WebSocketOptions {
	return WebSocketOptions{
		HandshakeTimeout:     30 * time.Second,
		ReadBufferSize:       4096,
		WriteBufferSize:      4096,
		PingInterval:         30 * time.Second,
		PongTimeout:          10 * time.Second,
		EnableReconnect:      true,
		ReconnectInterval:    5 * time.Second,
		MaxReconnectAttempts: 5,
		MaxMessageSize:       1024 * 1024, // 1MB
		EnableTextMode:       false,
		Compression:          true,
		Headers:              make(http.Header),
	}
}

// WebSocketStats tracks WebSocket transport statistics
type WebSocketStats struct {
	ConnectionAttempts int64
	ConnectionFailures int64
	MessagesReceived   int64
	MessagesSent       int64
	BytesReceived      int64
	BytesSent          int64
	ReconnectAttempts  int64
	PingsSent          int64
	PongsReceived      int64
	ConnectedTime      time.Duration
	LastConnected      time.Time
}

// Message types (simplified WebSocket constants)
const (
	TextMessage   = 1
	BinaryMessage = 2
	CloseMessage  = 8
	PingMessage   = 9
	PongMessage   = 10
)

// NewWebSocketTransport creates a new WebSocket transport
func NewWebSocketTransport(url string, options ...WebSocketOptions) (*WebSocketTransport, error) {
	opts := DefaultWebSocketOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	// Set default headers
	if opts.Headers == nil {
		opts.Headers = make(http.Header)
	}
	opts.Headers.Set("User-Agent", "capnweb-go/1.0")

	transport := &WebSocketTransport{
		url:          url,
		options:      opts,
		sendCh:       make(chan []byte, 1000),
		receiveCh:    make(chan []byte, 1000),
		closeCh:      make(chan struct{}),
		pongReceived: make(chan struct{}, 1),
	}

	return transport, nil
}

// NewWebSocketTransportWithConn creates a WebSocket transport with an existing connection
func NewWebSocketTransportWithConn(conn WebSocketConn, url string, options ...WebSocketOptions) *WebSocketTransport {
	opts := DefaultWebSocketOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	transport := &WebSocketTransport{
		conn:         conn,
		url:          url,
		options:      opts,
		sendCh:       make(chan []byte, 1000),
		receiveCh:    make(chan []byte, 1000),
		closeCh:      make(chan struct{}),
		pongReceived: make(chan struct{}, 1),
		connected:    true,
	}

	transport.stats.ConnectionAttempts++
	transport.stats.LastConnected = time.Now()

	// Set up pong handler
	conn.SetPongHandler(func(appData string) error {
		select {
		case transport.pongReceived <- struct{}{}:
		default:
		}
		transport.stats.PongsReceived++
		return nil
	})

	// Start goroutines
	go transport.readPump()
	go transport.writePump()
	go transport.keepAlive()

	return transport
}

// Connect establishes the WebSocket connection
func (w *WebSocketTransport) Connect(ctx context.Context) error {
	w.closeMux.Lock()
	defer w.closeMux.Unlock()

	if w.connected {
		return fmt.Errorf("already connected")
	}

	if w.closed {
		return fmt.Errorf("transport is closed")
	}

	// In production, this would use a real WebSocket library like gorilla/websocket
	// For now, we'll create a mock connection that simulates the interface
	conn := &mockWebSocketConn{
		writeMessages: make(chan []byte, 100),
		readMessages:  make(chan []byte, 100),
		closed:        false,
		url:           w.url,
	}

	w.conn = conn
	w.connected = true
	w.stats.ConnectionAttempts++
	w.stats.LastConnected = time.Now()

	// Set up pong handler
	w.conn.SetPongHandler(func(appData string) error {
		select {
		case w.pongReceived <- struct{}{}:
		default:
		}
		w.stats.PongsReceived++
		return nil
	})

	// Start goroutines
	go w.readPump()
	go w.writePump()
	go w.keepAlive()

	return nil
}

// Send implements the Transport interface
func (w *WebSocketTransport) Send(ctx context.Context, data []byte) error {
	w.closeMux.RLock()
	defer w.closeMux.RUnlock()

	if w.closed {
		return fmt.Errorf("transport is closed")
	}

	if !w.connected {
		return fmt.Errorf("not connected")
	}

	select {
	case w.sendCh <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-w.closeCh:
		return fmt.Errorf("transport closed")
	}
}

// Receive implements the Transport interface
func (w *WebSocketTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case data := <-w.receiveCh:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-w.closeCh:
		return nil, fmt.Errorf("transport closed")
	}
}

// Close implements the Transport interface
func (w *WebSocketTransport) Close() error {
	w.closeMux.Lock()
	defer w.closeMux.Unlock()

	if w.closed {
		return w.closeErr
	}

	w.closed = true
	w.connected = false

	// Close channels
	close(w.closeCh)
	close(w.sendCh)

	// Stop keep-alive
	if w.pingTicker != nil {
		w.pingTicker.Stop()
	}

	// Close connection
	if w.conn != nil {
		w.closeErr = w.conn.Close()
	}

	return w.closeErr
}

// readPump reads messages from the WebSocket
func (w *WebSocketTransport) readPump() {
	defer func() {
		w.Close()
	}()

	// Set read deadline
	w.conn.SetReadDeadline(time.Now().Add(w.options.PongTimeout))

	for {
		messageType, message, err := w.conn.ReadMessage()
		if err != nil {
			if w.options.EnableReconnect && !w.closed {
				go w.reconnect()
			}
			return
		}

		// Reset read deadline
		w.conn.SetReadDeadline(time.Now().Add(w.options.PongTimeout))

		// Handle different message types
		switch messageType {
		case TextMessage, BinaryMessage:
			w.stats.MessagesReceived++
			w.stats.BytesReceived += int64(len(message))

			select {
			case w.receiveCh <- message:
			case <-w.closeCh:
				return
			default:
				// Drop message if receive channel is full
			}

		case PingMessage:
			// WebSocket library usually handles this automatically
		case PongMessage:
			// Handled by pong handler
		case CloseMessage:
			return
		}
	}
}

// writePump writes messages to the WebSocket
func (w *WebSocketTransport) writePump() {
	for {
		select {
		case message, ok := <-w.sendCh:
			if !ok {
				// Channel closed
				w.conn.WriteMessage(CloseMessage, []byte{})
				return
			}

			w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

			messageType := BinaryMessage
			if w.options.EnableTextMode {
				messageType = TextMessage
			}

			if err := w.conn.WriteMessage(messageType, message); err != nil {
				return
			}

			w.stats.MessagesSent++
			w.stats.BytesSent += int64(len(message))

		case <-w.closeCh:
			return
		}
	}
}

// keepAlive maintains the connection with ping/pong
func (w *WebSocketTransport) keepAlive() {
	w.pingTicker = time.NewTicker(w.options.PingInterval)
	defer w.pingTicker.Stop()

	for {
		select {
		case <-w.pingTicker.C:
			w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

			if err := w.conn.WriteMessage(PingMessage, nil); err != nil {
				return
			}

			w.stats.PingsSent++

			// Wait for pong
			select {
			case <-w.pongReceived:
				// Pong received, continue
			case <-time.After(w.options.PongTimeout):
				// Pong timeout, close connection
				w.Close()
				return
			case <-w.closeCh:
				return
			}

		case <-w.closeCh:
			return
		}
	}
}

// reconnect attempts to reconnect the WebSocket
func (w *WebSocketTransport) reconnect() {
	if w.closed {
		return
	}

	w.stats.ReconnectAttempts++

	for attempt := 1; attempt <= w.options.MaxReconnectAttempts; attempt++ {
		if w.closed {
			return
		}

		time.Sleep(w.options.ReconnectInterval * time.Duration(attempt))

		ctx, cancel := context.WithTimeout(context.Background(), w.options.HandshakeTimeout)
		err := w.Connect(ctx)
		cancel()

		if err == nil {
			// Reconnection successful
			return
		}

		w.stats.ConnectionFailures++
	}

	// All reconnection attempts failed
	w.Close()
}

// GetStats returns transport statistics
func (w *WebSocketTransport) GetStats() WebSocketStats {
	stats := w.stats
	if w.connected {
		stats.ConnectedTime = time.Since(w.stats.LastConnected)
	}
	return stats
}

// IsConnected returns true if the transport is connected
func (w *WebSocketTransport) IsConnected() bool {
	w.closeMux.RLock()
	defer w.closeMux.RUnlock()
	return w.connected && !w.closed
}

// mockWebSocketConn is a mock WebSocket connection for testing
type mockWebSocketConn struct {
	writeMessages chan []byte
	readMessages  chan []byte
	closed        bool
	closeMux      sync.RWMutex
	pongHandler   func(string) error
	url           string
}

func (m *mockWebSocketConn) WriteMessage(messageType int, data []byte) error {
	m.closeMux.RLock()
	defer m.closeMux.RUnlock()

	if m.closed {
		return fmt.Errorf("connection closed")
	}

	// Simulate sending message
	select {
	case m.writeMessages <- data:
		return nil
	default:
		return fmt.Errorf("write buffer full")
	}
}

func (m *mockWebSocketConn) ReadMessage() (int, []byte, error) {
	select {
	case data := <-m.readMessages:
		return BinaryMessage, data, nil
	case <-time.After(100 * time.Millisecond):
		// Simulate no message available
		return 0, nil, fmt.Errorf("no message")
	}
}

func (m *mockWebSocketConn) Close() error {
	m.closeMux.Lock()
	defer m.closeMux.Unlock()

	if m.closed {
		return nil
	}

	m.closed = true
	close(m.writeMessages)
	close(m.readMessages)
	return nil
}

func (m *mockWebSocketConn) SetReadDeadline(t time.Time) error {
	return nil // Mock implementation
}

func (m *mockWebSocketConn) SetWriteDeadline(t time.Time) error {
	return nil // Mock implementation
}

func (m *mockWebSocketConn) SetPongHandler(h func(string) error) {
	m.pongHandler = h
}

// WebSocketTransportFactory creates WebSocket transports
type WebSocketTransportFactory struct {
	baseURL string
	options WebSocketOptions
}

// NewWebSocketTransportFactory creates a new factory
func NewWebSocketTransportFactory(baseURL string, options ...WebSocketOptions) *WebSocketTransportFactory {
	opts := DefaultWebSocketOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	return &WebSocketTransportFactory{
		baseURL: baseURL,
		options: opts,
	}
}

// CreateTransport creates a new WebSocket transport
func (f *WebSocketTransportFactory) CreateTransport(endpoint string) (Transport, error) {
	fullURL := f.baseURL + endpoint
	transport, err := NewWebSocketTransport(fullURL, f.options)
	if err != nil {
		return nil, err
	}

	// Auto-connect
	ctx, cancel := context.WithTimeout(context.Background(), f.options.HandshakeTimeout)
	defer cancel()

	err = transport.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return transport, nil
}

// WebSocketServer provides server-side WebSocket handling
type WebSocketServer struct {
	upgrader WebSocketUpgrader
	handler  func(WebSocketConn)
	options  WebSocketServerOptions
}

// WebSocketUpgrader abstracts WebSocket upgrader interface
type WebSocketUpgrader interface {
	Upgrade(w http.ResponseWriter, r *http.Request, responseHeader http.Header) (WebSocketConn, error)
}

// WebSocketServerOptions configures WebSocket server behavior
type WebSocketServerOptions struct {
	CheckOrigin      func(r *http.Request) bool
	Subprotocols     []string
	EnableCompression bool
	ReadBufferSize   int
	WriteBufferSize  int
	HandshakeTimeout time.Duration
}

// DefaultWebSocketServerOptions returns server defaults
func DefaultWebSocketServerOptions() WebSocketServerOptions {
	return WebSocketServerOptions{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins in default config
		},
		EnableCompression: true,
		ReadBufferSize:    4096,
		WriteBufferSize:   4096,
		HandshakeTimeout:  30 * time.Second,
	}
}

// NewWebSocketServer creates a new WebSocket server
func NewWebSocketServer(upgrader WebSocketUpgrader, handler func(WebSocketConn), options ...WebSocketServerOptions) *WebSocketServer {
	opts := DefaultWebSocketServerOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	return &WebSocketServer{
		upgrader: upgrader,
		handler:  handler,
		options:  opts,
	}
}

// ServeHTTP implements http.Handler
func (s *WebSocketServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check origin
	if !s.options.CheckOrigin(r) {
		http.Error(w, "Origin not allowed", http.StatusForbidden)
		return
	}

	// Upgrade to WebSocket
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "Failed to upgrade connection", http.StatusBadRequest)
		return
	}

	// Handle connection
	go s.handler(conn)
}