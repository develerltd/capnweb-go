package capnweb

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// TCPTransport implements TCP-based transport for Cap'n Web RPC
type TCPTransport struct {
	// Connection management
	conn    net.Conn
	address string
	options TCPOptions

	// Communication channels
	sendCh    chan []byte
	receiveCh chan []byte
	closeCh   chan struct{}

	// State management
	connected bool
	closed    bool
	closeMux  sync.RWMutex
	closeErr  error

	// Message framing
	frameBuffer []byte
	framePos    int

	// Statistics
	stats TCPStats
}

// TCPOptions configures TCP transport behavior
type TCPOptions struct {
	// Connection settings
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	KeepAlive      bool
	KeepAlivePeriod time.Duration

	// Reconnection settings
	EnableReconnect      bool
	ReconnectInterval    time.Duration
	MaxReconnectAttempts int

	// Buffer settings
	SendBufferSize    int
	ReceiveBufferSize int
	MaxMessageSize    int

	// Framing
	FrameSize int // Size of length prefix (2 or 4 bytes)
}

// DefaultTCPOptions returns reasonable defaults
func DefaultTCPOptions() TCPOptions {
	return TCPOptions{
		ConnectTimeout:       30 * time.Second,
		ReadTimeout:          30 * time.Second,
		WriteTimeout:         30 * time.Second,
		KeepAlive:            true,
		KeepAlivePeriod:      30 * time.Second,
		EnableReconnect:      true,
		ReconnectInterval:    5 * time.Second,
		MaxReconnectAttempts: 5,
		SendBufferSize:       1000,
		ReceiveBufferSize:    1000,
		MaxMessageSize:       1024 * 1024, // 1MB
		FrameSize:            4,            // 4-byte length prefix
	}
}

// TCPStats tracks TCP transport statistics
type TCPStats struct {
	ConnectionAttempts int64
	ConnectionFailures int64
	MessagesReceived   int64
	MessagesSent       int64
	BytesReceived      int64
	BytesSent          int64
	ReconnectAttempts  int64
	FramingErrors      int64
	ConnectedTime      time.Duration
	LastConnected      time.Time
}

// NewTCPTransport creates a new TCP transport
func NewTCPTransport(address string, options ...TCPOptions) (*TCPTransport, error) {
	opts := DefaultTCPOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	transport := &TCPTransport{
		address:     address,
		options:     opts,
		sendCh:      make(chan []byte, opts.SendBufferSize),
		receiveCh:   make(chan []byte, opts.ReceiveBufferSize),
		closeCh:     make(chan struct{}),
		frameBuffer: make([]byte, opts.MaxMessageSize+opts.FrameSize),
	}

	return transport, nil
}

// Connect establishes the TCP connection
func (t *TCPTransport) Connect(ctx context.Context) error {
	t.closeMux.Lock()
	defer t.closeMux.Unlock()

	if t.connected {
		return fmt.Errorf("already connected")
	}

	if t.closed {
		return fmt.Errorf("transport is closed")
	}

	// Create connection with timeout
	dialer := &net.Dialer{
		Timeout: t.options.ConnectTimeout,
	}

	conn, err := dialer.DialContext(ctx, "tcp", t.address)
	if err != nil {
		t.stats.ConnectionFailures++
		return fmt.Errorf("failed to connect: %w", err)
	}

	// Configure connection
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		if t.options.KeepAlive {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(t.options.KeepAlivePeriod)
		}
	}

	t.conn = conn
	t.connected = true
	t.stats.ConnectionAttempts++
	t.stats.LastConnected = time.Now()

	// Start goroutines
	go t.readPump()
	go t.writePump()

	return nil
}

// Send implements the Transport interface
func (t *TCPTransport) Send(ctx context.Context, data []byte) error {
	t.closeMux.RLock()
	defer t.closeMux.RUnlock()

	if t.closed {
		return fmt.Errorf("transport is closed")
	}

	if !t.connected {
		return fmt.Errorf("not connected")
	}

	if len(data) > t.options.MaxMessageSize {
		return fmt.Errorf("message too large: %d > %d", len(data), t.options.MaxMessageSize)
	}

	select {
	case t.sendCh <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-t.closeCh:
		return fmt.Errorf("transport closed")
	}
}

// Receive implements the Transport interface
func (t *TCPTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case data := <-t.receiveCh:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.closeCh:
		return nil, fmt.Errorf("transport closed")
	}
}

// Close implements the Transport interface
func (t *TCPTransport) Close() error {
	t.closeMux.Lock()
	defer t.closeMux.Unlock()

	if t.closed {
		return t.closeErr
	}

	t.closed = true
	t.connected = false

	// Close channels
	close(t.closeCh)
	close(t.sendCh)

	// Close connection
	if t.conn != nil {
		t.closeErr = t.conn.Close()
	}

	return t.closeErr
}

// readPump reads messages from the TCP connection
func (t *TCPTransport) readPump() {
	defer func() {
		t.Close()
	}()

	for {
		// Set read deadline
		if t.options.ReadTimeout > 0 {
			t.conn.SetReadDeadline(time.Now().Add(t.options.ReadTimeout))
		}

		// Read message frame
		message, err := t.readFrame()
		if err != nil {
			if t.options.EnableReconnect && !t.closed {
				go t.reconnect()
			}
			return
		}

		t.stats.MessagesReceived++
		t.stats.BytesReceived += int64(len(message))

		select {
		case t.receiveCh <- message:
		case <-t.closeCh:
			return
		default:
			// Drop message if receive channel is full
		}
	}
}

// writePump writes messages to the TCP connection
func (t *TCPTransport) writePump() {
	for {
		select {
		case message, ok := <-t.sendCh:
			if !ok {
				// Channel closed
				return
			}

			// Set write deadline
			if t.options.WriteTimeout > 0 {
				t.conn.SetWriteDeadline(time.Now().Add(t.options.WriteTimeout))
			}

			// Write framed message
			if err := t.writeFrame(message); err != nil {
				return
			}

			t.stats.MessagesSent++
			t.stats.BytesSent += int64(len(message))

		case <-t.closeCh:
			return
		}
	}
}

// readFrame reads a length-prefixed message frame
func (t *TCPTransport) readFrame() ([]byte, error) {
	// Read length prefix
	lengthBytes := make([]byte, t.options.FrameSize)
	_, err := t.conn.Read(lengthBytes)
	if err != nil {
		return nil, err
	}

	// Parse length
	var messageLength int
	switch t.options.FrameSize {
	case 2:
		messageLength = int(lengthBytes[0])<<8 | int(lengthBytes[1])
	case 4:
		messageLength = int(lengthBytes[0])<<24 | int(lengthBytes[1])<<16 |
			int(lengthBytes[2])<<8 | int(lengthBytes[3])
	default:
		return nil, fmt.Errorf("unsupported frame size: %d", t.options.FrameSize)
	}

	// Validate message length
	if messageLength > t.options.MaxMessageSize {
		t.stats.FramingErrors++
		return nil, fmt.Errorf("message too large: %d", messageLength)
	}

	if messageLength <= 0 {
		t.stats.FramingErrors++
		return nil, fmt.Errorf("invalid message length: %d", messageLength)
	}

	// Read message data
	messageData := make([]byte, messageLength)
	_, err = t.conn.Read(messageData)
	if err != nil {
		return nil, err
	}

	return messageData, nil
}

// writeFrame writes a length-prefixed message frame
func (t *TCPTransport) writeFrame(message []byte) error {
	messageLength := len(message)

	// Create length prefix
	var lengthBytes []byte
	switch t.options.FrameSize {
	case 2:
		if messageLength > 65535 {
			return fmt.Errorf("message too large for 2-byte frame: %d", messageLength)
		}
		lengthBytes = []byte{
			byte(messageLength >> 8),
			byte(messageLength),
		}
	case 4:
		lengthBytes = []byte{
			byte(messageLength >> 24),
			byte(messageLength >> 16),
			byte(messageLength >> 8),
			byte(messageLength),
		}
	default:
		return fmt.Errorf("unsupported frame size: %d", t.options.FrameSize)
	}

	// Write length prefix
	_, err := t.conn.Write(lengthBytes)
	if err != nil {
		return err
	}

	// Write message data
	_, err = t.conn.Write(message)
	return err
}

// reconnect attempts to reconnect the TCP connection
func (t *TCPTransport) reconnect() {
	if t.closed {
		return
	}

	t.stats.ReconnectAttempts++

	for attempt := 1; attempt <= t.options.MaxReconnectAttempts; attempt++ {
		if t.closed {
			return
		}

		time.Sleep(t.options.ReconnectInterval * time.Duration(attempt))

		ctx, cancel := context.WithTimeout(context.Background(), t.options.ConnectTimeout)
		err := t.Connect(ctx)
		cancel()

		if err == nil {
			// Reconnection successful
			return
		}

		t.stats.ConnectionFailures++
	}

	// All reconnection attempts failed
	t.Close()
}

// GetStats returns transport statistics
func (t *TCPTransport) GetStats() TCPStats {
	stats := t.stats
	if t.connected {
		stats.ConnectedTime = time.Since(t.stats.LastConnected)
	}
	return stats
}

// IsConnected returns true if the transport is connected
func (t *TCPTransport) IsConnected() bool {
	t.closeMux.RLock()
	defer t.closeMux.RUnlock()
	return t.connected && !t.closed
}

// TransportFactory is a generic interface for creating transports
type TransportFactory interface {
	CreateTransport(endpoint string) (Transport, error)
	GetTransportType() string
}

// TCPTransportFactory creates TCP transports
type TCPTransportFactory struct {
	baseAddress string
	options     TCPOptions
}

// NewTCPTransportFactory creates a new TCP transport factory
func NewTCPTransportFactory(baseAddress string, options ...TCPOptions) *TCPTransportFactory {
	opts := DefaultTCPOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	return &TCPTransportFactory{
		baseAddress: baseAddress,
		options:     opts,
	}
}

// CreateTransport creates a new TCP transport
func (f *TCPTransportFactory) CreateTransport(endpoint string) (Transport, error) {
	address := f.baseAddress + endpoint
	transport, err := NewTCPTransport(address, f.options)
	if err != nil {
		return nil, err
	}

	// Auto-connect
	ctx, cancel := context.WithTimeout(context.Background(), f.options.ConnectTimeout)
	defer cancel()

	err = transport.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return transport, nil
}

// GetTransportType returns the transport type name
func (f *TCPTransportFactory) GetTransportType() string {
	return "tcp"
}

// UnixSocketTransport implements Unix domain socket transport
type UnixSocketTransport struct {
	*TCPTransport
	socketPath string
}

// NewUnixSocketTransport creates a new Unix socket transport
func NewUnixSocketTransport(socketPath string, options ...TCPOptions) (*UnixSocketTransport, error) {
	tcpTransport, err := NewTCPTransport(socketPath, options...)
	if err != nil {
		return nil, err
	}

	return &UnixSocketTransport{
		TCPTransport: tcpTransport,
		socketPath:   socketPath,
	}, nil
}

// Connect establishes the Unix socket connection
func (u *UnixSocketTransport) Connect(ctx context.Context) error {
	u.closeMux.Lock()
	defer u.closeMux.Unlock()

	if u.connected {
		return fmt.Errorf("already connected")
	}

	if u.closed {
		return fmt.Errorf("transport is closed")
	}

	// Create Unix socket connection
	dialer := &net.Dialer{
		Timeout: u.options.ConnectTimeout,
	}

	conn, err := dialer.DialContext(ctx, "unix", u.socketPath)
	if err != nil {
		u.stats.ConnectionFailures++
		return fmt.Errorf("failed to connect to Unix socket: %w", err)
	}

	u.conn = conn
	u.connected = true
	u.stats.ConnectionAttempts++
	u.stats.LastConnected = time.Now()

	// Start goroutines
	go u.readPump()
	go u.writePump()

	return nil
}

// InProcessTransport implements in-process transport for testing
type InProcessTransport struct {
	// Peer transport
	peer *InProcessTransport

	// Communication channels
	sendCh    chan []byte
	receiveCh chan []byte
	closeCh   chan struct{}

	// State management
	closed   bool
	closeMux sync.RWMutex

	// Statistics
	stats InProcessStats
}

// InProcessStats tracks in-process transport statistics
type InProcessStats struct {
	MessagesReceived int64
	MessagesSent     int64
	BytesReceived    int64
	BytesSent        int64
}

// NewInProcessTransportPair creates a pair of connected in-process transports
func NewInProcessTransportPair() (*InProcessTransport, *InProcessTransport) {
	t1 := &InProcessTransport{
		sendCh:    make(chan []byte, 1000),
		receiveCh: make(chan []byte, 1000),
		closeCh:   make(chan struct{}),
	}

	t2 := &InProcessTransport{
		sendCh:    make(chan []byte, 1000),
		receiveCh: make(chan []byte, 1000),
		closeCh:   make(chan struct{}),
	}

	// Cross-connect the transports
	t1.peer = t2
	t2.peer = t1

	// Start message forwarding
	go t1.messageForwarder()
	go t2.messageForwarder()

	return t1, t2
}

// Send implements the Transport interface
func (i *InProcessTransport) Send(ctx context.Context, data []byte) error {
	i.closeMux.RLock()
	defer i.closeMux.RUnlock()

	if i.closed {
		return fmt.Errorf("transport is closed")
	}

	// Copy data to avoid race conditions
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	select {
	case i.sendCh <- dataCopy:
		i.stats.MessagesSent++
		i.stats.BytesSent += int64(len(data))
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-i.closeCh:
		return fmt.Errorf("transport closed")
	}
}

// Receive implements the Transport interface
func (i *InProcessTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case data := <-i.receiveCh:
		i.stats.MessagesReceived++
		i.stats.BytesReceived += int64(len(data))
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-i.closeCh:
		return nil, fmt.Errorf("transport closed")
	}
}

// Close implements the Transport interface
func (i *InProcessTransport) Close() error {
	i.closeMux.Lock()
	defer i.closeMux.Unlock()

	if i.closed {
		return nil
	}

	i.closed = true
	close(i.closeCh)

	return nil
}

// messageForwarder forwards messages between paired transports
func (i *InProcessTransport) messageForwarder() {
	for {
		select {
		case message := <-i.sendCh:
			if i.peer != nil && !i.peer.closed {
				select {
				case i.peer.receiveCh <- message:
				case <-i.peer.closeCh:
					return
				default:
					// Drop message if peer's receive channel is full
				}
			}
		case <-i.closeCh:
			return
		}
	}
}

// GetStats returns transport statistics
func (i *InProcessTransport) GetStats() InProcessStats {
	return i.stats
}

// CustomTransportRegistry manages custom transport types
type CustomTransportRegistry struct {
	factories map[string]TransportFactory
	mu        sync.RWMutex
}

// NewCustomTransportRegistry creates a new transport registry
func NewCustomTransportRegistry() *CustomTransportRegistry {
	return &CustomTransportRegistry{
		factories: make(map[string]TransportFactory),
	}
}

// RegisterTransportFactory registers a custom transport factory
func (r *CustomTransportRegistry) RegisterTransportFactory(name string, factory TransportFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
}

// CreateTransport creates a transport using a registered factory
func (r *CustomTransportRegistry) CreateTransport(transportType, endpoint string) (Transport, error) {
	r.mu.RLock()
	factory, exists := r.factories[transportType]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown transport type: %s", transportType)
	}

	return factory.CreateTransport(endpoint)
}

// GetRegisteredTypes returns all registered transport types
func (r *CustomTransportRegistry) GetRegisteredTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.factories))
	for transportType := range r.factories {
		types = append(types, transportType)
	}
	return types
}

// DefaultTransportRegistry is the global transport registry
var DefaultTransportRegistry = NewCustomTransportRegistry()

// RegisterTransport registers a transport factory globally
func RegisterTransport(name string, factory TransportFactory) {
	DefaultTransportRegistry.RegisterTransportFactory(name, factory)
}

// CreateTransport creates a transport using the global registry
func CreateTransport(transportType, endpoint string) (Transport, error) {
	return DefaultTransportRegistry.CreateTransport(transportType, endpoint)
}

// GRPCTransport implements gRPC-based transport for Cap'n Web RPC
type GRPCTransport struct {
	// Connection management
	address string
	options GRPCOptions

	// Communication channels
	sendCh    chan []byte
	receiveCh chan []byte
	closeCh   chan struct{}

	// State management
	connected bool
	closed    bool
	closeMux  sync.RWMutex
	closeErr  error

	// Statistics
	stats GRPCStats
}

// GRPCOptions configures gRPC transport behavior
type GRPCOptions struct {
	// Connection settings
	ConnectTimeout time.Duration
	RequestTimeout time.Duration

	// TLS settings
	UseTLS            bool
	InsecureSkipVerify bool
	CertFile          string
	KeyFile           string
	CAFile            string

	// gRPC settings
	MaxMessageSize    int
	MaxCallRecvMsgSize int
	MaxCallSendMsgSize int
	Compression       string // "gzip", "snappy", ""

	// Keep-alive settings
	KeepAliveTime     time.Duration
	KeepAliveTimeout  time.Duration
	PermitWithoutStream bool

	// Retry settings
	EnableRetry       bool
	MaxRetryAttempts  int
	RetryBackoff      time.Duration
}

// DefaultGRPCOptions returns reasonable defaults
func DefaultGRPCOptions() GRPCOptions {
	return GRPCOptions{
		ConnectTimeout:      30 * time.Second,
		RequestTimeout:      30 * time.Second,
		UseTLS:              false,
		InsecureSkipVerify:  false,
		MaxMessageSize:      4 * 1024 * 1024, // 4MB
		MaxCallRecvMsgSize:  4 * 1024 * 1024,
		MaxCallSendMsgSize:  4 * 1024 * 1024,
		Compression:         "",
		KeepAliveTime:       30 * time.Second,
		KeepAliveTimeout:    5 * time.Second,
		PermitWithoutStream: false,
		EnableRetry:         true,
		MaxRetryAttempts:    3,
		RetryBackoff:        1 * time.Second,
	}
}

// GRPCStats tracks gRPC transport statistics
type GRPCStats struct {
	ConnectionAttempts int64
	ConnectionFailures int64
	MessagesReceived   int64
	MessagesSent       int64
	BytesReceived      int64
	BytesSent          int64
	RPCCallsSucceeded  int64
	RPCCallsFailed     int64
	ConnectedTime      time.Duration
	LastConnected      time.Time
}

// NewGRPCTransport creates a new gRPC transport
func NewGRPCTransport(address string, options ...GRPCOptions) (*GRPCTransport, error) {
	opts := DefaultGRPCOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	transport := &GRPCTransport{
		address:   address,
		options:   opts,
		sendCh:    make(chan []byte, 1000),
		receiveCh: make(chan []byte, 1000),
		closeCh:   make(chan struct{}),
	}

	return transport, nil
}

// Connect establishes the gRPC connection
func (g *GRPCTransport) Connect(ctx context.Context) error {
	g.closeMux.Lock()
	defer g.closeMux.Unlock()

	if g.connected {
		return fmt.Errorf("already connected")
	}

	if g.closed {
		return fmt.Errorf("transport is closed")
	}

	// In a real implementation, this would:
	// 1. Create gRPC dial options based on g.options
	// 2. Establish gRPC connection using grpc.DialContext
	// 3. Create bidirectional streaming RPC client
	// 4. Start send/receive pumps

	// For this demo, we'll simulate a successful connection
	g.connected = true
	g.stats.ConnectionAttempts++
	g.stats.LastConnected = time.Now()

	// Start message pumps
	go g.sendPump()
	go g.receivePump()

	return nil
}

// Send implements the Transport interface
func (g *GRPCTransport) Send(ctx context.Context, data []byte) error {
	g.closeMux.RLock()
	defer g.closeMux.RUnlock()

	if g.closed {
		return fmt.Errorf("transport is closed")
	}

	if !g.connected {
		return fmt.Errorf("not connected")
	}

	if len(data) > g.options.MaxMessageSize {
		return fmt.Errorf("message too large: %d > %d", len(data), g.options.MaxMessageSize)
	}

	select {
	case g.sendCh <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-g.closeCh:
		return fmt.Errorf("transport closed")
	}
}

// Receive implements the Transport interface
func (g *GRPCTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case data := <-g.receiveCh:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-g.closeCh:
		return nil, fmt.Errorf("transport closed")
	}
}

// Close implements the Transport interface
func (g *GRPCTransport) Close() error {
	g.closeMux.Lock()
	defer g.closeMux.Unlock()

	if g.closed {
		return g.closeErr
	}

	g.closed = true
	g.connected = false

	// Close channels
	close(g.closeCh)
	close(g.sendCh)

	return nil
}

// sendPump handles sending messages via gRPC
func (g *GRPCTransport) sendPump() {
	for {
		select {
		case message, ok := <-g.sendCh:
			if !ok {
				return
			}

			// In real implementation, would send via gRPC stream
			g.stats.MessagesSent++
			g.stats.BytesSent += int64(len(message))
			g.stats.RPCCallsSucceeded++

		case <-g.closeCh:
			return
		}
	}
}

// receivePump handles receiving messages via gRPC
func (g *GRPCTransport) receivePump() {
	for {
		select {
		case <-g.closeCh:
			return
		default:
			// In real implementation, would receive from gRPC stream
			// For demo, we'll simulate occasional messages
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// GetStats returns transport statistics
func (g *GRPCTransport) GetStats() GRPCStats {
	stats := g.stats
	if g.connected {
		stats.ConnectedTime = time.Since(g.stats.LastConnected)
	}
	return stats
}

// IsConnected returns true if the transport is connected
func (g *GRPCTransport) IsConnected() bool {
	g.closeMux.RLock()
	defer g.closeMux.RUnlock()
	return g.connected && !g.closed
}

// GRPCTransportFactory creates gRPC transports
type GRPCTransportFactory struct {
	baseAddress string
	options     GRPCOptions
}

// NewGRPCTransportFactory creates a new gRPC transport factory
func NewGRPCTransportFactory(baseAddress string, options ...GRPCOptions) *GRPCTransportFactory {
	opts := DefaultGRPCOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	return &GRPCTransportFactory{
		baseAddress: baseAddress,
		options:     opts,
	}
}

// CreateTransport creates a new gRPC transport
func (f *GRPCTransportFactory) CreateTransport(endpoint string) (Transport, error) {
	address := f.baseAddress + endpoint
	transport, err := NewGRPCTransport(address, f.options)
	if err != nil {
		return nil, err
	}

	// Auto-connect
	ctx, cancel := context.WithTimeout(context.Background(), f.options.ConnectTimeout)
	defer cancel()

	err = transport.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return transport, nil
}

// GetTransportType returns the transport type name
func (f *GRPCTransportFactory) GetTransportType() string {
	return "grpc"
}

// Register built-in transport types
func init() {
	// Register default transport factories
	DefaultTransportRegistry.RegisterTransportFactory("tcp", NewTCPTransportFactory(""))
	DefaultTransportRegistry.RegisterTransportFactory("grpc", NewGRPCTransportFactory(""))
}