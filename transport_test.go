package capnweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestInProcessTransport tests the in-process transport implementation
func TestInProcessTransport(t *testing.T) {
	transport1, transport2 := NewInProcessTransportPair()
	defer transport1.Close()
	defer transport2.Close()

	ctx := context.Background()
	message := []byte(`{"test": "message"}`)

	// Test sending from transport1 to transport2
	err := transport1.Send(ctx, message)
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	received, err := transport2.Receive(ctx)
	if err != nil {
		t.Fatalf("Failed to receive message: %v", err)
	}

	if string(received) != string(message) {
		t.Errorf("Message mismatch: expected %s, got %s", message, received)
	}
}

// TestInProcessTransportBidirectional tests bidirectional communication
func TestInProcessTransportBidirectional(t *testing.T) {
	transport1, transport2 := NewInProcessTransportPair()
	defer transport1.Close()
	defer transport2.Close()

	ctx := context.Background()

	// Send from transport1 to transport2
	msg1 := []byte(`{"from": "transport1"}`)
	err := transport1.Send(ctx, msg1)
	if err != nil {
		t.Fatalf("transport1.Send failed: %v", err)
	}

	received1, err := transport2.Receive(ctx)
	if err != nil {
		t.Fatalf("transport2.Receive failed: %v", err)
	}

	if string(received1) != string(msg1) {
		t.Errorf("Expected %q, got %q", msg1, received1)
	}

	// Send from transport2 to transport1
	msg2 := []byte(`{"from": "transport2"}`)
	err = transport2.Send(ctx, msg2)
	if err != nil {
		t.Fatalf("transport2.Send failed: %v", err)
	}

	received2, err := transport1.Receive(ctx)
	if err != nil {
		t.Fatalf("transport1.Receive failed: %v", err)
	}

	if string(received2) != string(msg2) {
		t.Errorf("Expected %q, got %q", msg2, received2)
	}

	// Check statistics
	stats1 := transport1.GetStats()
	if stats1.MessagesSent != 1 || stats1.MessagesReceived != 1 {
		t.Errorf("Transport1 stats incorrect: sent=%d, received=%d", stats1.MessagesSent, stats1.MessagesReceived)
	}

	stats2 := transport2.GetStats()
	if stats2.MessagesSent != 1 || stats2.MessagesReceived != 1 {
		t.Errorf("Transport2 stats incorrect: sent=%d, received=%d", stats2.MessagesSent, stats2.MessagesReceived)
	}
}

// TestHTTPBatchTransport tests the HTTP batch transport implementation
func TestHTTPBatchTransport(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back the request body as response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// Simple echo response
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		w.Write(body)
	}))
	defer server.Close()

	// Create HTTP batch transport
	transport, err := NewHTTPBatchTransport(server.URL)
	if err != nil {
		t.Fatalf("Failed to create HTTP transport: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	message := []byte(`{"method": "test", "id": 1}`)

	// Test sending message
	err = transport.Send(ctx, message)
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	// Wait a bit for batching and HTTP request
	time.Sleep(200 * time.Millisecond)

	// Test receiving response
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	received, err := transport.Receive(ctx)
	if err != nil {
		t.Fatalf("Failed to receive message: %v", err)
	}

	// Verify the response contains our message
	if len(received) == 0 {
		t.Error("Received empty response")
	}

	// Check stats
	stats := transport.GetStats()
	if stats.RequestsSent == 0 {
		t.Error("No requests were sent according to stats")
	}
}

// TestHTTPBatchServer tests the HTTP batch server implementation
func TestHTTPBatchServer(t *testing.T) {
	// Create a handler that echoes messages
	handler := func(batch [][]byte) [][]byte {
		responses := make([][]byte, len(batch))
		for i, message := range batch {
			// Echo back with "response" wrapper
			response := map[string]interface{}{
				"id":     i,
				"result": json.RawMessage(message),
			}
			responseBytes, _ := json.Marshal(response)
			responses[i] = responseBytes
		}
		return responses
	}

	server := NewHTTPServer(handler)
	testServer := httptest.NewServer(server)
	defer testServer.Close()

	// Test single message
	singleMessage := `{"method": "test", "params": {"value": 42}}`
	resp, err := http.Post(testServer.URL, "application/json",
		strings.NewReader(singleMessage))
	if err != nil {
		t.Fatalf("Failed to send POST request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Test batch message
	batchMessage := `[{"method": "test1"}, {"method": "test2"}]`
	resp2, err := http.Post(testServer.URL, "application/json",
		strings.NewReader(batchMessage))
	if err != nil {
		t.Fatalf("Failed to send batch POST request: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp2.StatusCode)
	}
}

// TestWebSocketTransport tests the WebSocket transport implementation
func TestWebSocketTransport(t *testing.T) {
	transport, err := NewWebSocketTransport("ws://localhost:8080/test")
	if err != nil {
		t.Fatalf("Failed to create WebSocket transport: %v", err)
	}
	defer transport.Close()

	// Test connection (will use mock connection)
	ctx := context.Background()
	err = transport.Connect(ctx)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	if !transport.IsConnected() {
		t.Error("Transport should be connected")
	}

	// Test sending (to mock connection)
	message := []byte(`{"test": "websocket"}`)
	err = transport.Send(ctx, message)
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	// Check stats
	stats := transport.GetStats()
	if stats.ConnectionAttempts != 1 {
		t.Errorf("Expected 1 connection attempt, got %d", stats.ConnectionAttempts)
	}
}

// TestTransportRegistry tests the transport registry
func TestTransportRegistry(t *testing.T) {
	registry := NewCustomTransportRegistry()

	// Register a factory
	factory := NewTCPTransportFactory("localhost:")
	registry.RegisterTransportFactory("tcp-test", factory)

	// Check registered types
	types := registry.GetRegisteredTypes()
	found := false
	for _, transportType := range types {
		if transportType == "tcp-test" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Transport type 'tcp-test' not found in registry")
	}

	// Test creating transport (will fail to connect, but should create)
	_, err := registry.CreateTransport("tcp-test", "9999")
	if err == nil {
		t.Error("Expected connection error, but got none")
	}

	// Test unknown transport type
	_, err = registry.CreateTransport("unknown", "endpoint")
	if err == nil {
		t.Error("Expected error for unknown transport type")
	}
}

// TestTransportConcurrency tests transport thread safety
func TestTransportConcurrency(t *testing.T) {
	transport1, transport2 := NewInProcessTransportPair()
	defer transport1.Close()
	defer transport2.Close()

	ctx := context.Background()
	numWorkers := 10
	messagesPerWorker := 100

	var wg sync.WaitGroup
	wg.Add(numWorkers * 2) // senders and receivers

	// Start senders
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < messagesPerWorker; j++ {
				message := []byte(`{"worker": ` + string(rune(workerID+'0')) + `, "message": ` + string(rune(j+'0')) + `}`)
				err := transport1.Send(ctx, message)
				if err != nil {
					t.Errorf("Worker %d failed to send message %d: %v", workerID, j, err)
					return
				}
			}
		}(i)
	}

	// Start receivers
	receivedCount := 0
	var receiveMux sync.Mutex
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < messagesPerWorker; j++ {
				_, err := transport2.Receive(ctx)
				if err != nil {
					t.Errorf("Worker %d failed to receive message %d: %v", workerID, j, err)
					return
				}
				receiveMux.Lock()
				receivedCount++
				receiveMux.Unlock()
			}
		}(i)
	}

	wg.Wait()

	expectedMessages := numWorkers * messagesPerWorker
	if receivedCount != expectedMessages {
		t.Errorf("Expected %d messages, received %d", expectedMessages, receivedCount)
	}
}

// BenchmarkInProcessTransport benchmarks the in-process transport
func BenchmarkInProcessTransport(b *testing.B) {
	transport1, transport2 := NewInProcessTransportPair()
	defer transport1.Close()
	defer transport2.Close()

	ctx := context.Background()
	message := []byte(`{"benchmark": "test", "data": "some test data for benchmarking"}`)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			err := transport1.Send(ctx, message)
			if err != nil {
				b.Fatalf("Send failed: %v", err)
			}

			_, err = transport2.Receive(ctx)
			if err != nil {
				b.Fatalf("Receive failed: %v", err)
			}
		}
	})
}