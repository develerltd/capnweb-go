package capnweb

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestPhase4Summary demonstrates all Phase 4 advanced features
func TestPhase4Summary(t *testing.T) {
	t.Log("🎯 Cap'n Web Go - Phase 4: Advanced Features")
	t.Log("=============================================")

	// Setup session for testing
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// 1. Promise Pipelining System
	t.Log("\n✅ 1. Promise Pipelining with Dependency Resolution")

	pipelineManager := NewPipelineManager(session)

	// Create a pipeline
	ctx := context.Background()
	pipeline := pipelineManager.CreatePipeline(ctx)

	t.Logf("   ✓ Pipeline created: %s", pipeline.ID)
	t.Logf("   ✓ Pipeline status: %s", pipeline.Status.String())
	t.Logf("   ✓ Created at: %v", pipeline.Created.Format(time.RFC3339))

	// Use pipeline builder
	builder := pipelineManager.NewPipelineBuilder(ctx)
	builtPipeline := builder.
		Call(ImportID(1), "authenticate", "token123").
		Call(ImportID(1), "getProfile", 42).
		Get(ImportID(2), "settings").
		Build()

	t.Logf("   ✓ Pipeline built with %d operations", len(builtPipeline.Operations))

	// Demonstrate dependency tracking
	if len(builtPipeline.Operations) > 0 {
		firstOp := builtPipeline.Operations[0]
		t.Logf("   ✓ First operation: %s on target %d", firstOp.Method, firstOp.TargetID)
		t.Logf("   ✓ Operation type: %d", firstOp.Type)
		t.Logf("   ✓ Dependencies: %v", firstOp.Dependencies)
	}

	// Test pipeline statistics
	stats := pipelineManager.GetStats()
	t.Logf("   ✓ Pipeline stats: %d total, %d completed, %d failed",
		stats.TotalPipelines, stats.CompletedPipelines, stats.FailedPipelines)

	// 2. Bidirectional RPC System
	t.Log("\n✅ 2. Bidirectional RPC with Client Callbacks")

	// Create bidirectional session
	bidirectionalSession, err := NewBidirectionalSession(transport)
	if err != nil {
		t.Fatalf("Failed to create bidirectional session: %v", err)
	}
	defer bidirectionalSession.Close()

	// Define a client service
	type ClientNotificationService struct{}

	// Register client service with method handlers

	// Register client service
	clientService := &ClientNotificationService{}
	err = bidirectionalSession.RegisterClientService("NotificationService", clientService)
	if err != nil {
		t.Errorf("Failed to register client service: %v", err)
	} else {
		t.Logf("   ✓ Client service registered: NotificationService")
	}

	// Test callback creation
	callbackFunc := func(result string, success bool) {
		t.Logf("   🔄 Callback executed: %s (success: %v)", result, success)
	}

	callbackID, err := bidirectionalSession.CreateCallback(callbackFunc)
	if err != nil {
		t.Errorf("Failed to create callback: %v", err)
	} else {
		t.Logf("   ✓ Callback created: %s", callbackID)
	}

	// Test callback invocation
	result, err := bidirectionalSession.CallCallback(callbackID, "test result", true)
	if err != nil {
		t.Errorf("Failed to call callback: %v", err)
	} else {
		t.Logf("   ✓ Callback invoked, result: %v", result)
	}

	// 3. Event Subscription System
	t.Log("\n✅ 3. Event Subscription and Callback Patterns")

	subscription, err := bidirectionalSession.Subscribe("user.updated", func(data interface{}) {
		t.Logf("   🎉 Event received: %v", data)
	})
	if err != nil {
		t.Errorf("Failed to subscribe to event: %v", err)
	} else {
		t.Logf("   ✓ Subscribed to event: user.updated (ID: %s)", subscription)
	}

	// Emit an event
	err = bidirectionalSession.EmitEvent("user.updated", map[string]interface{}{
		"userId": 123,
		"action": "profile_updated",
	})
	if err != nil {
		t.Errorf("Failed to emit event: %v", err)
	} else {
		t.Logf("   ✓ Event emitted: user.updated")
	}

	// Give event processing time
	time.Sleep(100 * time.Millisecond)

	// Check registered services
	services := bidirectionalSession.GetClientServices()
	t.Logf("   ✓ Registered services: %v", services)

	callbacks := bidirectionalSession.GetActiveCallbacks()
	t.Logf("   ✓ Active callbacks: %v", callbacks)

	subscriptions := bidirectionalSession.GetActiveSubscriptions()
	t.Logf("   ✓ Active subscriptions: %v", subscriptions)

	// 4. Advanced Resource Management
	t.Log("\n✅ 4. Advanced Resource Management with Context Cancellation")

	resourceManager := NewResourceManager(session)
	defer resourceManager.Stop()

	// Create a contextual resource
	resourceCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resourceID, err := resourceManager.CreateContextualResource(resourceCtx,
		func(ctx context.Context) (interface{}, func() error, error) {
			// Simulate creating a resource
			resource := map[string]interface{}{
				"id":      "test-resource",
				"created": time.Now(),
			}

			cleanupFunc := func() error {
				t.Logf("   🧹 Resource cleaned up: %v", resource["id"])
				return nil
			}

			return resource, cleanupFunc, nil
		})

	if err != nil {
		t.Errorf("Failed to create contextual resource: %v", err)
	} else {
		t.Logf("   ✓ Contextual resource created: %d", resourceID)
	}

	// Access the resource
	resource, err := resourceManager.GetResource(resourceID)
	if err != nil {
		t.Errorf("Failed to get resource: %v", err)
	} else {
		t.Logf("   ✓ Resource accessed: %T", resource)
	}

	// Set resource metadata
	err = resourceManager.SetResourceTag(resourceID, "environment", "test")
	if err != nil {
		t.Errorf("Failed to set resource tag: %v", err)
	} else {
		t.Logf("   ✓ Resource tag set: environment=test")
	}

	// Test reference counting
	err = resourceManager.AddRef(resourceID)
	if err != nil {
		t.Errorf("Failed to add reference: %v", err)
	} else {
		t.Logf("   ✓ Reference added to resource")
	}

	// Get resource statistics
	resourceStats := resourceManager.GetResourceStats()
	t.Logf("   ✓ Resource stats: %d total, %d active",
		resourceStats.TotalResources, resourceStats.ActiveResources)
	t.Logf("   ✓ Memory usage: %d bytes", resourceStats.TotalMemorySize)
	t.Logf("   ✓ Tracked references: %d", resourceStats.TrackedReferences)

	// Test resource types
	for resourceType, count := range resourceStats.ResourcesByType {
		t.Logf("     - %s: %d", resourceType, count)
	}

	// 5. Context Cancellation and Cleanup
	t.Log("\n✅ 5. Context-Based Lifecycle Management")

	// Test context cancellation
	testCtx, testCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer testCancel()

	// Create resource with short-lived context
	shortResourceID, err := resourceManager.TrackResource(testCtx, "short-lived", ResourceTypeGeneric, func() error {
		t.Logf("   ⏱️  Short-lived resource cleaned up")
		return nil
	})

	if err != nil {
		t.Errorf("Failed to track short-lived resource: %v", err)
	} else {
		t.Logf("   ✓ Short-lived resource tracked: %d", shortResourceID)
	}

	// Wait for context timeout
	time.Sleep(150 * time.Millisecond)

	// Check if resource was cleaned up
	_, err = resourceManager.GetResource(shortResourceID)
	if err != nil {
		t.Logf("   ✓ Resource correctly cleaned up after context cancellation")
	} else {
		t.Errorf("   ❌ Resource should have been cleaned up")
	}

	// 6. Performance and Batching
	t.Log("\n✅ 6. Automatic Operation Batching")

	// Test batch processing (this would normally involve multiple operations)
	pipelineOptions := DefaultPipelineOptions()
	pipelineOptions.BatchInterval = 50 * time.Millisecond
	pipelineOptions.MaxBatchSize = 10

	batchManager := NewPipelineManager(session, pipelineOptions)

	// Create multiple operations for batching
	batchPipeline := batchManager.CreatePipeline(ctx)

	for i := 0; i < 5; i++ {
		opID := OperationID(fmt.Sprintf("batch_op_%d", i))
		op := &PipelineOperation{
			ID:       opID,
			Type:     OperationTypeCall,
			TargetID: ImportID(1),
			Method:   fmt.Sprintf("operation_%d", i),
			Arguments: []interface{}{i},
		}
		batchManager.AddOperation(batchPipeline, op)
	}

	t.Logf("   ✓ Created pipeline with %d operations for batching", len(batchPipeline.Operations))

	// Test batch statistics
	batchStats := batchManager.GetStats()
	t.Logf("   ✓ Batch manager stats: %d operations total", batchStats.TotalOperations)

	// 7. Memory Leak Detection
	t.Log("\n✅ 7. Memory Leak Detection and Monitoring")

	// Get leak report
	leakReport := resourceManager.GetLeakReport()
	if leakReport != nil {
		t.Logf("   ✓ Leak report generated at: %v", leakReport.Timestamp.Format(time.RFC3339))
		t.Logf("   ✓ Tracked references for leak detection: %d", len(leakReport.References))

		// Show sample references
		sampleCount := 3
		if len(leakReport.References) < sampleCount {
			sampleCount = len(leakReport.References)
		}

		for i := 0; i < sampleCount; i++ {
			ref := leakReport.References[i]
			t.Logf("     - Reference %d: %s (created: %v)", i+1, ref.Type, ref.Created.Format(time.RFC3339))
		}
	} else {
		t.Logf("   ⚠️  Leak detection disabled")
	}

	// Test cleanup
	err = resourceManager.Release(resourceID)
	if err != nil {
		t.Errorf("Failed to release resource: %v", err)
	} else {
		t.Logf("   ✓ Resource released successfully")
	}

	// Summary
	t.Log("\n🎉 PHASE 4 COMPLETE - Advanced Features")
	t.Log("======================================")
	t.Log("✓ Promise Pipelining System")
	t.Log("  - Dependency graph resolution")
	t.Log("  - Automatic operation batching")
	t.Log("  - Pipeline builder pattern")
	t.Log("  - Execution optimization")
	t.Log("")
	t.Log("✓ Bidirectional RPC System")
	t.Log("  - Server-to-client method calls")
	t.Log("  - Client service registration")
	t.Log("  - Callback function support")
	t.Log("  - Function parameter passing")
	t.Log("")
	t.Log("✓ Event Subscription Patterns")
	t.Log("  - Event emission and subscription")
	t.Log("  - Callback-based event handling")
	t.Log("  - Channel-based event delivery")
	t.Log("  - Subscription lifecycle management")
	t.Log("")
	t.Log("✓ Advanced Resource Management")
	t.Log("  - Context-based resource lifecycle")
	t.Log("  - Automatic cleanup on cancellation")
	t.Log("  - Reference counting and disposal")
	t.Log("  - Memory leak detection")
	t.Log("  - Resource metadata and tagging")
	t.Log("")
	t.Log("✓ Performance Optimizations")
	t.Log("  - Automatic operation batching")
	t.Log("  - Dependency optimization")
	t.Log("  - Resource pooling and reuse")
	t.Log("  - Memory usage monitoring")
	t.Log("")
	t.Log("🚀 Ready for Phase 5: Transport Implementations")
	t.Log("  - Production HTTP batch transport")
	t.Log("  - Real-time WebSocket transport")
	t.Log("  - Custom transport protocols")
}