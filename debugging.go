package capnweb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

// Tracer provides RPC call tracing and debugging capabilities
type Tracer struct {
	traces    map[string]*TraceSession
	tracesMu  sync.RWMutex
	enabled   bool
	output    io.Writer
	options   TracingOptions
}

// TracingOptions configures tracing behavior
type TracingOptions struct {
	// TraceLevel controls how much detail to capture
	TraceLevel TraceLevel

	// MaxTraces limits the number of concurrent trace sessions
	MaxTraces int

	// TraceTimeout automatically closes traces after this duration
	TraceTimeout time.Duration

	// IncludePayloads captures request/response payloads
	IncludePayloads bool

	// IncludeStackTraces captures stack traces for calls
	IncludeStackTraces bool

	// FilterMethods specifies which methods to trace (empty = all)
	FilterMethods []string

	// ExcludeMethods specifies which methods to exclude from tracing
	ExcludeMethods []string

	// SamplingRate controls what percentage of calls to trace (0.0 to 1.0)
	SamplingRate float64
}

// TraceLevel defines the level of detail in tracing
type TraceLevel int

const (
	TraceLevelOff TraceLevel = iota
	TraceLevelError
	TraceLevelWarn
	TraceLevelInfo
	TraceLevelDebug
	TraceLevelVerbose
)

// String returns the string representation of TraceLevel
func (tl TraceLevel) String() string {
	switch tl {
	case TraceLevelOff:
		return "OFF"
	case TraceLevelError:
		return "ERROR"
	case TraceLevelWarn:
		return "WARN"
	case TraceLevelInfo:
		return "INFO"
	case TraceLevelDebug:
		return "DEBUG"
	case TraceLevelVerbose:
		return "VERBOSE"
	default:
		return "UNKNOWN"
	}
}

// DefaultTracingOptions returns reasonable defaults for tracing
func DefaultTracingOptions() TracingOptions {
	return TracingOptions{
		TraceLevel:         TraceLevelInfo,
		MaxTraces:          100,
		TraceTimeout:       time.Hour,
		IncludePayloads:    true,
		IncludeStackTraces: false,
		SamplingRate:       1.0, // Trace everything by default
	}
}

// NewTracer creates a new RPC tracer
func NewTracer(output io.Writer, options TracingOptions) *Tracer {
	return &Tracer{
		traces:  make(map[string]*TraceSession),
		enabled: true,
		output:  output,
		options: options,
	}
}

// TraceSession represents a single tracing session
type TraceSession struct {
	ID        string
	Name      string
	StartTime time.Time
	EndTime   time.Time
	Spans     []*TraceSpan
	Metadata  map[string]interface{}
	mu        sync.RWMutex
}

// TraceSpan represents a single operation within a trace
type TraceSpan struct {
	ID            string
	ParentID      string
	OperationType OperationType
	Method        string
	StartTime     time.Time
	EndTime       time.Time
	Duration      time.Duration
	Tags          map[string]interface{}
	Logs          []*TraceLog
	Error         error
	Request       interface{}
	Response      interface{}
}

// OperationType describes the type of operation being traced
type OperationType string

const (
	OperationTypeCall     OperationType = "call"
	OperationTypeGet      OperationType = "get"
	OperationTypeResolve  OperationType = "resolve"
	OperationTypeReject   OperationType = "reject"
	OperationTypeRelease  OperationType = "release"
	OperationTypeTransport OperationType = "transport"
)

// TraceLog represents a log entry within a span
type TraceLog struct {
	Timestamp time.Time
	Level     TraceLevel
	Message   string
	Fields    map[string]interface{}
}

// StartTrace begins a new tracing session
func (t *Tracer) StartTrace(name string) *TraceSession {
	if !t.enabled {
		return nil
	}

	t.tracesMu.Lock()
	defer t.tracesMu.Unlock()

	// Check trace limits
	if len(t.traces) >= t.options.MaxTraces {
		// Remove oldest trace
		var oldestID string
		var oldestTime time.Time
		for id, trace := range t.traces {
			if oldestID == "" || trace.StartTime.Before(oldestTime) {
				oldestID = id
				oldestTime = trace.StartTime
			}
		}
		if oldestID != "" {
			delete(t.traces, oldestID)
		}
	}

	traceID := generateTraceID()
	session := &TraceSession{
		ID:        traceID,
		Name:      name,
		StartTime: time.Now(),
		Spans:     make([]*TraceSpan, 0),
		Metadata:  make(map[string]interface{}),
	}

	t.traces[traceID] = session
	return session
}

// EndTrace finishes a tracing session
func (t *Tracer) EndTrace(traceID string) {
	t.tracesMu.Lock()
	defer t.tracesMu.Unlock()

	if session, exists := t.traces[traceID]; exists {
		session.EndTime = time.Now()
		// Optionally export or persist the trace
		t.exportTrace(session)
	}
}

// StartSpan begins a new span within a trace
func (ts *TraceSession) StartSpan(operationType OperationType, method string) *TraceSpan {
	if ts == nil {
		return nil
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	span := &TraceSpan{
		ID:            generateSpanID(),
		OperationType: operationType,
		Method:        method,
		StartTime:     time.Now(),
		Tags:          make(map[string]interface{}),
		Logs:          make([]*TraceLog, 0),
	}

	ts.Spans = append(ts.Spans, span)
	return span
}

// FinishSpan completes a span
func (ts *TraceSession) FinishSpan(span *TraceSpan) {
	if span == nil {
		return
	}

	span.EndTime = time.Now()
	span.Duration = span.EndTime.Sub(span.StartTime)
}

// AddSpanTag adds a tag to a span
func (span *TraceSpan) AddTag(key string, value interface{}) {
	if span == nil {
		return
	}
	span.Tags[key] = value
}

// LogEvent adds a log event to a span
func (span *TraceSpan) LogEvent(level TraceLevel, message string, fields map[string]interface{}) {
	if span == nil {
		return
	}

	log := &TraceLog{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
		Fields:    fields,
	}

	span.Logs = append(span.Logs, log)
}

// SetError records an error for a span
func (span *TraceSpan) SetError(err error) {
	if span == nil {
		return
	}
	span.Error = err
	span.AddTag("error", true)
	span.AddTag("error.message", err.Error())
}

// SetRequest records the request payload for a span
func (span *TraceSpan) SetRequest(request interface{}) {
	if span == nil {
		return
	}
	span.Request = request
}

// SetResponse records the response payload for a span
func (span *TraceSpan) SetResponse(response interface{}) {
	if span == nil {
		return
	}
	span.Response = response
}

// GetTrace retrieves a trace by ID
func (t *Tracer) GetTrace(traceID string) *TraceSession {
	t.tracesMu.RLock()
	defer t.tracesMu.RUnlock()
	return t.traces[traceID]
}

// GetAllTraces returns all active traces
func (t *Tracer) GetAllTraces() []*TraceSession {
	t.tracesMu.RLock()
	defer t.tracesMu.RUnlock()

	traces := make([]*TraceSession, 0, len(t.traces))
	for _, trace := range t.traces {
		traces = append(traces, trace)
	}

	// Sort by start time
	sort.Slice(traces, func(i, j int) bool {
		return traces[i].StartTime.Before(traces[j].StartTime)
	})

	return traces
}

// ClearTraces removes all traces
func (t *Tracer) ClearTraces() {
	t.tracesMu.Lock()
	defer t.tracesMu.Unlock()
	t.traces = make(map[string]*TraceSession)
}

// SetEnabled enables or disables tracing
func (t *Tracer) SetEnabled(enabled bool) {
	t.enabled = enabled
}

// IsEnabled returns whether tracing is currently enabled
func (t *Tracer) IsEnabled() bool {
	return t.enabled
}

// exportTrace exports a completed trace
func (t *Tracer) exportTrace(session *TraceSession) {
	if t.output == nil {
		return
	}

	// Export as JSON
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return
	}

	fmt.Fprintf(t.output, "=== Trace: %s ===\n", session.Name)
	fmt.Fprintf(t.output, "%s\n", data)
	fmt.Fprintf(t.output, "=== End Trace ===\n\n")
}

// Performance monitoring and metrics

// PerformanceMonitor tracks performance metrics for RPC operations
type PerformanceMonitor struct {
	metrics   map[string]*MethodMetrics
	metricsMu sync.RWMutex
	options   MonitoringOptions
}

// MonitoringOptions configures performance monitoring
type MonitoringOptions struct {
	// CollectHistograms enables histogram collection for latencies
	CollectHistograms bool

	// HistogramBuckets defines bucket boundaries for histograms
	HistogramBuckets []time.Duration

	// MaxMethods limits the number of methods to track
	MaxMethods int

	// MetricRetention defines how long to keep metrics
	MetricRetention time.Duration
}

// DefaultMonitoringOptions returns reasonable defaults
func DefaultMonitoringOptions() MonitoringOptions {
	return MonitoringOptions{
		CollectHistograms: true,
		HistogramBuckets: []time.Duration{
			1 * time.Millisecond,
			5 * time.Millisecond,
			10 * time.Millisecond,
			25 * time.Millisecond,
			50 * time.Millisecond,
			100 * time.Millisecond,
			250 * time.Millisecond,
			500 * time.Millisecond,
			1 * time.Second,
			2500 * time.Millisecond,
			5 * time.Second,
		},
		MaxMethods:      1000,
		MetricRetention: 24 * time.Hour,
	}
}

// MethodMetrics tracks metrics for a specific method
type MethodMetrics struct {
	MethodName    string
	CallCount     int64
	ErrorCount    int64
	SuccessCount  int64
	TotalDuration time.Duration
	MinDuration   time.Duration
	MaxDuration   time.Duration
	LastCall      time.Time
	FirstCall     time.Time
	Histogram     map[string]int64 // bucket -> count
	mu            sync.RWMutex
}

// NewPerformanceMonitor creates a new performance monitor
func NewPerformanceMonitor(options MonitoringOptions) *PerformanceMonitor {
	return &PerformanceMonitor{
		metrics: make(map[string]*MethodMetrics),
		options: options,
	}
}

// RecordCall records metrics for an RPC call
func (pm *PerformanceMonitor) RecordCall(method string, duration time.Duration, err error) {
	pm.metricsMu.Lock()
	defer pm.metricsMu.Unlock()

	// Get or create metrics for this method
	methodMetrics, exists := pm.metrics[method]
	if !exists {
		// Check method limit
		if len(pm.metrics) >= pm.options.MaxMethods {
			// Remove oldest method
			var oldestMethod string
			var oldestTime time.Time
			for name, m := range pm.metrics {
				if oldestMethod == "" || m.FirstCall.Before(oldestTime) {
					oldestMethod = name
					oldestTime = m.FirstCall
				}
			}
			if oldestMethod != "" {
				delete(pm.metrics, oldestMethod)
			}
		}

		methodMetrics = &MethodMetrics{
			MethodName:  method,
			FirstCall:   time.Now(),
			MinDuration: duration,
			MaxDuration: duration,
			Histogram:   make(map[string]int64),
		}
		pm.metrics[method] = methodMetrics
	}

	methodMetrics.mu.Lock()
	defer methodMetrics.mu.Unlock()

	// Update basic metrics
	methodMetrics.CallCount++
	methodMetrics.TotalDuration += duration
	methodMetrics.LastCall = time.Now()

	if duration < methodMetrics.MinDuration {
		methodMetrics.MinDuration = duration
	}
	if duration > methodMetrics.MaxDuration {
		methodMetrics.MaxDuration = duration
	}

	// Update error/success counts
	if err != nil {
		methodMetrics.ErrorCount++
	} else {
		methodMetrics.SuccessCount++
	}

	// Update histogram
	if pm.options.CollectHistograms {
		bucket := pm.findHistogramBucket(duration)
		methodMetrics.Histogram[bucket]++
	}
}

// GetMethodMetrics returns metrics for a specific method
func (pm *PerformanceMonitor) GetMethodMetrics(method string) *MethodMetrics {
	pm.metricsMu.RLock()
	defer pm.metricsMu.RUnlock()

	if metrics, exists := pm.metrics[method]; exists {
		// Return a copy to avoid race conditions
		metrics.mu.RLock()
		defer metrics.mu.RUnlock()

		copy := *metrics
		copy.Histogram = make(map[string]int64)
		for k, v := range metrics.Histogram {
			copy.Histogram[k] = v
		}
		return &copy
	}
	return nil
}

// GetAllMetrics returns metrics for all methods
func (pm *PerformanceMonitor) GetAllMetrics() map[string]*MethodMetrics {
	pm.metricsMu.RLock()
	defer pm.metricsMu.RUnlock()

	result := make(map[string]*MethodMetrics)
	for method := range pm.metrics {
		result[method] = pm.GetMethodMetrics(method)
	}
	return result
}

// GetSummary returns a summary of all metrics
func (pm *PerformanceMonitor) GetSummary() *MetricsSummary {
	allMetrics := pm.GetAllMetrics()

	summary := &MetricsSummary{
		MethodCount:   len(allMetrics),
		TotalCalls:    0,
		TotalErrors:   0,
		TotalDuration: 0,
		Methods:       make([]*MethodSummary, 0, len(allMetrics)),
	}

	for _, metrics := range allMetrics {
		summary.TotalCalls += metrics.CallCount
		summary.TotalErrors += metrics.ErrorCount
		summary.TotalDuration += metrics.TotalDuration

		methodSummary := &MethodSummary{
			MethodName:      metrics.MethodName,
			CallCount:       metrics.CallCount,
			ErrorRate:       float64(metrics.ErrorCount) / float64(metrics.CallCount),
			AverageDuration: metrics.TotalDuration / time.Duration(metrics.CallCount),
			MinDuration:     metrics.MinDuration,
			MaxDuration:     metrics.MaxDuration,
		}
		summary.Methods = append(summary.Methods, methodSummary)
	}

	// Calculate overall error rate
	if summary.TotalCalls > 0 {
		summary.OverallErrorRate = float64(summary.TotalErrors) / float64(summary.TotalCalls)
	}

	// Sort methods by call count
	sort.Slice(summary.Methods, func(i, j int) bool {
		return summary.Methods[i].CallCount > summary.Methods[j].CallCount
	})

	return summary
}

// MetricsSummary provides a high-level overview of performance metrics
type MetricsSummary struct {
	MethodCount      int
	TotalCalls       int64
	TotalErrors      int64
	TotalDuration    time.Duration
	OverallErrorRate float64
	Methods          []*MethodSummary
}

// MethodSummary provides a summary for a single method
type MethodSummary struct {
	MethodName      string
	CallCount       int64
	ErrorRate       float64
	AverageDuration time.Duration
	MinDuration     time.Duration
	MaxDuration     time.Duration
}

// findHistogramBucket finds the appropriate histogram bucket for a duration
func (pm *PerformanceMonitor) findHistogramBucket(duration time.Duration) string {
	for _, bucket := range pm.options.HistogramBuckets {
		if duration <= bucket {
			return bucket.String()
		}
	}
	return "+Inf"
}

// Connection state inspection

// ConnectionInspector provides utilities for inspecting RPC connection state
type ConnectionInspector struct {
	sessions map[string]*SessionInfo
	mu       sync.RWMutex
}

// SessionInfo contains information about an RPC session
type SessionInfo struct {
	ID            string
	RemoteAddress string
	StartTime     time.Time
	LastActivity  time.Time
	MessagesSent  int64
	MessagesRecv  int64
	BytesSent     int64
	BytesRecv     int64
	Exports       map[ExportID]string
	Imports       map[ImportID]string
	State         SessionState
}

// SessionState represents the state of an RPC session
type SessionState string

const (
	SessionStateConnecting SessionState = "connecting"
	SessionStateActive     SessionState = "active"
	SessionStateClosing    SessionState = "closing"
	SessionStateClosed     SessionState = "closed"
	SessionStateError      SessionState = "error"
)

// NewConnectionInspector creates a new connection inspector
func NewConnectionInspector() *ConnectionInspector {
	return &ConnectionInspector{
		sessions: make(map[string]*SessionInfo),
	}
}

// RegisterSession registers a session for inspection
func (ci *ConnectionInspector) RegisterSession(session *Session) {
	ci.mu.Lock()
	defer ci.mu.Unlock()

	info := &SessionInfo{
		ID:           generateSessionID(),
		StartTime:    time.Now(),
		LastActivity: time.Now(),
		Exports:      make(map[ExportID]string),
		Imports:      make(map[ImportID]string),
		State:        SessionStateActive,
	}

	ci.sessions[info.ID] = info
}

// UpdateSessionActivity updates the last activity time for a session
func (ci *ConnectionInspector) UpdateSessionActivity(sessionID string) {
	ci.mu.Lock()
	defer ci.mu.Unlock()

	if info, exists := ci.sessions[sessionID]; exists {
		info.LastActivity = time.Now()
	}
}

// GetSessionInfo returns information about a specific session
func (ci *ConnectionInspector) GetSessionInfo(sessionID string) *SessionInfo {
	ci.mu.RLock()
	defer ci.mu.RUnlock()

	if info, exists := ci.sessions[sessionID]; exists {
		// Return a copy
		copy := *info
		copy.Exports = make(map[ExportID]string)
		copy.Imports = make(map[ImportID]string)
		for k, v := range info.Exports {
			copy.Exports[k] = v
		}
		for k, v := range info.Imports {
			copy.Imports[k] = v
		}
		return &copy
	}
	return nil
}

// GetAllSessions returns information about all sessions
func (ci *ConnectionInspector) GetAllSessions() []*SessionInfo {
	ci.mu.RLock()
	defer ci.mu.RUnlock()

	sessions := make([]*SessionInfo, 0, len(ci.sessions))
	for _, info := range ci.sessions {
		sessions = append(sessions, ci.GetSessionInfo(info.ID))
	}

	// Sort by start time
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartTime.Before(sessions[j].StartTime)
	})

	return sessions
}

// Helper functions for ID generation

func generateTraceID() string {
	return fmt.Sprintf("trace_%d", time.Now().UnixNano())
}

func generateSpanID() string {
	return fmt.Sprintf("span_%d", time.Now().UnixNano())
}

func generateSessionID() string {
	return fmt.Sprintf("session_%d", time.Now().UnixNano())
}

// TracingStub wraps a stub with tracing capabilities
type TracingStub struct {
	underlying Stub
	tracer     *Tracer
	traceID    string
}

// NewTracingStub creates a new tracing wrapper for a stub
func NewTracingStub(stub Stub, tracer *Tracer, traceID string) *TracingStub {
	return &TracingStub{
		underlying: stub,
		tracer:     tracer,
		traceID:    traceID,
	}
}

// Call implements Stub.Call with tracing
func (ts *TracingStub) Call(ctx context.Context, method string, args ...interface{}) (*Promise, error) {
	trace := ts.tracer.GetTrace(ts.traceID)
	span := trace.StartSpan(OperationTypeCall, method)
	defer trace.FinishSpan(span)

	span.SetRequest(args)
	span.AddTag("method", method)
	span.AddTag("arg_count", len(args))

	promise, err := ts.underlying.Call(ctx, method, args...)
	if err != nil {
		span.SetError(err)
		return nil, err
	}

	span.LogEvent(TraceLevelDebug, "Call successful", map[string]interface{}{
		"method": method,
	})

	return promise, nil
}

// Get implements Stub.Get with tracing
func (ts *TracingStub) Get(ctx context.Context, property string) (*Promise, error) {
	trace := ts.tracer.GetTrace(ts.traceID)
	span := trace.StartSpan(OperationTypeGet, property)
	defer trace.FinishSpan(span)

	span.AddTag("property", property)

	promise, err := ts.underlying.Get(ctx, property)
	if err != nil {
		span.SetError(err)
		return nil, err
	}

	span.LogEvent(TraceLevelDebug, "Get successful", map[string]interface{}{
		"property": property,
	})

	return promise, nil
}

// Dispose implements Stub.Dispose with tracing
func (ts *TracingStub) Dispose() error {
	trace := ts.tracer.GetTrace(ts.traceID)
	span := trace.StartSpan(OperationTypeRelease, "dispose")
	defer trace.FinishSpan(span)

	err := ts.underlying.Dispose()
	if err != nil {
		span.SetError(err)
	}

	return err
}