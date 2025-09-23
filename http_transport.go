package capnweb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// HTTPBatchTransport implements HTTP batch transport for Cap'n Web RPC
type HTTPBatchTransport struct {
	// Configuration
	url        string
	client     *http.Client
	options    HTTPBatchOptions

	// Batching
	batch      [][]byte
	batchMux   sync.Mutex
	batchTimer *time.Timer

	// Communication channels
	outbound chan []byte
	inbound  chan []byte

	// State management
	closed   bool
	closeMux sync.RWMutex
	closeErr error

	// Statistics
	stats HTTPBatchStats
}

// HTTPBatchOptions configures HTTP batch transport behavior
type HTTPBatchOptions struct {
	// BatchSize is the maximum number of messages per batch
	BatchSize int

	// BatchTimeout is the maximum time to wait before sending a batch
	BatchTimeout time.Duration

	// MaxRetries is the number of retry attempts for failed requests
	MaxRetries int

	// RetryDelay is the delay between retry attempts
	RetryDelay time.Duration

	// RequestTimeout is the timeout for individual HTTP requests
	RequestTimeout time.Duration

	// Headers are custom HTTP headers to include in requests
	Headers map[string]string

	// Compression enables gzip compression for request bodies
	Compression bool

	// KeepAlive enables HTTP keep-alive connections
	KeepAlive bool

	// MaxIdleConns sets the maximum number of idle HTTP connections
	MaxIdleConns int

	// TLSConfig provides custom TLS configuration
	TLSConfig interface{} // *tls.Config in production
}

// DefaultHTTPBatchOptions returns reasonable defaults
func DefaultHTTPBatchOptions() HTTPBatchOptions {
	return HTTPBatchOptions{
		BatchSize:      50,
		BatchTimeout:   100 * time.Millisecond,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
		RequestTimeout: 30 * time.Second,
		Headers:        make(map[string]string),
		Compression:    true,
		KeepAlive:      true,
		MaxIdleConns:   10,
	}
}

// HTTPBatchStats tracks HTTP transport statistics
type HTTPBatchStats struct {
	RequestsSent      int64
	RequestsSucceeded int64
	RequestsFailed    int64
	BatchesSent       int64
	BytesSent         int64
	BytesReceived     int64
	AverageLatency    time.Duration
	Retries           int64
}

// NewHTTPBatchTransport creates a new HTTP batch transport
func NewHTTPBatchTransport(url string, options ...HTTPBatchOptions) (*HTTPBatchTransport, error) {
	opts := DefaultHTTPBatchOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	// Set default headers
	if opts.Headers == nil {
		opts.Headers = make(map[string]string)
	}
	opts.Headers["Content-Type"] = "application/json"
	opts.Headers["Accept"] = "application/json"
	opts.Headers["User-Agent"] = "capnweb-go/1.0"

	// Create HTTP client
	client := &http.Client{
		Timeout: opts.RequestTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        opts.MaxIdleConns,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  !opts.Compression,
			DisableKeepAlives:   !opts.KeepAlive,
		},
	}

	transport := &HTTPBatchTransport{
		url:      url,
		client:   client,
		options:  opts,
		outbound: make(chan []byte, 1000),
		inbound:  make(chan []byte, 1000),
	}

	// Start batch processor
	go transport.batchProcessor()

	return transport, nil
}

// Send implements the Transport interface
func (h *HTTPBatchTransport) Send(ctx context.Context, data []byte) error {
	h.closeMux.RLock()
	if h.closed {
		h.closeMux.RUnlock()
		return fmt.Errorf("transport is closed")
	}
	h.closeMux.RUnlock()

	// Add to batch queue
	select {
	case h.outbound <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("send queue full")
	}
}

// Receive implements the Transport interface
func (h *HTTPBatchTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case data := <-h.inbound:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close implements the Transport interface
func (h *HTTPBatchTransport) Close() error {
	h.closeMux.Lock()
	defer h.closeMux.Unlock()

	if h.closed {
		return h.closeErr
	}

	h.closed = true
	close(h.outbound)

	// Send any remaining batch
	h.flushBatch()

	return nil
}

// batchProcessor handles batching and sending of messages
func (h *HTTPBatchTransport) batchProcessor() {
	for {
		select {
		case data, ok := <-h.outbound:
			if !ok {
				// Channel closed, send remaining batch and exit
				h.flushBatch()
				return
			}

			h.batchMux.Lock()
			h.batch = append(h.batch, data)

			// Check if batch is full
			if len(h.batch) >= h.options.BatchSize {
				h.sendBatch()
			} else if len(h.batch) == 1 {
				// Start batch timer for first message
				h.batchTimer = time.AfterFunc(h.options.BatchTimeout, func() {
					h.batchMux.Lock()
					if len(h.batch) > 0 {
						h.sendBatch()
					}
					h.batchMux.Unlock()
				})
			}
			h.batchMux.Unlock()
		}
	}
}

// sendBatch sends the current batch of messages
func (h *HTTPBatchTransport) sendBatch() {
	if len(h.batch) == 0 {
		return
	}

	batch := make([][]byte, len(h.batch))
	copy(batch, h.batch)
	h.batch = h.batch[:0]

	// Cancel timer if active
	if h.batchTimer != nil {
		h.batchTimer.Stop()
		h.batchTimer = nil
	}

	// Send batch in background
	go h.sendHTTPBatch(batch)
}

// flushBatch sends any remaining messages in the batch
func (h *HTTPBatchTransport) flushBatch() {
	h.batchMux.Lock()
	defer h.batchMux.Unlock()
	h.sendBatch()
}

// sendHTTPBatch sends a batch of messages via HTTP
func (h *HTTPBatchTransport) sendHTTPBatch(batch [][]byte) {
	startTime := time.Now()

	// Prepare request body
	requestBody, err := h.prepareBatchRequest(batch)
	if err != nil {
		h.stats.RequestsFailed++
		return
	}

	// Send request with retries
	var lastErr error
	for attempt := 0; attempt <= h.options.MaxRetries; attempt++ {
		if attempt > 0 {
			h.stats.Retries++
			time.Sleep(h.options.RetryDelay * time.Duration(attempt))
		}

		response, err := h.sendHTTPRequest(requestBody)
		if err != nil {
			lastErr = err
			continue
		}

		// Process response
		err = h.processHTTPResponse(response)
		if err != nil {
			lastErr = err
			continue
		}

		// Success
		h.stats.RequestsSent++
		h.stats.RequestsSucceeded++
		h.stats.BatchesSent++
		h.stats.BytesSent += int64(len(requestBody))

		// Update latency
		latency := time.Since(startTime)
		h.updateAverageLatency(latency)

		return
	}

	// All retries failed
	h.stats.RequestsFailed++
	// In production, you might want to log this error or send to an error channel
	_ = lastErr
}

// prepareBatchRequest prepares the HTTP request body for a batch
func (h *HTTPBatchTransport) prepareBatchRequest(batch [][]byte) ([]byte, error) {
	// Convert byte slices to json.RawMessage for proper JSON encoding
	jsonMessages := make([]json.RawMessage, len(batch))
	for i, message := range batch {
		jsonMessages[i] = json.RawMessage(message)
	}

	// Marshal as JSON array
	return json.Marshal(jsonMessages)
}

// sendHTTPRequest sends an HTTP request
func (h *HTTPBatchTransport) sendHTTPRequest(body []byte) ([]byte, error) {
	req, err := http.NewRequest("POST", h.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	for key, value := range h.options.Headers {
		req.Header.Set(key, value)
	}

	// Add content length
	req.ContentLength = int64(len(body))

	// Send request
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP error: %d %s", resp.StatusCode, resp.Status)
	}

	// Read response body
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	h.stats.BytesReceived += int64(len(responseBody))
	return responseBody, nil
}

// processHTTPResponse processes the HTTP response
func (h *HTTPBatchTransport) processHTTPResponse(responseBody []byte) error {
	if len(responseBody) == 0 {
		return nil
	}

	// Try to parse as JSON array first
	var jsonMessages []json.RawMessage
	if err := json.Unmarshal(responseBody, &jsonMessages); err == nil {
		// Successfully parsed as array - forward each message
		for _, message := range jsonMessages {
			select {
			case h.inbound <- []byte(message):
			default:
				return fmt.Errorf("inbound queue full")
			}
		}
		return nil
	}

	// Not a JSON array - treat as single message
	select {
	case h.inbound <- responseBody:
		return nil
	default:
		return fmt.Errorf("inbound queue full")
	}
}

// updateAverageLatency updates the running average latency
func (h *HTTPBatchTransport) updateAverageLatency(latency time.Duration) {
	// Simple moving average
	if h.stats.AverageLatency == 0 {
		h.stats.AverageLatency = latency
	} else {
		h.stats.AverageLatency = (h.stats.AverageLatency + latency) / 2
	}
}

// GetStats returns transport statistics
func (h *HTTPBatchTransport) GetStats() HTTPBatchStats {
	return h.stats
}

// SetHeader sets a custom HTTP header
func (h *HTTPBatchTransport) SetHeader(key, value string) {
	h.options.Headers[key] = value
}

// SetBatchSize updates the batch size
func (h *HTTPBatchTransport) SetBatchSize(size int) {
	h.batchMux.Lock()
	h.options.BatchSize = size
	h.batchMux.Unlock()
}

// SetBatchTimeout updates the batch timeout
func (h *HTTPBatchTransport) SetBatchTimeout(timeout time.Duration) {
	h.batchMux.Lock()
	h.options.BatchTimeout = timeout
	h.batchMux.Unlock()
}

// HTTPBatchTransportFactory creates HTTP batch transports
type HTTPBatchTransportFactory struct {
	baseURL string
	options HTTPBatchOptions
}

// NewHTTPBatchTransportFactory creates a new factory
func NewHTTPBatchTransportFactory(baseURL string, options ...HTTPBatchOptions) *HTTPBatchTransportFactory {
	opts := DefaultHTTPBatchOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	return &HTTPBatchTransportFactory{
		baseURL: baseURL,
		options: opts,
	}
}

// CreateTransport creates a new HTTP batch transport
func (f *HTTPBatchTransportFactory) CreateTransport(endpoint string) (Transport, error) {
	fullURL := f.baseURL + endpoint
	return NewHTTPBatchTransport(fullURL, f.options)
}

// HTTPServer provides server-side HTTP batch handling
type HTTPServer struct {
	handler func([][]byte) [][]byte
	options HTTPServerOptions
}

// HTTPServerOptions configures HTTP server behavior
type HTTPServerOptions struct {
	MaxRequestSize  int64
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	EnableCORS      bool
	CORSOrigins     []string
	EnableGzip      bool
	MaxConcurrent   int
}

// DefaultHTTPServerOptions returns server defaults
func DefaultHTTPServerOptions() HTTPServerOptions {
	return HTTPServerOptions{
		MaxRequestSize: 10 * 1024 * 1024, // 10MB
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		EnableCORS:     true,
		CORSOrigins:    []string{"*"},
		EnableGzip:     true,
		MaxConcurrent:  100,
	}
}

// NewHTTPServer creates a new HTTP server
func NewHTTPServer(handler func([][]byte) [][]byte, options ...HTTPServerOptions) *HTTPServer {
	opts := DefaultHTTPServerOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	return &HTTPServer{
		handler: handler,
		options: opts,
	}
}

// ServeHTTP implements http.Handler
func (s *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers if enabled
	if s.options.EnableCORS {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	// Only allow POST
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check content type
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusBadRequest)
		return
	}

	// Limit request size
	r.Body = http.MaxBytesReader(w, r.Body, s.options.MaxRequestSize)

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Parse as batch
	batch, err := s.parseBatchRequest(body)
	if err != nil {
		http.Error(w, "Invalid JSON batch", http.StatusBadRequest)
		return
	}

	// Process batch
	responses := s.handler(batch)

	// Send response
	w.Header().Set("Content-Type", "application/json")

	if len(responses) == 1 {
		w.Write(responses[0])
	} else {
		// Multiple responses - send as JSON array
		jsonResponses := make([]json.RawMessage, len(responses))
		for i, response := range responses {
			jsonResponses[i] = json.RawMessage(response)
		}

		responseBody, err := json.Marshal(jsonResponses)
		if err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}

		w.Write(responseBody)
	}
}

// parseBatchRequest parses a request body as either a single message or JSON array
func (s *HTTPServer) parseBatchRequest(body []byte) ([][]byte, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("empty request body")
	}

	// Try to parse as JSON array first
	var jsonMessages []json.RawMessage
	if err := json.Unmarshal(body, &jsonMessages); err == nil {
		// Successfully parsed as array
		batch := make([][]byte, len(jsonMessages))
		for i, message := range jsonMessages {
			batch[i] = []byte(message)
		}
		return batch, nil
	}

	// Not a JSON array - treat as single message
	return [][]byte{body}, nil
}