package capnweb

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestPromiseCreation(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	exportID := ExportID(42)
	promise := NewPromise(session, exportID)

	if promise.session != session {
		t.Error("Promise session not set correctly")
	}

	if promise.exportID != exportID {
		t.Errorf("Expected export ID %d, got %d", exportID, promise.exportID)
	}

	if !promise.IsPending() {
		t.Error("New promise should be pending")
	}

	if promise.IsResolved() {
		t.Error("New promise should not be resolved")
	}

	if promise.IsRejected() {
		t.Error("New promise should not be rejected")
	}
}

func TestPromiseWithStub(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	importID := ImportID(42)
	stub := newStub(session, &importID, nil, PropertyPath{"user"})
	path := PropertyPath{"profile"}

	promise := NewPromiseWithStub(session, stub, path)

	if promise.session != session {
		t.Error("Promise session not set correctly")
	}

	if promise.stub != stub {
		t.Error("Promise stub not set correctly")
	}

	promisePath := promise.GetPath()
	if len(promisePath) != 1 || promisePath[0] != "profile" {
		t.Errorf("Expected path [profile], got %v", promisePath)
	}
}

func TestPromiseResolve(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	exportID := ExportID(42)
	promise := NewPromise(session, exportID)

	testValue := "resolved value"
	promise.resolve(testValue)

	if !promise.IsResolved() {
		t.Error("Promise should be resolved")
	}

	if promise.IsPending() {
		t.Error("Promise should not be pending")
	}

	if promise.IsRejected() {
		t.Error("Promise should not be rejected")
	}

	// Test immediate await
	ctx := context.Background()
	result, err := promise.Await(ctx)
	if err != nil {
		t.Errorf("Await should not error for resolved promise: %v", err)
	}

	if result != testValue {
		t.Errorf("Expected result %v, got %v", testValue, result)
	}
}

func TestPromiseReject(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	exportID := ExportID(42)
	promise := NewPromise(session, exportID)

	testError := fmt.Errorf("test error")
	promise.reject(testError)

	if !promise.IsRejected() {
		t.Error("Promise should be rejected")
	}

	if promise.IsPending() {
		t.Error("Promise should not be pending")
	}

	if promise.IsResolved() {
		t.Error("Promise should not be resolved")
	}

	// Test immediate await
	ctx := context.Background()
	result, err := promise.Await(ctx)
	if err == nil {
		t.Error("Await should error for rejected promise")
	}

	if result != nil {
		t.Errorf("Expected nil result for rejected promise, got %v", result)
	}

	if err.Error() != testError.Error() {
		t.Errorf("Expected error %v, got %v", testError, err)
	}
}

func TestPromiseAwaitTimeout(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	exportID := ExportID(42)
	promise := NewPromise(session, exportID)

	// Test timeout
	shortTimeout := 50 * time.Millisecond
	start := time.Now()
	result, err := promise.AwaitWithTimeout(shortTimeout)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Await should timeout and return error")
	}

	if result != nil {
		t.Errorf("Expected nil result on timeout, got %v", result)
	}

	if elapsed < shortTimeout {
		t.Errorf("Expected timeout after %v, but completed in %v", shortTimeout, elapsed)
	}
}

func TestPromiseAwaitWithResolve(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	exportID := ExportID(42)
	promise := NewPromise(session, exportID)

	testValue := "async resolved value"

	// Resolve the promise after a short delay
	go func() {
		time.Sleep(10 * time.Millisecond)
		promise.resolve(testValue)
	}()

	// Await should block until resolved
	ctx := context.Background()
	result, err := promise.Await(ctx)
	if err != nil {
		t.Errorf("Await should not error: %v", err)
	}

	if result != testValue {
		t.Errorf("Expected result %v, got %v", testValue, result)
	}
}

func TestPromiseThen(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	importID := ImportID(42)
	stub := newStub(session, &importID, nil, PropertyPath{"user"})
	promise := NewPromiseWithStub(session, stub, PropertyPath{})

	// Test chaining with Then
	chainedPromise := promise.Then("getName", "arg1", 123)

	if chainedPromise == nil {
		t.Fatal("Chained promise should not be nil")
	}

	if chainedPromise.session != session {
		t.Error("Chained promise should have same session")
	}

	chainedPath := chainedPromise.GetPath()
	if len(chainedPath) != 1 || chainedPath[0] != "getName" {
		t.Errorf("Expected chained path [getName], got %v", chainedPath)
	}
}

func TestPromiseGet(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	importID := ImportID(42)
	stub := newStub(session, &importID, nil, PropertyPath{"user"})
	promise := NewPromiseWithStub(session, stub, PropertyPath{})

	// Test property access with Get
	propertyPromise := promise.Get("name")

	if propertyPromise == nil {
		t.Fatal("Property promise should not be nil")
	}

	if propertyPromise.session != session {
		t.Error("Property promise should have same session")
	}

	propertyPath := propertyPromise.GetPath()
	if len(propertyPath) != 1 || propertyPath[0] != "name" {
		t.Errorf("Expected property path [name], got %v", propertyPath)
	}
}

func TestPromiseTimeout(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	exportID := ExportID(42)
	promise := NewPromise(session, exportID)

	// Test timeout setting
	customTimeout := 5 * time.Second
	promise.SetTimeout(customTimeout)

	if promise.GetTimeout() != customTimeout {
		t.Errorf("Expected timeout %v, got %v", customTimeout, promise.GetTimeout())
	}
}

func TestPromiseString(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// Test promise with export ID
	exportID := ExportID(42)
	promise := NewPromise(session, exportID)
	promiseStr := promise.String()
	if promiseStr == "" {
		t.Error("Promise string representation should not be empty")
	}
	t.Logf("Promise with export: %s", promiseStr)

	// Test promise with path
	pathPromise := NewPromiseWithStub(session, nil, PropertyPath{"user", "profile"})
	pathStr := pathPromise.String()
	if pathStr == "" {
		t.Error("Promise string representation should not be empty")
	}
	t.Logf("Promise with path: %s", pathStr)

	// Test resolved promise
	promise.resolve("test value")
	resolvedStr := promise.String()
	t.Logf("Resolved promise: %s", resolvedStr)

	// Test rejected promise
	rejectPromise := NewPromise(session, ExportID(99))
	rejectPromise.reject(fmt.Errorf("test error"))
	rejectedStr := rejectPromise.String()
	t.Logf("Rejected promise: %s", rejectedStr)
}

func TestPromiseResolver(t *testing.T) {
	resolver := NewPromiseResolver()

	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	exportID := ExportID(42)
	promise := NewPromise(session, exportID)

	// Register promise
	resolver.RegisterPromise(exportID, promise)

	if resolver.GetPendingCount() != 1 {
		t.Errorf("Expected 1 pending promise, got %d", resolver.GetPendingCount())
	}

	// Resolve promise
	testValue := "resolved by resolver"
	resolved := resolver.ResolvePromise(exportID, testValue)
	if !resolved {
		t.Error("Promise should have been resolved")
	}

	if resolver.GetPendingCount() != 0 {
		t.Errorf("Expected 0 pending promises after resolve, got %d", resolver.GetPendingCount())
	}

	// Verify promise was resolved
	if !promise.IsResolved() {
		t.Error("Promise should be resolved")
	}

	ctx := context.Background()
	result, err := promise.Await(ctx)
	if err != nil {
		t.Errorf("Await should not error: %v", err)
	}
	if result != testValue {
		t.Errorf("Expected result %v, got %v", testValue, result)
	}
}

func TestPromiseResolverReject(t *testing.T) {
	resolver := NewPromiseResolver()

	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	exportID := ExportID(42)
	promise := NewPromise(session, exportID)

	// Register promise
	resolver.RegisterPromise(exportID, promise)

	// Reject promise
	testError := fmt.Errorf("resolver rejection")
	rejected := resolver.RejectPromise(exportID, testError)
	if !rejected {
		t.Error("Promise should have been rejected")
	}

	if resolver.GetPendingCount() != 0 {
		t.Errorf("Expected 0 pending promises after reject, got %d", resolver.GetPendingCount())
	}

	// Verify promise was rejected
	if !promise.IsRejected() {
		t.Error("Promise should be rejected")
	}
}

func TestPromiseResolverCleanup(t *testing.T) {
	resolver := NewPromiseResolver()

	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// Register multiple promises
	promises := make([]*Promise, 3)
	for i := 0; i < 3; i++ {
		exportID := ExportID(i + 1)
		promise := NewPromise(session, exportID)
		promises[i] = promise
		resolver.RegisterPromise(exportID, promise)
	}

	if resolver.GetPendingCount() != 3 {
		t.Errorf("Expected 3 pending promises, got %d", resolver.GetPendingCount())
	}

	// Cleanup all promises
	cleanupError := fmt.Errorf("cleanup error")
	resolver.CleanupPromises(cleanupError)

	if resolver.GetPendingCount() != 0 {
		t.Errorf("Expected 0 pending promises after cleanup, got %d", resolver.GetPendingCount())
	}

	// Verify all promises were rejected
	for i, promise := range promises {
		if !promise.IsRejected() {
			t.Errorf("Promise %d should be rejected after cleanup", i)
		}
	}
}

// Benchmark tests
func BenchmarkPromiseCreation(b *testing.B) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		b.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		promise := NewPromise(session, ExportID(i))
		_ = promise
	}
}

func BenchmarkPromiseResolve(b *testing.B) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		b.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	promises := make([]*Promise, b.N)
	for i := 0; i < b.N; i++ {
		promises[i] = NewPromise(session, ExportID(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		promises[i].resolve("test value")
	}
}

func BenchmarkPromiseAwaitResolved(b *testing.B) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		b.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	promise := NewPromise(session, ExportID(42))
	promise.resolve("test value")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := promise.Await(ctx)
		if err != nil {
			b.Fatalf("Await should not error: %v", err)
		}
	}
}