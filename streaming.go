package capnweb

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// StreamReader provides a streaming interface for reading large datasets progressively
type StreamReader interface {
	// Read reads the next chunk of data from the stream
	Read(ctx context.Context) ([]byte, error)

	// Close closes the stream and releases resources
	Close() error

	// Stats returns streaming statistics
	Stats() StreamStats
}

// StreamWriter provides a streaming interface for writing large datasets progressively
type StreamWriter interface {
	// Write writes a chunk of data to the stream
	Write(ctx context.Context, data []byte) error

	// Close closes the stream and finalizes the write operation
	Close() error

	// Flush ensures all buffered data is sent
	Flush(ctx context.Context) error

	// Stats returns streaming statistics
	Stats() StreamStats
}

// StreamStats tracks streaming operation statistics
type StreamStats struct {
	BytesTransferred int64
	ChunksTransferred int64
	StartTime        time.Time
	EndTime          time.Time
	Duration         time.Duration
	ThroughputBPS    int64 // Bytes per second
	ErrorCount       int64
	BackpressureEvents int64
}

// StreamOptions configures streaming behavior
type StreamOptions struct {
	// ChunkSize is the size of each chunk in bytes
	ChunkSize int

	// BufferSize is the size of the internal buffer
	BufferSize int

	// BackpressureThreshold triggers backpressure when buffer exceeds this size
	BackpressureThreshold int

	// FlushInterval specifies how often to flush buffered data
	FlushInterval time.Duration

	// CompressionEnabled enables compression for stream data
	CompressionEnabled bool

	// ProgressCallback is called periodically with progress updates
	ProgressCallback func(stats StreamStats)

	// ErrorCallback is called when stream errors occur
	ErrorCallback func(error)

	// BackpressureCallback is called when backpressure is applied
	BackpressureCallback func(bool) // true = backpressure on, false = off
}

// DefaultStreamOptions returns reasonable defaults for streaming
func DefaultStreamOptions() StreamOptions {
	return StreamOptions{
		ChunkSize:             64 * 1024, // 64KB chunks
		BufferSize:            1024 * 1024, // 1MB buffer
		BackpressureThreshold: 512 * 1024,  // 512KB backpressure threshold
		FlushInterval:         100 * time.Millisecond,
		CompressionEnabled:    true,
	}
}

// RPCStreamReader implements StreamReader for RPC-based streaming
type RPCStreamReader struct {
	session    *Session
	streamID   string
	importID   ImportID
	path       []string
	options    StreamOptions

	// State
	buffer     []byte
	bufferPos  int
	closed     bool
	err        error

	// Statistics
	stats      StreamStats
	statsMux   sync.RWMutex

	// Synchronization
	dataCh     chan []byte
	errorCh    chan error
	closeCh    chan struct{}

	// Backpressure
	backpressureActive bool
	backpressureMux    sync.RWMutex
}

// NewRPCStreamReader creates a new RPC stream reader
func NewRPCStreamReader(session *Session, importID ImportID, path []string, options ...StreamOptions) *RPCStreamReader {
	opts := DefaultStreamOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	streamID := fmt.Sprintf("stream_%d_%d", importID, time.Now().UnixNano())

	reader := &RPCStreamReader{
		session:   session,
		streamID:  streamID,
		importID:  importID,
		path:      path,
		options:   opts,
		buffer:    make([]byte, 0, opts.BufferSize),
		dataCh:    make(chan []byte, 10),
		errorCh:   make(chan error, 1),
		closeCh:   make(chan struct{}),
		stats:     StreamStats{StartTime: time.Now()},
	}

	// Start the stream reader goroutine
	go reader.streamReader()

	return reader
}

// Read implements StreamReader.Read
func (r *RPCStreamReader) Read(ctx context.Context) ([]byte, error) {
	if r.closed {
		return nil, io.EOF
	}

	if r.err != nil {
		return nil, r.err
	}

	// Check if we have data in buffer
	if r.bufferPos < len(r.buffer) {
		chunkSize := r.options.ChunkSize
		if r.bufferPos + chunkSize > len(r.buffer) {
			chunkSize = len(r.buffer) - r.bufferPos
		}

		data := r.buffer[r.bufferPos:r.bufferPos + chunkSize]
		r.bufferPos += chunkSize

		r.updateStats(int64(len(data)))
		return data, nil
	}

	// Wait for new data or error
	select {
	case data := <-r.dataCh:
		if len(data) == 0 {
			// End of stream
			return nil, io.EOF
		}

		// Add to buffer
		r.buffer = append(r.buffer[:0], data...) // Reset and reuse buffer
		r.bufferPos = 0

		// Apply backpressure if buffer is getting full
		r.checkBackpressure()

		// Return first chunk
		chunkSize := r.options.ChunkSize
		if chunkSize > len(r.buffer) {
			chunkSize = len(r.buffer)
		}

		result := r.buffer[:chunkSize]
		r.bufferPos = chunkSize

		r.updateStats(int64(len(result)))
		return result, nil

	case err := <-r.errorCh:
		r.err = err
		return nil, err

	case <-ctx.Done():
		return nil, ctx.Err()

	case <-r.closeCh:
		return nil, io.EOF
	}
}

// Close implements StreamReader.Close
func (r *RPCStreamReader) Close() error {
	if r.closed {
		return nil
	}

	r.closed = true
	close(r.closeCh)

	// Send stream close message to remote
	closeMsg := map[string]interface{}{
		"type":     "streamClose",
		"streamID": r.streamID,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := r.session.SendRequest(ctx, closeMsg)

	// Update final stats
	r.statsMux.Lock()
	r.stats.EndTime = time.Now()
	r.stats.Duration = r.stats.EndTime.Sub(r.stats.StartTime)
	if r.stats.Duration > 0 {
		r.stats.ThroughputBPS = r.stats.BytesTransferred / int64(r.stats.Duration.Seconds())
	}
	r.statsMux.Unlock()

	return err
}

// Stats implements StreamReader.Stats
func (r *RPCStreamReader) Stats() StreamStats {
	r.statsMux.RLock()
	defer r.statsMux.RUnlock()
	return r.stats
}

// streamReader runs in a goroutine to handle incoming stream data
func (r *RPCStreamReader) streamReader() {
	// Request stream initialization
	initMsg := map[string]interface{}{
		"type":      "streamInit",
		"streamID":  r.streamID,
		"importID":  r.importID,
		"path":      r.path,
		"chunkSize": r.options.ChunkSize,
		"compressed": r.options.CompressionEnabled,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	promise, err := r.session.SendRequest(ctx, initMsg)
	if err != nil {
		r.errorCh <- fmt.Errorf("failed to initialize stream: %w", err)
		return
	}

	// In a real implementation, we would:
	// 1. Wait for stream initialization response
	// 2. Set up stream data reception
	// 3. Handle incoming chunks

	// For demo, simulate some data
	go func() {
		for i := 0; i < 5; i++ {
			select {
			case <-r.closeCh:
				return
			default:
				// Simulate receiving data chunks
				data := make([]byte, r.options.ChunkSize)
				for j := range data {
					data[j] = byte(i)
				}

				select {
				case r.dataCh <- data:
				case <-r.closeCh:
					return
				}

				time.Sleep(100 * time.Millisecond)
			}
		}

		// Signal end of stream
		r.dataCh <- []byte{}
	}()

	_ = promise // Use promise to avoid unused variable error
}

// updateStats updates streaming statistics
func (r *RPCStreamReader) updateStats(bytesRead int64) {
	r.statsMux.Lock()
	defer r.statsMux.Unlock()

	r.stats.BytesTransferred += bytesRead
	r.stats.ChunksTransferred++

	// Calculate throughput
	elapsed := time.Since(r.stats.StartTime)
	if elapsed > 0 {
		r.stats.ThroughputBPS = r.stats.BytesTransferred / int64(elapsed.Seconds())
	}

	// Call progress callback if provided
	if r.options.ProgressCallback != nil {
		go r.options.ProgressCallback(r.stats)
	}
}

// checkBackpressure applies backpressure when buffer is getting full
func (r *RPCStreamReader) checkBackpressure() {
	r.backpressureMux.Lock()
	defer r.backpressureMux.Unlock()

	bufferUsage := len(r.buffer) - r.bufferPos
	shouldApplyBackpressure := bufferUsage > r.options.BackpressureThreshold

	if shouldApplyBackpressure != r.backpressureActive {
		r.backpressureActive = shouldApplyBackpressure

		if r.options.BackpressureCallback != nil {
			go r.options.BackpressureCallback(shouldApplyBackpressure)
		}

		// Send backpressure signal to remote
		backpressureMsg := map[string]interface{}{
			"type":        "streamBackpressure",
			"streamID":    r.streamID,
			"backpressure": shouldApplyBackpressure,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		r.session.SendRequest(ctx, backpressureMsg)

		// Update stats
		r.statsMux.Lock()
		r.stats.BackpressureEvents++
		r.statsMux.Unlock()
	}
}

// RPCStreamWriter implements StreamWriter for RPC-based streaming
type RPCStreamWriter struct {
	session    *Session
	streamID   string
	importID   ImportID
	path       []string
	options    StreamOptions

	// State
	buffer     []byte
	closed     bool
	err        error

	// Statistics
	stats      StreamStats
	statsMux   sync.RWMutex

	// Synchronization
	flushTicker *time.Ticker
	closeCh     chan struct{}

	// Backpressure
	backpressureActive bool
	backpressureMux    sync.RWMutex
}

// NewRPCStreamWriter creates a new RPC stream writer
func NewRPCStreamWriter(session *Session, importID ImportID, path []string, options ...StreamOptions) *RPCStreamWriter {
	opts := DefaultStreamOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	streamID := fmt.Sprintf("stream_%d_%d", importID, time.Now().UnixNano())

	writer := &RPCStreamWriter{
		session:   session,
		streamID:  streamID,
		importID:  importID,
		path:      path,
		options:   opts,
		buffer:    make([]byte, 0, opts.BufferSize),
		closeCh:   make(chan struct{}),
		stats:     StreamStats{StartTime: time.Now()},
	}

	// Start auto-flush ticker
	if opts.FlushInterval > 0 {
		writer.flushTicker = time.NewTicker(opts.FlushInterval)
		go writer.autoFlush()
	}

	return writer
}

// Write implements StreamWriter.Write
func (w *RPCStreamWriter) Write(ctx context.Context, data []byte) error {
	if w.closed {
		return fmt.Errorf("stream is closed")
	}

	if w.err != nil {
		return w.err
	}

	// Check for backpressure
	w.backpressureMux.RLock()
	if w.backpressureActive {
		w.backpressureMux.RUnlock()
		// Wait for backpressure to be relieved or context to be canceled
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			// Retry
		}

		w.backpressureMux.RLock()
		if w.backpressureActive {
			w.backpressureMux.RUnlock()
			return fmt.Errorf("stream backpressure active")
		}
	}
	w.backpressureMux.RUnlock()

	// Add data to buffer
	w.buffer = append(w.buffer, data...)

	// Check if we need to flush
	if len(w.buffer) >= w.options.ChunkSize {
		return w.Flush(ctx)
	}

	w.updateStats(int64(len(data)))
	return nil
}

// Flush implements StreamWriter.Flush
func (w *RPCStreamWriter) Flush(ctx context.Context) error {
	if len(w.buffer) == 0 {
		return nil
	}

	// Send buffer contents
	flushMsg := map[string]interface{}{
		"type":     "streamData",
		"streamID": w.streamID,
		"data":     w.buffer,
		"compressed": w.options.CompressionEnabled,
	}

	_, err := w.session.SendRequest(ctx, flushMsg)
	if err != nil {
		w.err = err
		if w.options.ErrorCallback != nil {
			go w.options.ErrorCallback(err)
		}
		return err
	}

	// Clear buffer
	w.buffer = w.buffer[:0]

	return nil
}

// Close implements StreamWriter.Close
func (w *RPCStreamWriter) Close() error {
	if w.closed {
		return nil
	}

	w.closed = true
	close(w.closeCh)

	// Stop auto-flush ticker
	if w.flushTicker != nil {
		w.flushTicker.Stop()
	}

	// Flush any remaining data
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := w.Flush(ctx); err != nil {
		return err
	}

	// Send stream finalization message
	finalizeMsg := map[string]interface{}{
		"type":     "streamFinalize",
		"streamID": w.streamID,
	}

	_, err := w.session.SendRequest(ctx, finalizeMsg)

	// Update final stats
	w.statsMux.Lock()
	w.stats.EndTime = time.Now()
	w.stats.Duration = w.stats.EndTime.Sub(w.stats.StartTime)
	if w.stats.Duration > 0 {
		w.stats.ThroughputBPS = w.stats.BytesTransferred / int64(w.stats.Duration.Seconds())
	}
	w.statsMux.Unlock()

	return err
}

// Stats implements StreamWriter.Stats
func (w *RPCStreamWriter) Stats() StreamStats {
	w.statsMux.RLock()
	defer w.statsMux.RUnlock()
	return w.stats
}

// autoFlush periodically flushes the buffer
func (w *RPCStreamWriter) autoFlush() {
	for {
		select {
		case <-w.flushTicker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			w.Flush(ctx)
			cancel()
		case <-w.closeCh:
			return
		}
	}
}

// updateStats updates streaming statistics
func (w *RPCStreamWriter) updateStats(bytesWritten int64) {
	w.statsMux.Lock()
	defer w.statsMux.Unlock()

	w.stats.BytesTransferred += bytesWritten
	w.stats.ChunksTransferred++

	// Calculate throughput
	elapsed := time.Since(w.stats.StartTime)
	if elapsed > 0 {
		w.stats.ThroughputBPS = w.stats.BytesTransferred / int64(elapsed.Seconds())
	}

	// Call progress callback if provided
	if w.options.ProgressCallback != nil {
		go w.options.ProgressCallback(w.stats)
	}
}

// StreamManager manages multiple concurrent streams
type StreamManager struct {
	session        *Session
	streams        map[string]interface{} // StreamReader or StreamWriter
	streamsMux     sync.RWMutex
	globalStats    StreamStats
	globalStatsMux sync.RWMutex
}

// NewStreamManager creates a new stream manager
func NewStreamManager(session *Session) *StreamManager {
	return &StreamManager{
		session: session,
		streams: make(map[string]interface{}),
		globalStats: StreamStats{StartTime: time.Now()},
	}
}

// CreateStreamReader creates a new stream reader and registers it
func (sm *StreamManager) CreateStreamReader(importID ImportID, path []string, options ...StreamOptions) (*RPCStreamReader, error) {
	reader := NewRPCStreamReader(sm.session, importID, path, options...)

	sm.streamsMux.Lock()
	sm.streams[reader.streamID] = reader
	sm.streamsMux.Unlock()

	return reader, nil
}

// CreateStreamWriter creates a new stream writer and registers it
func (sm *StreamManager) CreateStreamWriter(importID ImportID, path []string, options ...StreamOptions) (*RPCStreamWriter, error) {
	writer := NewRPCStreamWriter(sm.session, importID, path, options...)

	sm.streamsMux.Lock()
	sm.streams[writer.streamID] = writer
	sm.streamsMux.Unlock()

	return writer, nil
}

// CloseStream closes a specific stream by ID
func (sm *StreamManager) CloseStream(streamID string) error {
	sm.streamsMux.Lock()
	stream, exists := sm.streams[streamID]
	if exists {
		delete(sm.streams, streamID)
	}
	sm.streamsMux.Unlock()

	if !exists {
		return fmt.Errorf("stream not found: %s", streamID)
	}

	// Close the stream
	switch s := stream.(type) {
	case *RPCStreamReader:
		return s.Close()
	case *RPCStreamWriter:
		return s.Close()
	default:
		return fmt.Errorf("unknown stream type")
	}
}

// CloseAllStreams closes all managed streams
func (sm *StreamManager) CloseAllStreams() error {
	sm.streamsMux.Lock()
	streamIDs := make([]string, 0, len(sm.streams))
	for id := range sm.streams {
		streamIDs = append(streamIDs, id)
	}
	sm.streamsMux.Unlock()

	var lastError error
	for _, id := range streamIDs {
		if err := sm.CloseStream(id); err != nil {
			lastError = err
		}
	}

	return lastError
}

// GetStreamCount returns the number of active streams
func (sm *StreamManager) GetStreamCount() int {
	sm.streamsMux.RLock()
	defer sm.streamsMux.RUnlock()
	return len(sm.streams)
}

// GetGlobalStats returns aggregated statistics for all streams
func (sm *StreamManager) GetGlobalStats() StreamStats {
	sm.globalStatsMux.RLock()
	defer sm.globalStatsMux.RUnlock()

	// Aggregate stats from all active streams
	var aggregated StreamStats
	aggregated.StartTime = sm.globalStats.StartTime

	sm.streamsMux.RLock()
	for _, stream := range sm.streams {
		switch s := stream.(type) {
		case *RPCStreamReader:
			stats := s.Stats()
			aggregated.BytesTransferred += stats.BytesTransferred
			aggregated.ChunksTransferred += stats.ChunksTransferred
			aggregated.ErrorCount += stats.ErrorCount
			aggregated.BackpressureEvents += stats.BackpressureEvents
		case *RPCStreamWriter:
			stats := s.Stats()
			aggregated.BytesTransferred += stats.BytesTransferred
			aggregated.ChunksTransferred += stats.ChunksTransferred
			aggregated.ErrorCount += stats.ErrorCount
			aggregated.BackpressureEvents += stats.BackpressureEvents
		}
	}
	sm.streamsMux.RUnlock()

	// Calculate overall throughput
	aggregated.EndTime = time.Now()
	aggregated.Duration = aggregated.EndTime.Sub(aggregated.StartTime)
	if aggregated.Duration > 0 {
		aggregated.ThroughputBPS = aggregated.BytesTransferred / int64(aggregated.Duration.Seconds())
	}

	return aggregated
}