package capnweb

import (
	"context"
	"testing"
	"time"
)

func TestNewSession(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	if session.transport != transport {
		t.Error("Session transport not set correctly")
	}

	if session.exports == nil {
		t.Error("Exports table not initialized")
	}

	if session.imports == nil {
		t.Error("Imports table not initialized")
	}

	if session.IsClosed() {
		t.Error("New session should not be closed")
	}
}

func TestSessionClose(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	if session.IsClosed() {
		t.Error("Session should not be closed initially")
	}

	err = session.Close()
	if err != nil {
		t.Errorf("Failed to close session: %v", err)
	}

	if !session.IsClosed() {
		t.Error("Session should be closed after Close()")
	}

	// Double close should not error
	err = session.Close()
	if err != nil {
		t.Errorf("Double close should not error: %v", err)
	}
}

func TestSessionExportValue(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// Test exporting a simple value
	testValue := "hello world"
	exportID, err := session.exportValue(testValue)
	if err != nil {
		t.Fatalf("Failed to export value: %v", err)
	}

	if exportID == 0 {
		t.Error("Export ID should not be zero")
	}

	// Check that the value is in the exports table
	session.mu.RLock()
	entry, exists := session.exports[exportID]
	session.mu.RUnlock()

	if !exists {
		t.Error("Exported value not found in exports table")
	}

	if entry.Value != testValue {
		t.Errorf("Expected exported value %v, got %v", testValue, entry.Value)
	}

	if entry.RefCount != 1 {
		t.Errorf("Expected ref count 1, got %d", entry.RefCount)
	}
}

func TestSessionCreateImportStub(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	importID := ImportID(42)
	stub, err := session.createImportStub(importID, false)
	if err != nil {
		t.Fatalf("Failed to create import stub: %v", err)
	}

	// Check that stub is the right type
	stubImpl, ok := stub.(*stubImpl)
	if !ok {
		t.Fatalf("Expected *stubImpl, got %T", stub)
	}

	if stubImpl.GetImportID() == nil || *stubImpl.GetImportID() != importID {
		t.Errorf("Expected import ID %d, got %v", importID, stubImpl.GetImportID())
	}

	// Check that the import is in the imports table
	session.mu.RLock()
	entry, exists := session.imports[importID]
	session.mu.RUnlock()

	if !exists {
		t.Error("Import not found in imports table")
	}

	if entry.ImportID != importID {
		t.Errorf("Expected import ID %d, got %d", importID, entry.ImportID)
	}

	if entry.RefCount != 1 {
		t.Errorf("Expected ref count 1, got %d", entry.RefCount)
	}
}

func TestSessionMessageHandling(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// Test pull message handling
	exportID := ExportID(123)
	testValue := "test response"

	// Add value to exports table
	session.mu.Lock()
	session.exports[exportID] = &exportEntry{
		Value:        testValue,
		RefCount:     1,
		Created:      time.Now(),
		LastAccessed: time.Now(),
	}
	session.mu.Unlock()

	// Create pull message
	pullMsg := session.protocol.NewPullMessage(ImportID(exportID))

	// Handle the message
	session.handlePull(pullMsg)

	// Check that a resolve message was sent
	// Note: In a real test, you'd want to verify the transport received the message
}

func TestSessionStatistics(t *testing.T) {
	opts := DefaultSessionOptions()
	opts.EnableStats = true

	transport := &MemoryTransport{}
	session, err := NewSession(transport, opts)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	stats := session.GetStats()
	if stats == nil {
		t.Fatal("Statistics should be enabled")
	}

	if stats.SessionStarted.IsZero() {
		t.Error("Session start time should be set")
	}

	// Test export statistics
	_, err = session.exportValue("test")
	if err != nil {
		t.Fatalf("Failed to export value: %v", err)
	}

	updatedStats := session.GetStats()
	if updatedStats.ExportsCreated != 1 {
		t.Errorf("Expected 1 export created, got %d", updatedStats.ExportsCreated)
	}

	if updatedStats.ExportsActive != 1 {
		t.Errorf("Expected 1 active export, got %d", updatedStats.ExportsActive)
	}
}

func TestSessionWithOptions(t *testing.T) {
	opts := SessionOptions{
		MessageQueueSize:      500,
		ResponseTimeout:       15 * time.Second,
		EnableStats:           true,
		MaxConcurrentRequests: 50,
		KeepAliveInterval:     20 * time.Second,
	}

	transport := &MemoryTransport{}
	session, err := NewSession(transport, opts)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	if session.options.MessageQueueSize != 500 {
		t.Errorf("Expected message queue size 500, got %d", session.options.MessageQueueSize)
	}

	if session.options.ResponseTimeout != 15*time.Second {
		t.Errorf("Expected timeout 15s, got %v", session.options.ResponseTimeout)
	}

	stats := session.GetStats()
	if stats == nil {
		t.Error("Statistics should be enabled")
	}
}

func TestSessionContextCancellation(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	ctx := session.Context()
	if ctx == nil {
		t.Fatal("Session context should not be nil")
	}

	// Close session
	session.Close()

	// Context should be cancelled
	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Session context should be cancelled when session is closed")
	}
}

func TestSessionSendMessage(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// Test sending a simple message
	msg := session.protocol.NewPushMessage("test", nil)
	ctx := context.Background()

	err = session.Send(ctx, msg)
	if err != nil {
		t.Errorf("Failed to send message: %v", err)
	}

	// Test sending after close
	session.Close()
	err = session.Send(ctx, msg)
	if err == nil {
		t.Error("Should not be able to send message after close")
	}
}

// Benchmark tests
func BenchmarkSessionExportValue(b *testing.B) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		b.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	testValue := "benchmark test"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := session.exportValue(testValue)
		if err != nil {
			b.Fatalf("Failed to export value: %v", err)
		}
	}
}

func BenchmarkSessionCreateImportStub(b *testing.B) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		b.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := session.createImportStub(ImportID(i), false)
		if err != nil {
			b.Fatalf("Failed to create import stub: %v", err)
		}
	}
}