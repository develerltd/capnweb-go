package capnweb

import (
	"context"
	"fmt"
	"os"
	"time"
)

// This file contains comprehensive examples demonstrating Cap'n Web usage

// Example 1: Basic RPC Client/Server
func ExampleBasicRPC() {
	fmt.Println("=== Basic RPC Example ===")

	// 1. Create an in-process transport pair for testing
	clientTransport, serverTransport := NewInProcessTransportPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	// 2. Create client session
	clientSession, err := NewSession(clientTransport, DefaultSessionOptions())
	if err != nil {
		fmt.Printf("Failed to create client session: %v\n", err)
		return
	}
	defer clientSession.Close()

	// 3. Create server session
	serverSession, err := NewSession(serverTransport, DefaultSessionOptions())
	if err != nil {
		fmt.Printf("Failed to create server session: %v\n", err)
		return
	}
	defer serverSession.Close()

	// 4. Define service interface
	type CalculatorService interface {
		Add(ctx context.Context, a, b int) (int, error)
		Multiply(ctx context.Context, a, b int) (int, error)
		Divide(ctx context.Context, a, b float64) (float64, error)
	}

	// 5. Implement service
	type calculatorImpl struct{}

	// Implementation would be defined separately in a real application
	// For this example, we'll just show the interface usage

	// 6. Register service with interface registry
	registry := NewInterfaceRegistry()
	_, err = registry.RegisterInterface((*CalculatorService)(nil))
	if err != nil {
		fmt.Printf("Failed to register interface: %v\n", err)
		return
	}

	// 7. Export service implementation (mock)
	impl := &calculatorImpl{}
	exportID := ExportID(1) // Mock export ID for example

	fmt.Printf("Service exported with ID: %d\n", exportID)
	_ = impl // Use the variable

	// 8. Create client stub (mock for example)
	stub := &MockStub{}
	_ = stub // Use the variable
	defer func() {
		if stub != nil {
			stub.Dispose()
		}
	}()

	// 9. Make RPC calls (demonstration)
	fmt.Printf("In a real implementation, RPC calls would be made here:\n")
	fmt.Printf("Add(10, 20) = 30\n")
	fmt.Printf("Multiply(5, 6) = 30\n")

	fmt.Println("Basic RPC example completed successfully!")
}

// Example 2: Promise Pipelining
func ExamplePromisePipelining() {
	fmt.Println("\n=== Promise Pipelining Example ===")

	// Create transport and session (simplified setup)
	clientTransport, serverTransport := NewInProcessTransportPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	clientSession, _ := NewSession(clientTransport, DefaultSessionOptions())
	defer clientSession.Close()

	serverSession, _ := NewSession(serverTransport, DefaultSessionOptions())
	defer serverSession.Close()

	// Define a user service
	type User struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	type UserService interface {
		GetUser(ctx context.Context, id int) (*User, error)
		GetUserName(ctx context.Context, id int) (string, error)
		GetFriends(ctx context.Context, userID int) ([]*User, error)
	}

	// Implementation
	type userServiceImpl struct {
		users map[int]*User
	}

	// Service methods would be implemented separately

	// Set up service
	impl := &userServiceImpl{
		users: map[int]*User{
			1: {ID: 1, Name: "Alice"},
			2: {ID: 2, Name: "Bob"},
			3: {ID: 3, Name: "Charlie"},
		},
	}

	// In a real implementation, would export and import stubs
	fmt.Println("Setting up RPC stubs...")

	ctx := context.Background()

	// Example of promise pipelining: chain multiple calls
	fmt.Println("Making pipelined calls...")

	// In a real implementation, these would be actual RPC calls
	fmt.Printf("User: %+v\n", impl.users[1])
	fmt.Printf("Friends: [Friend 1, Friend 2]\n")
	fmt.Printf("Name: %s\n", impl.users[1].Name)

	fmt.Println("Promise pipelining example completed!")
}

// Example 3: HTTP Transport Usage
func ExampleHTTPTransport() {
	fmt.Println("\n=== HTTP Transport Example ===")

	// Create HTTP transport
	transport, err := NewHTTPBatchTransport("http://localhost:8080/rpc")
	if err != nil {
		fmt.Printf("Failed to create HTTP transport: %v\n", err)
		return
	}
	defer transport.Close()

	// Create session
	session, err := NewSession(transport, DefaultSessionOptions())
	if err != nil {
		fmt.Printf("Failed to create session: %v\n", err)
		return
	}
	defer session.Close()

	fmt.Println("HTTP transport session created")

	// In a real scenario, you would make RPC calls here
	// For demo purposes, we'll just show the setup

	stats := transport.GetStats()
	fmt.Printf("Transport stats: %+v\n", stats)

	fmt.Println("HTTP transport example completed!")
}

// Example 4: Security and Capabilities
func ExampleSecurity() {
	fmt.Println("\n=== Security Example ===")

	// Create security manager
	config := DefaultSecurityConfig()
	securityManager := NewSecurityManager(config)

	// Create token auth provider
	tokenProvider := NewTokenAuthProvider()
	tokenProvider.AddToken("user123-token", "user123")
	tokenProvider.AddToken("admin456-token", "admin456")

	// Register auth provider
	securityManager.RegisterAuthProvider(tokenProvider)

	ctx := context.Background()

	// Authenticate user
	credentials := map[string]interface{}{
		"token": "user123-token",
	}

	securityContext, err := securityManager.Authenticate(ctx, "bearer", credentials)
	if err != nil {
		fmt.Printf("Authentication failed: %v\n", err)
		return
	}

	fmt.Printf("User authenticated: %s\n", securityContext.Subject)

	// Create capability for user
	capability, err := securityManager.CreateCapability(
		"user123",
		"document.*",
		[]string{"read", "write"},
		map[string]string{"department": "engineering"},
	)
	if err != nil {
		fmt.Printf("Failed to create capability: %v\n", err)
		return
	}

	fmt.Printf("Capability created: %s\n", capability.ID)

	// Grant capability to session
	err = securityManager.GrantCapability(securityContext.SessionID, capability.ID)
	if err != nil {
		fmt.Printf("Failed to grant capability: %v\n", err)
		return
	}

	// Test authorization
	err = securityManager.CheckAuthorization(securityContext.SessionID, "document.123", "read")
	if err != nil {
		fmt.Printf("Authorization failed: %v\n", err)
		return
	}

	fmt.Println("Authorization successful for read operation")

	// Test delegation
	delegated, err := securityManager.DelegateCapability(capability.ID, "user789", []string{"read"})
	if err != nil {
		fmt.Printf("Failed to delegate capability: %v\n", err)
		return
	}

	fmt.Printf("Capability delegated to user789: %s\n", delegated.ID)

	fmt.Println("Security example completed!")
}

// Example 5: Rate Limiting and Quotas
func ExampleRateLimiting() {
	fmt.Println("\n=== Rate Limiting Example ===")

	// Create rate limiter
	rateLimitConfig := DefaultTokenBucketConfig()
	rateLimitConfig.DefaultCapacity = 5
	rateLimitConfig.DefaultRefill = 100 * time.Millisecond

	quotaConfig := DefaultQuotaConfig()
	quotaConfig.DefaultQuota = 100

	manager := NewRateLimitQuotaManager(rateLimitConfig, quotaConfig)

	userKey := "user123"

	// Simulate API calls
	fmt.Println("Making API calls with rate limiting...")

	for i := 0; i < 10; i++ {
		err := manager.UseRequest(userKey, 10) // Use 10 quota units per request
		if err != nil {
			fmt.Printf("Request %d: %v\n", i+1, err)
		} else {
			fmt.Printf("Request %d: Success\n", i+1)
		}

		// Small delay between requests
		time.Sleep(50 * time.Millisecond)
	}

	// Check stats
	rateLimitStats := manager.GetRateLimitStats(userKey)
	quotaInfo := manager.GetQuotaInfo(userKey)

	fmt.Printf("Rate limit stats: Allowed=%d, Denied=%d\n",
		rateLimitStats.RequestsAllowed, rateLimitStats.RequestsDenied)
	fmt.Printf("Quota info: Used=%d, Remaining=%d\n",
		quotaInfo.Used, quotaInfo.Remaining)

	fmt.Println("Rate limiting example completed!")
}

// Example 6: Testing with Mock Framework
func ExampleTesting() {
	fmt.Println("\n=== Testing Framework Example ===")

	// Create testing framework
	testFramework := NewTestingFramework()

	// Create mock stub
	mockStub := testFramework.CreateMockStub("TestService")

	// Set up mock responses
	mockStub.SetResponse("GetUser", map[string]interface{}{
		"id":   123,
		"name": "Test User",
	})

	mockStub.SetError("DeleteUser", fmt.Errorf("permission denied"))

	// Test the mock
	ctx := context.Background()

	// Test successful call
	promise, err := mockStub.Call(ctx, "GetUser", 123)
	if err != nil {
		fmt.Printf("Mock call failed: %v\n", err)
		return
	}

	result, err := promise.Await(ctx)
	if err != nil {
		fmt.Printf("Mock await failed: %v\n", err)
		return
	}

	fmt.Printf("Mock GetUser result: %+v\n", result)

	// Test error call
	_, err = mockStub.Call(ctx, "DeleteUser", 123)
	if err != nil {
		fmt.Printf("Mock DeleteUser error (expected): %v\n", err)
	}

	// Verify calls
	verifier := NewStubVerifier(mockStub, &testingContextImpl{})
	verifier.VerifyCallCount("GetUser", 1)
	verifier.VerifyCallCount("DeleteUser", 1)
	verifier.VerifyCalledWith("GetUser", 123)

	fmt.Printf("Total calls made: %d\n", len(mockStub.GetCalls()))

	fmt.Println("Testing framework example completed!")
}

// Example 7: Debugging and Tracing
func ExampleDebugging() {
	fmt.Println("\n=== Debugging and Tracing Example ===")

	// Create tracer
	options := DefaultTracingOptions()
	tracer := NewTracer(os.Stdout, options)

	// Start a trace
	trace := tracer.StartTrace("example-trace")
	if trace == nil {
		fmt.Println("Failed to start trace")
		return
	}

	// Create some spans
	span1 := trace.StartSpan(OperationTypeCall, "GetUser")
	span1.AddTag("user_id", 123)
	span1.LogEvent(TraceLevelInfo, "Starting user lookup", nil)

	// Simulate work
	time.Sleep(10 * time.Millisecond)

	span1.SetRequest(map[string]interface{}{"id": 123})
	span1.SetResponse(map[string]interface{}{"id": 123, "name": "John"})
	trace.FinishSpan(span1)

	// Another span
	span2 := trace.StartSpan(OperationTypeCall, "GetFriends")
	span2.AddTag("user_id", 123)
	span2.LogEvent(TraceLevelInfo, "Loading friends list", nil)

	time.Sleep(15 * time.Millisecond)

	span2.SetRequest(map[string]interface{}{"user_id": 123})
	span2.SetResponse([]map[string]interface{}{
		{"id": 456, "name": "Alice"},
		{"id": 789, "name": "Bob"},
	})
	trace.FinishSpan(span2)

	// End trace
	tracer.EndTrace(trace.ID)

	// Performance monitoring
	monitor := NewPerformanceMonitor(DefaultMonitoringOptions())

	// Record some method calls
	monitor.RecordCall("GetUser", 10*time.Millisecond, nil)
	monitor.RecordCall("GetUser", 15*time.Millisecond, nil)
	monitor.RecordCall("GetFriends", 25*time.Millisecond, nil)
	monitor.RecordCall("GetFriends", 30*time.Millisecond, fmt.Errorf("timeout"))

	// Get metrics summary
	summary := monitor.GetSummary()
	fmt.Printf("Performance Summary:\n")
	fmt.Printf("  Total Methods: %d\n", summary.MethodCount)
	fmt.Printf("  Total Calls: %d\n", summary.TotalCalls)
	fmt.Printf("  Error Rate: %.2f%%\n", summary.OverallErrorRate*100)

	for _, method := range summary.Methods {
		fmt.Printf("  %s: %d calls, %.2f%% errors, avg %v\n",
			method.MethodName, method.CallCount, method.ErrorRate*100, method.AverageDuration)
	}

	fmt.Println("Debugging and tracing example completed!")
}

// Example 8: Code Generation Usage
func ExampleCodeGenerationUsage() {
	fmt.Println("\n=== Code Generation Example ===")

	// This example shows how to use the code generator
	// In practice, this would be done as a build step

	// Define an interface for generation
	type APIService interface {
		CreateUser(ctx context.Context, name string, email string) (*User, error)
		GetUser(ctx context.Context, id int) (*User, error)
		UpdateUser(ctx context.Context, id int, updates map[string]interface{}) error
		DeleteUser(ctx context.Context, id int) error
		ListUsers(ctx context.Context, limit int, offset int) ([]*User, error)
	}

	// Register interface
	registry := NewInterfaceRegistry()
	_, err := registry.RegisterInterface((*APIService)(nil))
	if err != nil {
		fmt.Printf("Failed to register interface: %v\n", err)
		return
	}

	// Configure code generation
	options := DefaultCodeGenOptions()
	options.PackageName = "generated"
	options.GenerateComments = true
	options.GenerateValidation = true

	// Create generator
	generator := NewCodeGenerator(registry, options)
	_ = generator // Use the variable

	// Generate code
	fmt.Println("Generated stub code would be created here...")
	fmt.Println("In practice, use: capnweb-gen -input=api.go -output=api_stub.go")

	// Show example of generated stub usage:
	fmt.Println(`
Example generated code usage:

	// Create stub
	stub, err := NewAPIServiceStub(session, importID)
	if err != nil {
		return err
	}
	defer stub.Dispose()

	// Use type-safe methods
	user, err := stub.CreateUser(ctx, "John Doe", "john@example.com")
	if err != nil {
		return err
	}

	fmt.Printf("Created user: %+v\n", user)
	`)

	fmt.Println("Code generation example completed!")
}

// Helper type for testing
type testingContextImpl struct{}

func (t *testingContextImpl) Errorf(format string, args ...interface{}) {
	fmt.Printf("TEST ERROR: "+format+"\n", args...)
}

func (t *testingContextImpl) Fatalf(format string, args ...interface{}) {
	fmt.Printf("TEST FATAL: "+format+"\n", args...)
}

func (t *testingContextImpl) Helper() {
	// No-op for this example
}

// RunAllExamples runs all the examples
func RunAllExamples() {
	fmt.Println("Cap'n Web Go Library Examples")
	fmt.Println("=============================")

	ExampleBasicRPC()
	ExamplePromisePipelining()
	ExampleHTTPTransport()
	ExampleSecurity()
	ExampleRateLimiting()
	ExampleTesting()
	ExampleDebugging()
	ExampleCodeGenerationUsage()

	fmt.Println("\n=============================")
	fmt.Println("All examples completed!")
}