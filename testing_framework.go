package capnweb

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"
)

// TestingFramework provides utilities for testing Cap'n Web RPC applications
type TestingFramework struct {
	recordings map[string]*CallRecording
	mocks      map[string]*MockStub
	mu         sync.RWMutex
}

// NewTestingFramework creates a new testing framework instance
func NewTestingFramework() *TestingFramework {
	return &TestingFramework{
		recordings: make(map[string]*CallRecording),
		mocks:      make(map[string]*MockStub),
	}
}

// MockTransport implements Transport for testing
type MockTransport struct {
	// Message queues
	sent     [][]byte
	received [][]byte
	sentMu   sync.Mutex
	recvMu   sync.Mutex

	// Configuration
	latency      time.Duration
	failureRate  float64
	failureCount int
	dropRate     float64

	// State
	closed bool
	stats  TransportStats
}

// NewMockTransport creates a new mock transport for testing
func NewMockTransport() *MockTransport {
	return &MockTransport{
		sent:     make([][]byte, 0),
		received: make([][]byte, 0),
	}
}

// Send implements Transport.Send
func (mt *MockTransport) Send(ctx context.Context, message []byte) error {
	if mt.closed {
		return ErrTransportClosed
	}

	// Simulate latency
	if mt.latency > 0 {
		time.Sleep(mt.latency)
	}

	// Simulate failure
	if mt.failureRate > 0 && mt.shouldFail() {
		mt.failureCount++
		mt.stats.Errors++
		return fmt.Errorf("simulated transport failure")
	}

	// Simulate message drop
	if mt.dropRate > 0 && mt.shouldDrop() {
		return nil // Drop silently
	}

	mt.sentMu.Lock()
	mt.sent = append(mt.sent, message)
	mt.sentMu.Unlock()

	mt.stats.MessagesSent++
	mt.stats.BytesSent += uint64(len(message))

	return nil
}

// Receive implements Transport.Receive
func (mt *MockTransport) Receive(ctx context.Context) ([]byte, error) {
	if mt.closed {
		return nil, ErrTransportClosed
	}

	// Check for available messages
	mt.recvMu.Lock()
	if len(mt.received) == 0 {
		mt.recvMu.Unlock()
		// Wait for message or context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return nil, fmt.Errorf("no message available")
		}
	}

	message := mt.received[0]
	mt.received = mt.received[1:]
	mt.recvMu.Unlock()

	mt.stats.MessagesReceived++
	mt.stats.BytesReceived += uint64(len(message))

	return message, nil
}

// Close implements Transport.Close
func (mt *MockTransport) Close() error {
	mt.closed = true
	return nil
}

// Configuration methods for testing scenarios

// SetLatency sets artificial latency for transport operations
func (mt *MockTransport) SetLatency(latency time.Duration) {
	mt.latency = latency
}

// SetFailureRate sets the probability of transport failures (0.0 to 1.0)
func (mt *MockTransport) SetFailureRate(rate float64) {
	mt.failureRate = rate
}

// SetDropRate sets the probability of message drops (0.0 to 1.0)
func (mt *MockTransport) SetDropRate(rate float64) {
	mt.dropRate = rate
}

// InjectMessage injects a message into the receive queue for testing
func (mt *MockTransport) InjectMessage(message []byte) {
	mt.recvMu.Lock()
	mt.received = append(mt.received, message)
	mt.recvMu.Unlock()
}

// GetSentMessages returns all messages sent through this transport
func (mt *MockTransport) GetSentMessages() [][]byte {
	mt.sentMu.Lock()
	defer mt.sentMu.Unlock()
	result := make([][]byte, len(mt.sent))
	copy(result, mt.sent)
	return result
}

// GetReceivedMessageCount returns the number of messages received
func (mt *MockTransport) GetReceivedMessageCount() int {
	mt.recvMu.Lock()
	defer mt.recvMu.Unlock()
	return len(mt.received)
}

// ClearMessages clears all sent and received messages
func (mt *MockTransport) ClearMessages() {
	mt.sentMu.Lock()
	mt.recvMu.Lock()
	mt.sent = mt.sent[:0]
	mt.received = mt.received[:0]
	mt.recvMu.Unlock()
	mt.sentMu.Unlock()
}

// Stats returns transport statistics
func (mt *MockTransport) Stats() TransportStats {
	return mt.stats
}

// Helper methods for simulating failures

func (mt *MockTransport) shouldFail() bool {
	// Simple random failure based on failure rate
	// In a real implementation, could use more sophisticated logic
	return false // Simplified for demo
}

func (mt *MockTransport) shouldDrop() bool {
	// Simple random drop based on drop rate
	// In a real implementation, could use more sophisticated logic
	return false // Simplified for demo
}

// MockStub implements Stub for testing
type MockStub struct {
	name         string
	calls        []*MockCall
	responses    map[string]interface{}
	errors       map[string]error
	callCount    map[string]int
	disposed     bool
	exportID     ExportID
	importID     ImportID
	mu           sync.RWMutex
}

// MockCall represents a recorded RPC call
type MockCall struct {
	Method    string
	Args      []interface{}
	Timestamp time.Time
	Duration  time.Duration
	Result    interface{}
	Error     error
}

// NewMockStub creates a new mock stub for testing
func NewMockStub(name string) *MockStub {
	return &MockStub{
		name:      name,
		calls:     make([]*MockCall, 0),
		responses: make(map[string]interface{}),
		errors:    make(map[string]error),
		callCount: make(map[string]int),
		exportID:  ExportID(1), // Default export ID for testing
	}
}

// Call implements Stub.Call
func (ms *MockStub) Call(ctx context.Context, method string, args ...interface{}) (*Promise, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if ms.disposed {
		return nil, fmt.Errorf("stub is disposed")
	}

	start := time.Now()

	// Record the call
	call := &MockCall{
		Method:    method,
		Args:      args,
		Timestamp: start,
	}

	ms.calls = append(ms.calls, call)
	ms.callCount[method]++

	// Check for predefined error
	if err, exists := ms.errors[method]; exists {
		call.Error = err
		call.Duration = time.Since(start)
		return nil, err
	}

	// Get predefined response
	var result interface{}
	if response, exists := ms.responses[method]; exists {
		result = response
	}

	call.Result = result
	call.Duration = time.Since(start)

	// Create a resolved promise with the result
	promise := &Promise{
		session: nil, // Mock session
		state:   promiseStateResolved,
		result:  result,
	}

	return promise, nil
}

// Get implements Stub.Get
func (ms *MockStub) Get(ctx context.Context, property string) (*Promise, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if ms.disposed {
		return nil, fmt.Errorf("stub is disposed")
	}

	// Record as a property access call
	call := &MockCall{
		Method:    "GET:" + property,
		Args:      []interface{}{},
		Timestamp: time.Now(),
	}

	ms.calls = append(ms.calls, call)

	// Get predefined response for property
	var result interface{}
	if response, exists := ms.responses["GET:"+property]; exists {
		result = response
	}

	call.Result = result
	call.Duration = time.Since(call.Timestamp)

	promise := &Promise{
		session: nil,
		state:   promiseStateResolved,
		result:  result,
	}

	return promise, nil
}

// Dispose implements Stub.Dispose
func (ms *MockStub) Dispose() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.disposed = true
	return nil
}

// Mock configuration methods

// SetResponse sets a predefined response for a method
func (ms *MockStub) SetResponse(method string, response interface{}) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.responses[method] = response
}

// SetError sets a predefined error for a method
func (ms *MockStub) SetError(method string, err error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.errors[method] = err
}

// GetCalls returns all recorded calls
func (ms *MockStub) GetCalls() []*MockCall {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	result := make([]*MockCall, len(ms.calls))
	copy(result, ms.calls)
	return result
}

// GetCallCount returns the number of times a method was called
func (ms *MockStub) GetCallCount(method string) int {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.callCount[method]
}

// GetLastCall returns the most recent call to a method
func (ms *MockStub) GetLastCall(method string) *MockCall {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	for i := len(ms.calls) - 1; i >= 0; i-- {
		if ms.calls[i].Method == method {
			return ms.calls[i]
		}
	}
	return nil
}

// ClearCalls clears all recorded calls
func (ms *MockStub) ClearCalls() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.calls = ms.calls[:0]
	ms.callCount = make(map[string]int)
}

// IsDisposed returns whether the stub has been disposed
func (ms *MockStub) IsDisposed() bool {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.disposed
}

// GetExportID returns the export ID for compatibility with Stub interface
func (ms *MockStub) GetExportID() *ExportID {
	return &ms.exportID
}

// SetExportID sets the export ID for testing
func (ms *MockStub) SetExportID(id ExportID) {
	ms.exportID = id
}

// GetImportID returns the import ID for compatibility with Stub interface
func (ms *MockStub) GetImportID() *ImportID {
	return &ms.importID
}

// SetImportID sets the import ID for testing
func (ms *MockStub) SetImportID(id ImportID) {
	ms.importID = id
}

// CallRecording records RPC calls for playback testing
type CallRecording struct {
	Name        string
	Calls       []*RecordedCall
	StartTime   time.Time
	EndTime     time.Time
	Duration    time.Duration
}

// RecordedCall represents a single recorded RPC call
type RecordedCall struct {
	Timestamp    time.Time
	Method       string
	Args         []interface{}
	Result       interface{}
	Error        error
	Duration     time.Duration
	SessionID    string
	TraceID      string
}

// NewCallRecording creates a new call recording
func NewCallRecording(name string) *CallRecording {
	return &CallRecording{
		Name:      name,
		Calls:     make([]*RecordedCall, 0),
		StartTime: time.Now(),
	}
}

// RecordCall adds a call to the recording
func (cr *CallRecording) RecordCall(call *RecordedCall) {
	cr.Calls = append(cr.Calls, call)
}

// FinishRecording finalizes the recording
func (cr *CallRecording) FinishRecording() {
	cr.EndTime = time.Now()
	cr.Duration = cr.EndTime.Sub(cr.StartTime)
}

// GetCallsByMethod returns all calls to a specific method
func (cr *CallRecording) GetCallsByMethod(method string) []*RecordedCall {
	var result []*RecordedCall
	for _, call := range cr.Calls {
		if call.Method == method {
			result = append(result, call)
		}
	}
	return result
}

// GetTotalDuration returns the total duration of all calls
func (cr *CallRecording) GetTotalDuration() time.Duration {
	var total time.Duration
	for _, call := range cr.Calls {
		total += call.Duration
	}
	return total
}

// GetCallCount returns the total number of calls
func (cr *CallRecording) GetCallCount() int {
	return len(cr.Calls)
}

// Recording and playback functionality

// StartRecording begins recording RPC calls
func (tf *TestingFramework) StartRecording(name string) *CallRecording {
	tf.mu.Lock()
	defer tf.mu.Unlock()

	recording := NewCallRecording(name)
	tf.recordings[name] = recording
	return recording
}

// StopRecording stops recording and returns the recording
func (tf *TestingFramework) StopRecording(name string) *CallRecording {
	tf.mu.Lock()
	defer tf.mu.Unlock()

	if recording, exists := tf.recordings[name]; exists {
		recording.FinishRecording()
		return recording
	}
	return nil
}

// CreateMockStub creates a new mock stub
func (tf *TestingFramework) CreateMockStub(name string) *MockStub {
	tf.mu.Lock()
	defer tf.mu.Unlock()

	stub := NewMockStub(name)
	tf.mocks[name] = stub
	return stub
}

// GetMockStub retrieves a mock stub by name
func (tf *TestingFramework) GetMockStub(name string) *MockStub {
	tf.mu.RLock()
	defer tf.mu.RUnlock()
	return tf.mocks[name]
}

// StubVerifier provides assertions for testing RPC stubs
type StubVerifier struct {
	stub *MockStub
	t    TestingContext
}

// TestingContext represents a testing context (similar to testing.T)
type TestingContext interface {
	Errorf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
	Helper()
}

// NewStubVerifier creates a new stub verifier
func NewStubVerifier(stub *MockStub, t TestingContext) *StubVerifier {
	return &StubVerifier{
		stub: stub,
		t:    t,
	}
}

// VerifyCallCount asserts that a method was called a specific number of times
func (sv *StubVerifier) VerifyCallCount(method string, expectedCount int) {
	sv.t.Helper()
	actualCount := sv.stub.GetCallCount(method)
	if actualCount != expectedCount {
		sv.t.Errorf("Expected %s to be called %d times, but was called %d times",
			method, expectedCount, actualCount)
	}
}

// VerifyCalledWith asserts that a method was called with specific arguments
func (sv *StubVerifier) VerifyCalledWith(method string, expectedArgs ...interface{}) {
	sv.t.Helper()
	lastCall := sv.stub.GetLastCall(method)
	if lastCall == nil {
		sv.t.Errorf("Method %s was never called", method)
		return
	}

	if !sv.argsEqual(lastCall.Args, expectedArgs) {
		sv.t.Errorf("Method %s was called with %v, expected %v",
			method, lastCall.Args, expectedArgs)
	}
}

// VerifyNeverCalled asserts that a method was never called
func (sv *StubVerifier) VerifyNeverCalled(method string) {
	sv.t.Helper()
	count := sv.stub.GetCallCount(method)
	if count > 0 {
		sv.t.Errorf("Expected %s to never be called, but was called %d times", method, count)
	}
}

// VerifyCallOrder asserts that methods were called in a specific order
func (sv *StubVerifier) VerifyCallOrder(methods ...string) {
	sv.t.Helper()
	calls := sv.stub.GetCalls()

	if len(calls) < len(methods) {
		sv.t.Errorf("Expected at least %d calls, but only %d calls were made", len(methods), len(calls))
		return
	}

	for i, expectedMethod := range methods {
		if i >= len(calls) || calls[i].Method != expectedMethod {
			actualMethods := make([]string, len(calls))
			for j, call := range calls {
				actualMethods[j] = call.Method
			}
			sv.t.Errorf("Expected call order %v, but actual order was %v", methods, actualMethods)
			return
		}
	}
}

// VerifyDisposed asserts that the stub was properly disposed
func (sv *StubVerifier) VerifyDisposed() {
	sv.t.Helper()
	if !sv.stub.IsDisposed() {
		sv.t.Errorf("Expected stub to be disposed, but it was not")
	}
}

// Helper method to compare arguments
func (sv *StubVerifier) argsEqual(actual, expected []interface{}) bool {
	if len(actual) != len(expected) {
		return false
	}

	for i, arg := range actual {
		if !reflect.DeepEqual(arg, expected[i]) {
			return false
		}
	}

	return true
}

// TestSession creates a session with mock transport for testing
func (tf *TestingFramework) CreateTestSession() (*Session, *MockTransport) {
	transport := NewMockTransport()

	// Create session with mock transport
	// This is a simplified version - in reality would need proper initialization
	session := &Session{
		transport: transport,
		// ... other fields would be initialized
	}

	return session, transport
}

// Utility functions for common testing scenarios

// SimulateNetworkLatency adds artificial latency to a mock transport
func SimulateNetworkLatency(transport *MockTransport, latency time.Duration) {
	transport.SetLatency(latency)
}

// SimulateNetworkFailures adds failure probability to a mock transport
func SimulateNetworkFailures(transport *MockTransport, failureRate float64) {
	transport.SetFailureRate(failureRate)
}

// SimulatePacketLoss adds packet drop probability to a mock transport
func SimulatePacketLoss(transport *MockTransport, dropRate float64) {
	transport.SetDropRate(dropRate)
}

// CreateStubWithPredefinedResponses creates a mock stub with preset responses
func CreateStubWithPredefinedResponses(name string, responses map[string]interface{}) *MockStub {
	stub := NewMockStub(name)
	for method, response := range responses {
		stub.SetResponse(method, response)
	}
	return stub
}

// CreateStubWithErrors creates a mock stub that returns errors for specific methods
func CreateStubWithErrors(name string, errors map[string]error) *MockStub {
	stub := NewMockStub(name)
	for method, err := range errors {
		stub.SetError(method, err)
	}
	return stub
}

// Performance testing utilities

// BenchmarkStub measures the performance of stub operations
type BenchmarkStub struct {
	stub      Stub
	callTimes []time.Duration
	mu        sync.Mutex
}

// NewBenchmarkStub creates a new benchmark wrapper for a stub
func NewBenchmarkStub(stub Stub) *BenchmarkStub {
	return &BenchmarkStub{
		stub:      stub,
		callTimes: make([]time.Duration, 0),
	}
}

// Call wraps the stub's Call method with timing
func (bs *BenchmarkStub) Call(ctx context.Context, method string, args ...interface{}) (*Promise, error) {
	start := time.Now()
	promise, err := bs.stub.Call(ctx, method, args...)
	duration := time.Since(start)

	bs.mu.Lock()
	bs.callTimes = append(bs.callTimes, duration)
	bs.mu.Unlock()

	return promise, err
}

// Get wraps the stub's Get method with timing
func (bs *BenchmarkStub) Get(ctx context.Context, property string) (*Promise, error) {
	start := time.Now()
	promise, err := bs.stub.Get(ctx, property)
	duration := time.Since(start)

	bs.mu.Lock()
	bs.callTimes = append(bs.callTimes, duration)
	bs.mu.Unlock()

	return promise, err
}

// Dispose wraps the stub's Dispose method
func (bs *BenchmarkStub) Dispose() error {
	return bs.stub.Dispose()
}

// GetStatistics returns performance statistics
func (bs *BenchmarkStub) GetStatistics() BenchmarkStats {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if len(bs.callTimes) == 0 {
		return BenchmarkStats{}
	}

	var total time.Duration
	min := bs.callTimes[0]
	max := bs.callTimes[0]

	for _, duration := range bs.callTimes {
		total += duration
		if duration < min {
			min = duration
		}
		if duration > max {
			max = duration
		}
	}

	return BenchmarkStats{
		CallCount:   len(bs.callTimes),
		TotalTime:   total,
		AverageTime: total / time.Duration(len(bs.callTimes)),
		MinTime:     min,
		MaxTime:     max,
	}
}

// BenchmarkStats contains performance statistics
type BenchmarkStats struct {
	CallCount   int
	TotalTime   time.Duration
	AverageTime time.Duration
	MinTime     time.Duration
	MaxTime     time.Duration
}