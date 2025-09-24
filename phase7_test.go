package capnweb

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestCodeGenerationPhase7 tests the code generation functionality
func TestCodeGenerationPhase7(t *testing.T) {
	// Create registry and register interface
	registry := NewInterfaceRegistry()

	// Mock interface for testing
	type TestAPI interface {
		GetData(ctx context.Context, id int) (string, error)
		SetData(ctx context.Context, id int, value string) error
	}

	_, err := registry.RegisterInterface((*TestAPI)(nil))
	if err != nil {
		t.Fatalf("Failed to register interface: %v", err)
	}

	// Test code generation
	options := DefaultCodeGenOptions()
	options.PackageName = "test"
	options.GenerateComments = true
	options.GenerateValidation = true

	generator := NewCodeGenerator(registry, options)

	var output bytes.Buffer
	err = generator.GenerateCode(&output)
	if err != nil {
		t.Fatalf("Failed to generate code: %v", err)
	}

	generatedCode := output.String()
	if len(generatedCode) == 0 {
		t.Error("Generated code is empty")
	}

	// Check for expected elements in generated code
	expectedElements := []string{
		"package test",
		"import",
		"context",
		"capnweb",
		"Stub",
	}

	for _, element := range expectedElements {
		if !strings.Contains(generatedCode, element) {
			t.Errorf("Generated code missing expected element: %s", element)
		}
	}

	t.Logf("Generated code length: %d characters", len(generatedCode))
}

// TestMockTransport tests the mock transport functionality
func TestMockTransport(t *testing.T) {
	transport := NewMockTransport()

	// Test basic send/receive
	ctx := context.Background()
	testMessage := []byte("test message")

	// Inject a message for receiving
	transport.InjectMessage(testMessage)

	// Test send
	err := transport.Send(ctx, testMessage)
	if err != nil {
		t.Errorf("Send failed: %v", err)
	}

	// Test receive
	received, err := transport.Receive(ctx)
	if err != nil {
		t.Errorf("Receive failed: %v", err)
	}

	if string(received) != string(testMessage) {
		t.Errorf("Received message mismatch: expected %s, got %s", testMessage, received)
	}

	// Test message tracking
	sentMessages := transport.GetSentMessages()
	if len(sentMessages) != 1 {
		t.Errorf("Expected 1 sent message, got %d", len(sentMessages))
	}

	// Test latency simulation
	transport.SetLatency(10 * time.Millisecond)
	start := time.Now()
	transport.Send(ctx, testMessage)
	duration := time.Since(start)

	if duration < 10*time.Millisecond {
		t.Errorf("Latency simulation not working: expected at least 10ms, got %v", duration)
	}

	// Test stats
	stats := transport.Stats()
	if stats.MessagesSent < 2 {
		t.Errorf("Expected at least 2 messages sent, got %d", stats.MessagesSent)
	}

	// Test close
	err = transport.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Sending after close should fail
	err = transport.Send(ctx, testMessage)
	if err == nil {
		t.Error("Send should fail after close")
	}
}

// TestMockStub tests the mock stub functionality
func TestMockStub(t *testing.T) {
	stub := NewMockStub("TestService")

	// Set up mock responses
	stub.SetResponse("GetUser", map[string]interface{}{
		"id":   123,
		"name": "Test User",
	})

	stub.SetError("DeleteUser", fmt.Errorf("permission denied"))

	ctx := context.Background()

	// Test successful call
	promise, err := stub.Call(ctx, "GetUser", 123)
	if err != nil {
		t.Errorf("Mock call failed: %v", err)
	}

	result, err := promise.Await(ctx)
	if err != nil {
		t.Errorf("Promise await failed: %v", err)
	}

	userMap, ok := result.(map[string]interface{})
	if !ok {
		t.Error("Result is not a map")
	} else {
		if userMap["id"] != 123 {
			t.Errorf("Expected user ID 123, got %v", userMap["id"])
		}
	}

	// Test error call
	_, err = stub.Call(ctx, "DeleteUser", 123)
	if err == nil {
		t.Error("Expected error for DeleteUser call")
	}

	// Test call tracking
	calls := stub.GetCalls()
	if len(calls) != 2 {
		t.Errorf("Expected 2 calls, got %d", len(calls))
	}

	callCount := stub.GetCallCount("GetUser")
	if callCount != 1 {
		t.Errorf("Expected 1 GetUser call, got %d", callCount)
	}

	lastCall := stub.GetLastCall("GetUser")
	if lastCall == nil {
		t.Error("Expected to find last GetUser call")
	} else if lastCall.Method != "GetUser" {
		t.Errorf("Expected method GetUser, got %s", lastCall.Method)
	}

	// Test property access
	stub.SetResponse("GET:status", "active")
	promise, err = stub.Get(ctx, "status")
	if err != nil {
		t.Errorf("Property get failed: %v", err)
	}

	status, err := promise.Await(ctx)
	if err != nil {
		t.Errorf("Property await failed: %v", err)
	}

	if status != "active" {
		t.Errorf("Expected status 'active', got %v", status)
	}

	// Test dispose
	err = stub.Dispose()
	if err != nil {
		t.Errorf("Dispose failed: %v", err)
	}

	if !stub.IsDisposed() {
		t.Error("Stub should be disposed")
	}

	// Operations after dispose should fail
	_, err = stub.Call(ctx, "GetUser", 456)
	if err == nil {
		t.Error("Call should fail after dispose")
	}
}

// TestStubVerifier tests the stub verification functionality
func TestStubVerifier(t *testing.T) {
	mockT := &mockTestingContext{}
	stub := NewMockStub("VerificationTest")
	verifier := NewStubVerifier(stub, mockT)

	ctx := context.Background()

	// Make some calls
	stub.SetResponse("Method1", "result1")
	stub.SetResponse("Method2", "result2")

	stub.Call(ctx, "Method1", "arg1")
	stub.Call(ctx, "Method1", "arg2")
	stub.Call(ctx, "Method2", "arg3")

	// Test call count verification
	verifier.VerifyCallCount("Method1", 2)
	if mockT.errorCount > 0 {
		t.Error("VerifyCallCount should not have failed")
	}

	verifier.VerifyCallCount("Method2", 5) // This should fail
	if mockT.errorCount != 1 {
		t.Error("VerifyCallCount should have failed")
	}

	// Reset mock testing context
	mockT.errorCount = 0

	// Test called with verification
	verifier.VerifyCalledWith("Method1", "arg2")
	if mockT.errorCount > 0 {
		t.Error("VerifyCalledWith should not have failed")
	}

	verifier.VerifyCalledWith("Method1", "wrong-arg")
	if mockT.errorCount != 1 {
		t.Error("VerifyCalledWith should have failed")
	}

	// Test never called
	mockT.errorCount = 0
	verifier.VerifyNeverCalled("Method3")
	if mockT.errorCount > 0 {
		t.Error("VerifyNeverCalled should not have failed")
	}

	verifier.VerifyNeverCalled("Method1")
	if mockT.errorCount != 1 {
		t.Error("VerifyNeverCalled should have failed")
	}

	// Test dispose verification
	mockT.errorCount = 0
	verifier.VerifyDisposed()
	if mockT.errorCount != 1 {
		t.Error("VerifyDisposed should have failed (not disposed)")
	}

	stub.Dispose()
	mockT.errorCount = 0
	verifier.VerifyDisposed()
	if mockT.errorCount > 0 {
		t.Error("VerifyDisposed should not have failed (now disposed)")
	}
}

// TestTracer tests the tracing functionality
func TestTracer(t *testing.T) {
	var output bytes.Buffer
	options := DefaultTracingOptions()
	tracer := NewTracer(&output, options)

	// Test trace creation
	trace := tracer.StartTrace("test-trace")
	if trace == nil {
		t.Fatal("Failed to create trace")
	}

	if trace.ID == "" {
		t.Error("Trace ID should not be empty")
	}

	if trace.Name != "test-trace" {
		t.Errorf("Expected trace name 'test-trace', got %s", trace.Name)
	}

	// Test span creation
	span := trace.StartSpan(OperationTypeCall, "TestMethod")
	if span == nil {
		t.Fatal("Failed to create span")
	}

	// Add tags and logs
	span.AddTag("user_id", 123)
	span.AddTag("operation", "test")
	span.LogEvent(TraceLevelInfo, "Starting operation", map[string]interface{}{
		"timestamp": time.Now().Unix(),
	})

	// Set request/response
	span.SetRequest(map[string]interface{}{"param": "value"})
	span.SetResponse(map[string]interface{}{"result": "success"})

	// Finish span
	trace.FinishSpan(span)

	if span.Duration == 0 {
		t.Error("Span duration should be greater than 0")
	}

	// Add another span with error
	errorSpan := trace.StartSpan(OperationTypeCall, "ErrorMethod")
	errorSpan.SetError(fmt.Errorf("test error"))
	trace.FinishSpan(errorSpan)

	// End trace
	tracer.EndTrace(trace.ID)

	// Verify trace was recorded
	retrievedTrace := tracer.GetTrace(trace.ID)
	if retrievedTrace == nil {
		t.Error("Trace should be retrievable after creation")
	}

	if len(retrievedTrace.Spans) != 2 {
		t.Errorf("Expected 2 spans, got %d", len(retrievedTrace.Spans))
	}

	// Check first span details
	firstSpan := retrievedTrace.Spans[0]
	if firstSpan.Method != "TestMethod" {
		t.Errorf("Expected method 'TestMethod', got %s", firstSpan.Method)
	}

	if len(firstSpan.Tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(firstSpan.Tags))
	}

	if len(firstSpan.Logs) != 1 {
		t.Errorf("Expected 1 log entry, got %d", len(firstSpan.Logs))
	}

	// Check error span
	errorSpanRetrieved := retrievedTrace.Spans[1]
	if errorSpanRetrieved.Error == nil {
		t.Error("Error span should have an error")
	}

	// Test trace retrieval
	allTraces := tracer.GetAllTraces()
	if len(allTraces) == 0 {
		t.Error("Should have at least one trace")
	}

	// Test trace clearing
	tracer.ClearTraces()
	allTracesAfterClear := tracer.GetAllTraces()
	if len(allTracesAfterClear) != 0 {
		t.Error("All traces should be cleared")
	}

	// Verify output was written
	if output.Len() == 0 {
		t.Error("Expected output to be written to buffer")
	}
}

// TestPerformanceMonitor tests the performance monitoring functionality
func TestPerformanceMonitor(t *testing.T) {
	options := DefaultMonitoringOptions()
	monitor := NewPerformanceMonitor(options)

	// Record some method calls
	monitor.RecordCall("Method1", 10*time.Millisecond, nil)
	monitor.RecordCall("Method1", 20*time.Millisecond, nil)
	monitor.RecordCall("Method1", 15*time.Millisecond, fmt.Errorf("test error"))
	monitor.RecordCall("Method2", 50*time.Millisecond, nil)

	// Test method metrics
	method1Metrics := monitor.GetMethodMetrics("Method1")
	if method1Metrics == nil {
		t.Fatal("Method1 metrics should exist")
	}

	if method1Metrics.CallCount != 3 {
		t.Errorf("Expected 3 calls for Method1, got %d", method1Metrics.CallCount)
	}

	if method1Metrics.ErrorCount != 1 {
		t.Errorf("Expected 1 error for Method1, got %d", method1Metrics.ErrorCount)
	}

	if method1Metrics.SuccessCount != 2 {
		t.Errorf("Expected 2 successes for Method1, got %d", method1Metrics.SuccessCount)
	}

	if method1Metrics.MinDuration != 10*time.Millisecond {
		t.Errorf("Expected min duration 10ms, got %v", method1Metrics.MinDuration)
	}

	if method1Metrics.MaxDuration != 20*time.Millisecond {
		t.Errorf("Expected max duration 20ms, got %v", method1Metrics.MaxDuration)
	}

	expectedTotal := 45 * time.Millisecond
	if method1Metrics.TotalDuration != expectedTotal {
		t.Errorf("Expected total duration %v, got %v", expectedTotal, method1Metrics.TotalDuration)
	}

	// Test all metrics
	allMetrics := monitor.GetAllMetrics()
	if len(allMetrics) != 2 {
		t.Errorf("Expected 2 methods in metrics, got %d", len(allMetrics))
	}

	// Test summary
	summary := monitor.GetSummary()
	if summary.MethodCount != 2 {
		t.Errorf("Expected 2 methods in summary, got %d", summary.MethodCount)
	}

	if summary.TotalCalls != 4 {
		t.Errorf("Expected 4 total calls, got %d", summary.TotalCalls)
	}

	if summary.TotalErrors != 1 {
		t.Errorf("Expected 1 total error, got %d", summary.TotalErrors)
	}

	expectedErrorRate := 0.25 // 1 error out of 4 calls
	if summary.OverallErrorRate != expectedErrorRate {
		t.Errorf("Expected error rate %f, got %f", expectedErrorRate, summary.OverallErrorRate)
	}

	// Verify methods are sorted by call count
	if len(summary.Methods) >= 2 {
		if summary.Methods[0].CallCount < summary.Methods[1].CallCount {
			t.Error("Methods should be sorted by call count in descending order")
		}
	}
}

// TestConnectionInspector tests the connection inspection functionality
func TestConnectionInspector(t *testing.T) {
	inspector := NewConnectionInspector()

	// Create mock sessions
	session1 := &Session{} // Simplified for testing
	session2 := &Session{}

	// Register sessions
	inspector.RegisterSession(session1)
	inspector.RegisterSession(session2)

	// Get all sessions
	sessions := inspector.GetAllSessions()
	if len(sessions) != 2 {
		t.Errorf("Expected 2 sessions, got %d", len(sessions))
	}

	// Test session info
	firstSession := sessions[0]
	if firstSession.ID == "" {
		t.Error("Session ID should not be empty")
	}

	if firstSession.State != SessionStateActive {
		t.Errorf("Expected session state %s, got %s", SessionStateActive, firstSession.State)
	}

	// Test activity update
	sessionID := firstSession.ID
	originalActivity := firstSession.LastActivity
	time.Sleep(1 * time.Millisecond) // Ensure time difference

	inspector.UpdateSessionActivity(sessionID)

	updatedSession := inspector.GetSessionInfo(sessionID)
	if updatedSession.LastActivity.Equal(originalActivity) {
		t.Error("Last activity time should have been updated")
	}

	// Test non-existent session
	nonExistentInfo := inspector.GetSessionInfo("non-existent")
	if nonExistentInfo != nil {
		t.Error("Non-existent session should return nil")
	}
}

// TestTracingStub tests the tracing stub wrapper
func TestTracingStub(t *testing.T) {
	var output bytes.Buffer
	options := DefaultTracingOptions()
	tracer := NewTracer(&output, options)

	// Create mock underlying stub
	mockStub := NewMockStub("TracedService")
	mockStub.SetResponse("TestMethod", "test-result")

	// Start trace
	trace := tracer.StartTrace("tracing-stub-test")
	tracingStub := NewTracingStub(mockStub, tracer, trace.ID)

	ctx := context.Background()

	// Make a traced call
	promise, err := tracingStub.Call(ctx, "TestMethod", "arg1", "arg2")
	if err != nil {
		t.Errorf("Traced call failed: %v", err)
	}

	result, err := promise.Await(ctx)
	if err != nil {
		t.Errorf("Traced promise await failed: %v", err)
	}

	if result != "test-result" {
		t.Errorf("Expected result 'test-result', got %v", result)
	}

	// Make a traced get
	mockStub.SetResponse("GET:property", "property-value")
	promise, err = tracingStub.Get(ctx, "property")
	if err != nil {
		t.Errorf("Traced get failed: %v", err)
	}

	propResult, err := promise.Await(ctx)
	if err != nil {
		t.Errorf("Traced get await failed: %v", err)
	}

	if propResult != "property-value" {
		t.Errorf("Expected property value 'property-value', got %v", propResult)
	}

	// End trace
	tracer.EndTrace(trace.ID)

	// Verify spans were created
	retrievedTrace := tracer.GetTrace(trace.ID)
	if retrievedTrace == nil {
		t.Fatal("Trace should exist")
	}

	if len(retrievedTrace.Spans) != 2 {
		t.Errorf("Expected 2 spans, got %d", len(retrievedTrace.Spans))
	}

	// Check call span
	callSpan := retrievedTrace.Spans[0]
	if callSpan.OperationType != OperationTypeCall {
		t.Errorf("Expected operation type %s, got %s", OperationTypeCall, callSpan.OperationType)
	}

	if callSpan.Method != "TestMethod" {
		t.Errorf("Expected method 'TestMethod', got %s", callSpan.Method)
	}

	// Check get span
	getSpan := retrievedTrace.Spans[1]
	if getSpan.OperationType != OperationTypeGet {
		t.Errorf("Expected operation type %s, got %s", OperationTypeGet, getSpan.OperationType)
	}

	// Dispose traced stub
	err = tracingStub.Dispose()
	if err != nil {
		t.Errorf("Traced dispose failed: %v", err)
	}

	// Verify underlying stub was disposed
	if !mockStub.IsDisposed() {
		t.Error("Underlying stub should be disposed")
	}
}

// TestBenchmarkStub tests the benchmark stub wrapper
func TestBenchmarkStub(t *testing.T) {
	mockStub := NewMockStub("BenchmarkService")
	mockStub.SetResponse("FastMethod", "fast-result")
	mockStub.SetResponse("SlowMethod", "slow-result")

	benchmarkStub := NewBenchmarkStub(mockStub)

	ctx := context.Background()

	// Make some calls
	for i := 0; i < 5; i++ {
		benchmarkStub.Call(ctx, "FastMethod", i)
	}

	for i := 0; i < 3; i++ {
		benchmarkStub.Call(ctx, "SlowMethod", i)
	}

	// Get statistics
	stats := benchmarkStub.GetStatistics()

	if stats.CallCount != 8 {
		t.Errorf("Expected 8 calls, got %d", stats.CallCount)
	}

	if stats.TotalTime == 0 {
		t.Error("Total time should be greater than 0")
	}

	if stats.AverageTime == 0 {
		t.Error("Average time should be greater than 0")
	}

	if stats.MinTime == 0 {
		t.Error("Min time should be greater than 0")
	}

	if stats.MaxTime == 0 {
		t.Error("Max time should be greater than 0")
	}

	if stats.MinTime > stats.MaxTime {
		t.Error("Min time should not be greater than max time")
	}

	// Test dispose
	err := benchmarkStub.Dispose()
	if err != nil {
		t.Errorf("Benchmark dispose failed: %v", err)
	}
}

// TestTestingFramework tests the overall testing framework
func TestTestingFramework(t *testing.T) {
	framework := NewTestingFramework()

	// Test recording
	recording := framework.StartRecording("test-recording")
	if recording == nil {
		t.Fatal("Failed to start recording")
	}

	// Add some recorded calls
	recording.RecordCall(&RecordedCall{
		Timestamp: time.Now(),
		Method:    "Method1",
		Args:      []interface{}{"arg1"},
		Result:    "result1",
		Duration:  10 * time.Millisecond,
	})

	recording.RecordCall(&RecordedCall{
		Timestamp: time.Now(),
		Method:    "Method2",
		Args:      []interface{}{"arg2"},
		Result:    "result2",
		Duration:  20 * time.Millisecond,
	})

	// Stop recording
	finalRecording := framework.StopRecording("test-recording")
	if finalRecording == nil {
		t.Fatal("Failed to stop recording")
	}

	if finalRecording.GetCallCount() != 2 {
		t.Errorf("Expected 2 recorded calls, got %d", finalRecording.GetCallCount())
	}

	totalDuration := finalRecording.GetTotalDuration()
	expectedDuration := 30 * time.Millisecond
	if totalDuration != expectedDuration {
		t.Errorf("Expected total duration %v, got %v", expectedDuration, totalDuration)
	}

	// Test method filtering
	method1Calls := finalRecording.GetCallsByMethod("Method1")
	if len(method1Calls) != 1 {
		t.Errorf("Expected 1 Method1 call, got %d", len(method1Calls))
	}

	// Test mock stub creation
	mockStub := framework.CreateMockStub("framework-test")
	if mockStub == nil {
		t.Fatal("Failed to create mock stub")
	}

	// Test retrieval
	retrievedStub := framework.GetMockStub("framework-test")
	if retrievedStub != mockStub {
		t.Error("Retrieved stub should be the same as created stub")
	}

	// Test test session creation
	session, transport := framework.CreateTestSession()
	if session == nil || transport == nil {
		t.Error("Failed to create test session and transport")
	}
}

// TestConcurrentTracing tests tracing under concurrent load
func TestConcurrentTracing(t *testing.T) {
	var output bytes.Buffer
	options := DefaultTracingOptions()
	tracer := NewTracer(&output, options)

	numWorkers := 10
	operationsPerWorker := 50

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < operationsPerWorker; j++ {
				trace := tracer.StartTrace(fmt.Sprintf("worker-%d-op-%d", workerID, j))
				if trace != nil {
					span := trace.StartSpan(OperationTypeCall, "ConcurrentMethod")
					span.AddTag("worker_id", workerID)
					span.AddTag("operation_id", j)
					trace.FinishSpan(span)
					tracer.EndTrace(trace.ID)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify traces were created without races
	allTraces := tracer.GetAllTraces()
	expectedTraces := numWorkers * operationsPerWorker

	if len(allTraces) != expectedTraces {
		t.Errorf("Expected %d traces, got %d", expectedTraces, len(allTraces))
	}
}

// Helper type for testing
type mockTestingContext struct {
	errorCount int
	errors     []string
}

func (m *mockTestingContext) Errorf(format string, args ...interface{}) {
	m.errorCount++
	m.errors = append(m.errors, fmt.Sprintf(format, args...))
}

func (m *mockTestingContext) Fatalf(format string, args ...interface{}) {
	m.errorCount++
	m.errors = append(m.errors, fmt.Sprintf("FATAL: "+format, args...))
}

func (m *mockTestingContext) Helper() {
	// No-op for testing
}