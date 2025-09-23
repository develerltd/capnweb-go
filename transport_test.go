package capnweb

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestMemoryTransportPair(t *testing.T) {
	t1, t2 := NewMemoryTransportPair()
	defer t1.Close()
	defer t2.Close()

	ctx := context.Background()

	// Test basic send/receive
	message := []byte("hello world")
	err := t1.Send(ctx, message)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	received, err := t2.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive failed: %v", err)
	}

	if string(received) != string(message) {
		t.Errorf("Expected %q, got %q", message, received)
	}
}

func TestMemoryTransportBidirectional(t *testing.T) {
	t1, t2 := NewMemoryTransportPair()
	defer t1.Close()
	defer t2.Close()

	ctx := context.Background()

	// Send from t1 to t2
	msg1 := []byte("message from t1")
	err := t1.Send(ctx, msg1)
	if err != nil {
		t.Fatalf("t1.Send failed: %v", err)
	}

	received1, err := t2.Receive(ctx)
	if err != nil {
		t.Fatalf("t2.Receive failed: %v", err)
	}

	if string(received1) != string(msg1) {
		t.Errorf("Expected %q, got %q", msg1, received1)
	}

	// Send from t2 to t1
	msg2 := []byte("message from t2")
	err = t2.Send(ctx, msg2)
	if err != nil {
		t.Fatalf("t2.Send failed: %v", err)
	}

	received2, err := t1.Receive(ctx)
	if err != nil {
		t.Fatalf("t1.Receive failed: %v", err)
	}

	if string(received2) != string(msg2) {
		t.Errorf("Expected %q, got %q", msg2, received2)
	}
}

func TestMemoryTransportClose(t *testing.T) {
	t1, t2 := NewMemoryTransportPair()

	ctx := context.Background()

	// Close t1
	err := t1.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Send should fail after close
	err = t1.Send(ctx, []byte("test"))
	if err != ErrTransportClosed {
		t.Errorf("Expected ErrTransportClosed, got %v", err)
	}

	// Receive should return EOF after close
	_, err = t1.Receive(ctx)
	if err != io.EOF {
		t.Errorf("Expected io.EOF, got %v", err)
	}

	// t2 should still work
	err = t2.Send(ctx, []byte("test"))
	if err != nil {
		t.Errorf("t2.Send should still work: %v", err)
	}

	t2.Close()
}

func TestMemoryTransportAbort(t *testing.T) {
	t1, t2 := NewMemoryTransportPair()
	defer t2.Close()

	ctx := context.Background()
	abortErr := errors.New("test abort")

	// Abort t1
	t1.Abort(abortErr)

	// Send should fail after abort
	err := t1.Send(ctx, []byte("test"))
	if err == nil {
		t.Error("Send should fail after abort")
	}

	// Receive should fail after abort
	_, err = t1.Receive(ctx)
	if err == nil {
		t.Error("Receive should fail after abort")
	}
}

func TestMemoryTransportStats(t *testing.T) {
	t1, t2 := NewMemoryTransportPair()
	defer t1.Close()
	defer t2.Close()

	ctx := context.Background()

	// Send some messages
	messages := [][]byte{
		[]byte("message 1"),
		[]byte("message 2"),
		[]byte("longer message 3"),
	}

	var totalBytes uint64
	for _, msg := range messages {
		err := t1.Send(ctx, msg)
		if err != nil {
			t.Fatalf("Send failed: %v", err)
		}
		totalBytes += uint64(len(msg))

		// Receive the message
		_, err = t2.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive failed: %v", err)
		}
	}

	// Check t1 stats (sender)
	stats1 := t1.Stats()
	if stats1.MessagesSent != uint64(len(messages)) {
		t.Errorf("Expected %d messages sent, got %d", len(messages), stats1.MessagesSent)
	}
	if stats1.BytesSent != totalBytes {
		t.Errorf("Expected %d bytes sent, got %d", totalBytes, stats1.BytesSent)
	}

	// Check t2 stats (receiver)
	stats2 := t2.Stats()
	if stats2.MessagesReceived != uint64(len(messages)) {
		t.Errorf("Expected %d messages received, got %d", len(messages), stats2.MessagesReceived)
	}
	if stats2.BytesReceived != totalBytes {
		t.Errorf("Expected %d bytes received, got %d", totalBytes, stats2.BytesReceived)
	}
}

func TestMemoryTransportMessageSizeLimit(t *testing.T) {
	t1, t2 := NewMemoryTransportPair()
	defer t1.Close()
	defer t2.Close()

	ctx := context.Background()

	// Create a message that exceeds the size limit
	largeMessage := make([]byte, MessageSizeLimit+1)

	err := t1.Send(ctx, largeMessage)
	if err != ErrMessageTooLarge {
		t.Errorf("Expected ErrMessageTooLarge, got %v", err)
	}

	// Check that error was counted in stats
	stats := t1.Stats()
	if stats.Errors != 1 {
		t.Errorf("Expected 1 error, got %d", stats.Errors)
	}
}

func TestMemoryTransportContextCancellation(t *testing.T) {
	t1, t2 := NewMemoryTransportPair()
	defer t1.Close()
	defer t2.Close()

	// Test send cancellation - fill the buffer first to force blocking
	ctx := context.Background()
	for i := 0; i < 16; i++ { // Fill the buffer
		t1.Send(ctx, []byte("test"))
	}

	// Now test with canceled context
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := t1.Send(canceledCtx, []byte("test"))
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}

	// Test receive cancellation with timeout
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err = t1.Receive(timeoutCtx)
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}
}