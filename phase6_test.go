package capnweb

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestMapOperations tests the map operations functionality
func TestMapOperations(t *testing.T) {
	session := &Session{} // Mock session for testing
	mapOp := NewMapOperation(session, ImportID(123), []string{"items"})

	// Test map operation creation
	if mapOp.importID != 123 {
		t.Errorf("Expected importID 123, got %d", mapOp.importID)
	}

	if len(mapOp.path) != 1 || mapOp.path[0] != "items" {
		t.Errorf("Expected path ['items'], got %v", mapOp.path)
	}

	// Test setting chunk size
	mapOp.SetChunkSize(50)
	if mapOp.chunkSize != 50 {
		t.Errorf("Expected chunk size 50, got %d", mapOp.chunkSize)
	}

	// Test function serialization
	testFunc := func(x int) int { return x * 2 }
	serialized := mapOp.serializeFunction(testFunc)

	if serialized["type"] != "function" {
		t.Error("Expected serialized function type to be 'function'")
	}

	paramTypes, ok := serialized["paramTypes"].([]string)
	if !ok || len(paramTypes) != 1 {
		t.Error("Expected one parameter type")
	}

	returnTypes, ok := serialized["returnTypes"].([]string)
	if !ok || len(returnTypes) != 1 {
		t.Error("Expected one return type")
	}
}

// TestBatchMapOperation tests batch map operations
func TestBatchMapOperation(t *testing.T) {
	session := &Session{} // Mock session
	batch := NewBatchMapOperation(session)

	if batch.Count() != 0 {
		t.Error("Expected empty batch initially")
	}

	// Add operations
	mapOp := NewMapOperation(session, ImportID(123), []string{"items"})
	transformFunc := func(x int) string { return "processed" }

	batch.AddMapOperation(mapOp, transformFunc)
	if batch.Count() != 1 {
		t.Error("Expected batch count 1 after adding operation")
	}

	filterFunc := func(x int) bool { return x > 0 }
	batch.AddFilterOperation(mapOp, filterFunc)
	if batch.Count() != 2 {
		t.Error("Expected batch count 2 after adding filter operation")
	}

	// Test clear
	batch.Clear()
	if batch.Count() != 0 {
		t.Error("Expected empty batch after clear")
	}
}

// TestStreamReader tests the streaming reader functionality
func TestStreamReader(t *testing.T) {
	session := &Session{} // Mock session
	options := DefaultStreamOptions()
	options.ChunkSize = 1024

	reader := NewRPCStreamReader(session, ImportID(456), []string{"stream"}, options)
	defer reader.Close()

	// Test initial state
	if reader.closed {
		t.Error("Reader should not be closed initially")
	}

	if reader.streamID == "" {
		t.Error("Stream ID should not be empty")
	}

	// Test reading (will use simulated data)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	data, err := reader.Read(ctx)
	if err != nil {
		t.Errorf("Expected successful read, got error: %v", err)
	}

	if len(data) == 0 {
		t.Error("Expected non-empty data")
	}

	// Test stats
	stats := reader.Stats()
	if stats.BytesTransferred == 0 {
		t.Error("Expected bytes transferred to be > 0")
	}

	if stats.ChunksTransferred == 0 {
		t.Error("Expected chunks transferred to be > 0")
	}
}

// TestStreamWriter tests the streaming writer functionality
func TestStreamWriter(t *testing.T) {
	session := &Session{} // Mock session
	options := DefaultStreamOptions()
	options.FlushInterval = 50 * time.Millisecond

	writer := NewRPCStreamWriter(session, ImportID(789), []string{"output"}, options)
	defer writer.Close()

	// Test initial state
	if writer.closed {
		t.Error("Writer should not be closed initially")
	}

	// Test writing
	ctx := context.Background()
	testData := []byte("test data for streaming")

	err := writer.Write(ctx, testData)
	if err != nil {
		t.Errorf("Expected successful write, got error: %v", err)
	}

	// Test flush
	err = writer.Flush(ctx)
	if err != nil {
		t.Errorf("Expected successful flush, got error: %v", err)
	}

	// Test stats
	stats := writer.Stats()
	if stats.BytesTransferred == 0 {
		t.Error("Expected bytes transferred to be > 0")
	}
}

// TestStreamManager tests the stream manager functionality
func TestStreamManager(t *testing.T) {
	session := &Session{} // Mock session
	manager := NewStreamManager(session)

	// Test creating stream reader
	reader, err := manager.CreateStreamReader(ImportID(123), []string{"input"})
	if err != nil {
		t.Errorf("Failed to create stream reader: %v", err)
	}

	if manager.GetStreamCount() != 1 {
		t.Error("Expected stream count 1 after creating reader")
	}

	// Test creating stream writer
	_, err = manager.CreateStreamWriter(ImportID(456), []string{"output"})
	if err != nil {
		t.Errorf("Failed to create stream writer: %v", err)
	}

	if manager.GetStreamCount() != 2 {
		t.Error("Expected stream count 2 after creating writer")
	}

	// Test closing specific stream
	err = manager.CloseStream(reader.streamID)
	if err != nil {
		t.Errorf("Failed to close stream: %v", err)
	}

	if manager.GetStreamCount() != 1 {
		t.Error("Expected stream count 1 after closing one stream")
	}

	// Test closing all streams
	err = manager.CloseAllStreams()
	if err != nil {
		t.Errorf("Failed to close all streams: %v", err)
	}

	if manager.GetStreamCount() != 0 {
		t.Error("Expected stream count 0 after closing all streams")
	}

	// Test global stats
	stats := manager.GetGlobalStats()
	if stats.StartTime.IsZero() {
		t.Error("Expected non-zero start time in global stats")
	}
}

// TestCapability tests the capability functionality
func TestCapability(t *testing.T) {
	// Create a capability
	capability := &Capability{
		ID:          "test-cap-123",
		Resource:    "user.123",
		Operations:  []string{"read", "write"},
		Attributes:  map[string]string{"scope": "profile"},
		IssuedAt:    time.Now(),
		Issuer:      "test-issuer",
		Subject:     "user-456",
		Delegatable: true,
		Used:        false,
		UseCount:    0,
		MaxUses:     5,
	}

	// Test validity
	if !capability.IsValid() {
		t.Error("Capability should be valid")
	}

	// Test operation check
	if !capability.CanPerform("user.123", "read") {
		t.Error("Capability should allow read operation on user.123")
	}

	if capability.CanPerform("user.456", "read") {
		t.Error("Capability should not allow read operation on user.456")
	}

	if !capability.CanPerform("user.123", "write") {
		t.Error("Capability should allow write operation on user.123")
	}

	if capability.CanPerform("user.123", "delete") {
		t.Error("Capability should not allow delete operation")
	}

	// Test wildcard resource
	capability.Resource = "user.*"
	if !capability.CanPerform("user.789", "read") {
		t.Error("Capability should allow read operation on user.789 with wildcard")
	}

	// Test using capability
	err := capability.Use()
	if err != nil {
		t.Errorf("Expected successful use, got error: %v", err)
	}

	if capability.UseCount != 1 {
		t.Errorf("Expected use count 1, got %d", capability.UseCount)
	}

	// Test expiration
	pastTime := time.Now().Add(-time.Hour)
	capability.ExpiresAt = &pastTime
	if capability.IsValid() {
		t.Error("Capability should be invalid after expiration")
	}

	// Test max uses
	capability.ExpiresAt = nil
	capability.UseCount = 5
	if capability.IsValid() {
		t.Error("Capability should be invalid after reaching max uses")
	}
}

// TestCapabilityStore tests the capability store functionality
func TestCapabilityStore(t *testing.T) {
	store := NewMemoryCapabilityStore()

	capability := &Capability{
		ID:         "test-cap-456",
		Resource:   "file.txt",
		Operations: []string{"read"},
		Subject:    "user-123",
		IssuedAt:   time.Now(),
		Issuer:     "test",
	}

	// Test storing capability
	err := store.Store(capability)
	if err != nil {
		t.Errorf("Failed to store capability: %v", err)
	}

	// Test retrieving capability
	retrieved, err := store.Retrieve("test-cap-456")
	if err != nil {
		t.Errorf("Failed to retrieve capability: %v", err)
	}

	if retrieved.ID != capability.ID {
		t.Error("Retrieved capability ID doesn't match")
	}

	// Test validation
	validated, err := store.Validate("test-cap-456", "file.txt", "read")
	if err != nil {
		t.Errorf("Capability validation failed: %v", err)
	}

	if validated.ID != capability.ID {
		t.Error("Validated capability ID doesn't match")
	}

	// Test invalid validation
	_, err = store.Validate("test-cap-456", "file.txt", "write")
	if err == nil {
		t.Error("Expected validation to fail for write operation")
	}

	// Test listing capabilities
	capabilities, err := store.List("user-123")
	if err != nil {
		t.Errorf("Failed to list capabilities: %v", err)
	}

	if len(capabilities) != 1 {
		t.Errorf("Expected 1 capability, got %d", len(capabilities))
	}

	// Test revoking capability
	err = store.Revoke("test-cap-456")
	if err != nil {
		t.Errorf("Failed to revoke capability: %v", err)
	}

	_, err = store.Retrieve("test-cap-456")
	if err == nil {
		t.Error("Expected error when retrieving revoked capability")
	}
}

// TestTokenAuthProvider tests the token authentication provider
func TestTokenAuthProvider(t *testing.T) {
	provider := NewTokenAuthProvider()

	// Add a token
	provider.AddToken("test-token-123", "user-456")

	// Test successful authentication
	ctx := context.Background()
	credentials := map[string]interface{}{
		"token": "test-token-123",
	}

	subject, err := provider.Authenticate(ctx, credentials)
	if err != nil {
		t.Errorf("Authentication failed: %v", err)
	}

	if subject != "user-456" {
		t.Errorf("Expected subject 'user-456', got '%s'", subject)
	}

	// Test invalid token
	credentials["token"] = "invalid-token"
	_, err = provider.Authenticate(ctx, credentials)
	if err == nil {
		t.Error("Expected authentication to fail with invalid token")
	}

	// Test missing token
	credentials = map[string]interface{}{}
	_, err = provider.Authenticate(ctx, credentials)
	if err == nil {
		t.Error("Expected authentication to fail with missing token")
	}

	// Test auth scheme
	if provider.GetAuthScheme() != "bearer" {
		t.Error("Expected auth scheme 'bearer'")
	}

	// Test token removal
	provider.RemoveToken("test-token-123")
	credentials["token"] = "test-token-123"
	_, err = provider.Authenticate(ctx, credentials)
	if err == nil {
		t.Error("Expected authentication to fail after token removal")
	}
}

// TestSecurityManager tests the security manager functionality
func TestSecurityManager(t *testing.T) {
	config := DefaultSecurityConfig()
	config.SessionTimeout = time.Minute
	manager := NewSecurityManager(config)

	// Register auth provider
	tokenProvider := NewTokenAuthProvider()
	tokenProvider.AddToken("valid-token", "user-123")
	manager.RegisterAuthProvider(tokenProvider)

	// Test authentication
	ctx := context.Background()
	credentials := map[string]interface{}{
		"token": "valid-token",
	}

	securityContext, err := manager.Authenticate(ctx, "bearer", credentials)
	if err != nil {
		t.Errorf("Authentication failed: %v", err)
	}

	if securityContext.Subject != "user-123" {
		t.Error("Expected subject 'user-123'")
	}

	if !securityContext.Authenticated {
		t.Error("Security context should be authenticated")
	}

	// Test getting security context
	retrievedContext, err := manager.GetSecurityContext(securityContext.SessionID)
	if err != nil {
		t.Errorf("Failed to get security context: %v", err)
	}

	if retrievedContext.SessionID != securityContext.SessionID {
		t.Error("Session ID mismatch")
	}

	// Test creating capability
	capability, err := manager.CreateCapability("user-123", "document.*", []string{"read", "write"}, nil)
	if err != nil {
		t.Errorf("Failed to create capability: %v", err)
	}

	// Test granting capability
	err = manager.GrantCapability(securityContext.SessionID, capability.ID)
	if err != nil {
		t.Errorf("Failed to grant capability: %v", err)
	}

	// Test authorization check
	err = manager.CheckAuthorization(securityContext.SessionID, "document.123", "read")
	if err != nil {
		t.Errorf("Authorization check failed: %v", err)
	}

	// Test authorization denial
	err = manager.CheckAuthorization(securityContext.SessionID, "document.123", "delete")
	if err == nil {
		t.Error("Expected authorization to fail for delete operation")
	}

	// Test capability delegation
	delegated, err := manager.DelegateCapability(capability.ID, "user-456", []string{"read"})
	if err != nil {
		t.Errorf("Failed to delegate capability: %v", err)
	}

	if delegated.Subject != "user-456" {
		t.Error("Delegated capability should have new subject")
	}

	if len(delegated.Operations) != 1 || delegated.Operations[0] != "read" {
		t.Error("Delegated capability should have restricted operations")
	}

	// Test capability revocation
	err = manager.RevokeCapability(capability.ID)
	if err != nil {
		t.Errorf("Failed to revoke capability: %v", err)
	}

	// Test logout
	err = manager.Logout(securityContext.SessionID)
	if err != nil {
		t.Errorf("Failed to logout: %v", err)
	}

	_, err = manager.GetSecurityContext(securityContext.SessionID)
	if err == nil {
		t.Error("Expected error when getting security context after logout")
	}
}

// TestTokenBucketLimiter tests the token bucket rate limiter
func TestTokenBucketLimiter(t *testing.T) {
	config := DefaultTokenBucketConfig()
	config.DefaultCapacity = 5
	config.DefaultRefill = 100 * time.Millisecond

	limiter := NewTokenBucketLimiter(config)

	key := "test-user"

	// Test initial allows
	for i := 0; i < 5; i++ {
		if !limiter.Allow(key) {
			t.Errorf("Request %d should be allowed", i)
		}
	}

	// Test rate limit exceeded
	if limiter.Allow(key) {
		t.Error("Request should be denied when bucket is empty")
	}

	// Test AllowN
	if limiter.AllowN(key, 2) {
		t.Error("AllowN(2) should be denied when bucket is empty")
	}

	// Wait for refill
	time.Sleep(150 * time.Millisecond)

	// Test refill
	if !limiter.Allow(key) {
		t.Error("Request should be allowed after refill")
	}

	// Test stats
	stats := limiter.Stats(key)
	if stats.RequestsAllowed == 0 {
		t.Error("Expected some allowed requests in stats")
	}

	if stats.RequestsDenied == 0 {
		t.Error("Expected some denied requests in stats")
	}

	// Test setting custom limit
	err := limiter.SetLimit(key, 10, 50*time.Millisecond)
	if err != nil {
		t.Errorf("Failed to set limit: %v", err)
	}

	capacity, refillRate := limiter.GetLimit(key)
	if capacity != 10 {
		t.Errorf("Expected capacity 10, got %d", capacity)
	}

	if refillRate != 50*time.Millisecond {
		t.Errorf("Expected refill rate 50ms, got %v", refillRate)
	}

	// Test reset
	limiter.Reset(key)
	newStats := limiter.Stats(key)
	if newStats.RequestsAllowed != 0 {
		t.Error("Expected stats to be reset")
	}
}

// TestQuotaManager tests the quota manager functionality
func TestQuotaManager(t *testing.T) {
	config := DefaultQuotaConfig()
	config.DefaultQuota = 100
	config.ResetInterval = time.Second

	manager := NewQuotaManager(config)
	key := "test-user"

	// Test initial quota usage
	if !manager.UseQuota(key, 50) {
		t.Error("Should be able to use 50 units of quota")
	}

	info := manager.GetQuotaInfo(key)
	if info.Used != 50 {
		t.Errorf("Expected used quota 50, got %d", info.Used)
	}

	if info.Remaining != 50 {
		t.Errorf("Expected remaining quota 50, got %d", info.Remaining)
	}

	// Test quota check
	if !manager.CheckQuota(key, 30) {
		t.Error("Should be able to check for 30 units")
	}

	if manager.CheckQuota(key, 60) {
		t.Error("Should not be able to check for 60 units (exceeds remaining)")
	}

	// Test quota exhaustion
	if !manager.UseQuota(key, 50) {
		t.Error("Should be able to use remaining 50 units")
	}

	if manager.UseQuota(key, 1) {
		t.Error("Should not be able to use more quota when exhausted")
	}

	// Test quota info
	info = manager.GetQuotaInfo(key)
	if info.Used != 100 {
		t.Errorf("Expected used quota 100, got %d", info.Used)
	}

	if info.Remaining != 0 {
		t.Errorf("Expected remaining quota 0, got %d", info.Remaining)
	}

	// Test stats
	stats := manager.GetStats(key)
	if stats.TotalRequests == 0 {
		t.Error("Expected some total requests in stats")
	}

	if stats.AllowedRequests != 2 {
		t.Errorf("Expected 2 allowed requests, got %d", stats.AllowedRequests)
	}

	if stats.DeniedRequests == 0 {
		t.Error("Expected some denied requests in stats")
	}

	// Test quota reset
	manager.ResetQuota(key)
	info = manager.GetQuotaInfo(key)
	if info.Used != 0 {
		t.Error("Expected quota to be reset to 0")
	}

	// Test setting custom quota
	manager.SetQuota(key, 200, 2*time.Second)
	info = manager.GetQuotaInfo(key)
	if info.Limit != 200 {
		t.Errorf("Expected quota limit 200, got %d", info.Limit)
	}
}

// TestRateLimitQuotaManager tests the combined rate limit and quota manager
func TestRateLimitQuotaManager(t *testing.T) {
	rateLimitConfig := DefaultTokenBucketConfig()
	rateLimitConfig.DefaultCapacity = 3
	rateLimitConfig.DefaultRefill = 100 * time.Millisecond

	quotaConfig := DefaultQuotaConfig()
	quotaConfig.DefaultQuota = 10

	manager := NewRateLimitQuotaManager(rateLimitConfig, quotaConfig)
	key := "test-user"

	// Test successful request
	err := manager.UseRequest(key, 2)
	if err != nil {
		t.Errorf("First request should succeed: %v", err)
	}

	// Test multiple requests within rate limit and quota
	for i := 0; i < 2; i++ {
		err := manager.UseRequest(key, 2)
		if err != nil {
			t.Errorf("Request %d should succeed: %v", i, err)
		}
	}

	// Test rate limit exceeded
	err = manager.UseRequest(key, 2)
	if err == nil {
		t.Error("Request should fail due to rate limit")
	}

	// Wait for rate limit to refresh
	time.Sleep(150 * time.Millisecond)

	// Test quota exceeded (we've used 6 units, trying to use 6 more = 12 > 10)
	err = manager.UseRequest(key, 6)
	if err == nil {
		t.Error("Request should fail due to quota exceeded")
	}

	// Test wait for request
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = manager.WaitForRequest(ctx, key, 2)
	if err != nil {
		t.Errorf("Wait for request should succeed: %v", err)
	}

	// Test getting stats
	rateLimitStats := manager.GetRateLimitStats(key)
	if rateLimitStats.RequestsAllowed == 0 {
		t.Error("Expected some allowed requests in rate limit stats")
	}

	quotaStats := manager.GetQuotaStats(key)
	if quotaStats.AllowedRequests == 0 {
		t.Error("Expected some allowed requests in quota stats")
	}

	quotaInfo := manager.GetQuotaInfo(key)
	if quotaInfo.Used == 0 {
		t.Error("Expected some quota usage")
	}

	// Test setting limits
	err = manager.SetRateLimit(key, 5, 50*time.Millisecond)
	if err != nil {
		t.Errorf("Failed to set rate limit: %v", err)
	}

	manager.SetQuota(key, 20, time.Minute)

	// Test reset
	manager.Reset(key)
}

// TestConcurrentMapOperations tests map operations under concurrent load
func TestConcurrentMapOperations(t *testing.T) {
	session := &Session{}
	numWorkers := 10
	operationsPerWorker := 100

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()

			mapOp := NewMapOperation(session, ImportID(workerID), []string{"data"})
			batch := NewBatchMapOperation(session)

			for j := 0; j < operationsPerWorker; j++ {
				// Test various operations
				transformFunc := func(x int) int { return x * 2 }
				batch.AddMapOperation(mapOp, transformFunc)

				if j%10 == 0 {
					batch.Clear()
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestConcurrentSecurity tests security manager under concurrent load
func TestConcurrentSecurity(t *testing.T) {
	config := DefaultSecurityConfig()
	manager := NewSecurityManager(config)

	tokenProvider := NewTokenAuthProvider()
	for i := 0; i < 100; i++ {
		tokenProvider.AddToken(fmt.Sprintf("token-%d", i), fmt.Sprintf("user-%d", i))
	}
	manager.RegisterAuthProvider(tokenProvider)

	numWorkers := 20
	operationsPerWorker := 50

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()

			ctx := context.Background()
			credentials := map[string]interface{}{
				"token": fmt.Sprintf("token-%d", workerID),
			}

			for j := 0; j < operationsPerWorker; j++ {
				// Authenticate
				securityContext, err := manager.Authenticate(ctx, "bearer", credentials)
				if err != nil {
					t.Errorf("Worker %d authentication failed: %v", workerID, err)
					return
				}

				// Create capability
				capability, err := manager.CreateCapability(
					securityContext.Subject,
					fmt.Sprintf("resource-%d", j),
					[]string{"read"},
					nil,
				)
				if err != nil {
					t.Errorf("Worker %d capability creation failed: %v", workerID, err)
					return
				}

				// Grant capability
				err = manager.GrantCapability(securityContext.SessionID, capability.ID)
				if err != nil {
					t.Errorf("Worker %d capability grant failed: %v", workerID, err)
					return
				}

				// Check authorization
				err = manager.CheckAuthorization(securityContext.SessionID, fmt.Sprintf("resource-%d", j), "read")
				if err != nil {
					t.Errorf("Worker %d authorization failed: %v", workerID, err)
					return
				}

				// Logout
				manager.Logout(securityContext.SessionID)
			}
		}(i)
	}

	wg.Wait()
}

// TestConcurrentRateLimiting tests rate limiting under concurrent load
func TestConcurrentRateLimiting(t *testing.T) {
	config := DefaultTokenBucketConfig()
	config.DefaultCapacity = 10
	config.DefaultRefill = 10 * time.Millisecond

	limiter := NewTokenBucketLimiter(config)
	key := "concurrent-test"

	numWorkers := 50
	requestsPerWorker := 100

	var wg sync.WaitGroup
	var allowedCount, deniedCount int64
	var mu sync.Mutex

	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()

			for j := 0; j < requestsPerWorker; j++ {
				if limiter.Allow(key) {
					mu.Lock()
					allowedCount++
					mu.Unlock()
				} else {
					mu.Lock()
					deniedCount++
					mu.Unlock()
				}

				// Small delay to avoid tight loop
				time.Sleep(time.Microsecond)
			}
		}()
	}

	wg.Wait()

	totalRequests := int64(numWorkers * requestsPerWorker)
	if allowedCount + deniedCount != totalRequests {
		t.Errorf("Request count mismatch: allowed=%d, denied=%d, total=%d",
			allowedCount, deniedCount, totalRequests)
	}

	// Should have some denied requests due to rate limiting
	if deniedCount == 0 {
		t.Error("Expected some requests to be denied due to rate limiting")
	}

	t.Logf("Rate limiting results: %d allowed, %d denied out of %d total requests",
		allowedCount, deniedCount, totalRequests)
}