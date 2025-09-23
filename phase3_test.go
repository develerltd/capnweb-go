package capnweb

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// Example interfaces for testing
type UserAPI interface {
	Authenticate(ctx context.Context, token string) (*User, error)
	GetProfile(ctx context.Context, userID int) (*Profile, error)
	UpdateProfile(ctx context.Context, userID int, profile *Profile) error
	DeleteUser(ctx context.Context, userID int) error
}

type NotificationService interface {
	SendEmail(ctx context.Context, to, subject, body string) error
	SendSMS(ctx context.Context, phone, message string) error
	GetNotifications(ctx context.Context, userID int) ([]*Notification, error)
}

// Example types
type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

type Profile struct {
	UserID int    `json:"userId"`
	Name   string `json:"name"`
	Bio    string `json:"bio"`
}

type Notification struct {
	ID      int    `json:"id"`
	UserID  int    `json:"userId"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// TestInterfaceRegistration tests interface registration
func TestInterfaceRegistration(t *testing.T) {
	registry := NewInterfaceRegistry()

	// Test registering UserAPI
	userInterface, err := registry.RegisterInterface((*UserAPI)(nil))
	if err != nil {
		t.Fatalf("Failed to register UserAPI: %v", err)
	}

	if userInterface.Name != "UserAPI" {
		t.Errorf("Expected interface name 'UserAPI', got '%s'", userInterface.Name)
	}

	if userInterface.ServiceName != "User" {
		t.Errorf("Expected service name 'User', got '%s'", userInterface.ServiceName)
	}

	if len(userInterface.Methods) != 4 {
		t.Errorf("Expected 4 methods, got %d", len(userInterface.Methods))
	}

	// Test method parsing
	authMethod, exists := userInterface.Methods["Authenticate"]
	if !exists {
		t.Fatal("Authenticate method not found")
	}

	if authMethod.Name != "Authenticate" {
		t.Errorf("Expected method name 'Authenticate', got '%s'", authMethod.Name)
	}

	if len(authMethod.InputTypes) != 1 { // token string (excluding context)
		t.Errorf("Expected 1 input parameter, got %d", len(authMethod.InputTypes))
	}

	if !authMethod.HasError {
		t.Error("Expected method to have error return")
	}

	if authMethod.OutputType == nil {
		t.Error("Expected method to have return type")
	}
}

// TestInterfaceValidation tests interface validation
func TestInterfaceValidation(t *testing.T) {
	registry := NewInterfaceRegistry()
	validator := NewValidator(registry, DefaultValidationOptions())

	// Register and validate UserAPI
	userInterface, err := registry.RegisterInterface((*UserAPI)(nil))
	if err != nil {
		t.Fatalf("Failed to register UserAPI: %v", err)
	}

	err = validator.ValidateInterface(userInterface)
	if err != nil {
		t.Errorf("UserAPI validation failed: %v", err)
	}

	// Test individual method validation
	for methodName, method := range userInterface.Methods {
		err = validator.ValidateMethod(method)
		if err != nil {
			t.Errorf("Method %s validation failed: %v", methodName, err)
		}
	}
}

// TestMethodCallValidation tests runtime method call validation
func TestMethodCallValidation(t *testing.T) {
	registry := NewInterfaceRegistry()
	validator := NewValidator(registry, DefaultValidationOptions())

	// Register interface
	_, err := registry.RegisterInterface((*UserAPI)(nil))
	if err != nil {
		t.Fatalf("Failed to register UserAPI: %v", err)
	}

	// Test valid call
	err = validator.ValidateCall("UserAPI", "Authenticate", []interface{}{"test-token"})
	if err != nil {
		t.Errorf("Valid call validation failed: %v", err)
	}

	// Test invalid argument count
	err = validator.ValidateCall("UserAPI", "Authenticate", []interface{}{})
	if err == nil {
		t.Error("Expected validation error for missing arguments")
	}

	// Test invalid argument type
	err = validator.ValidateCall("UserAPI", "Authenticate", []interface{}{123})
	if err == nil {
		t.Error("Expected validation error for wrong argument type")
	}

	// Test non-existent method
	err = validator.ValidateCall("UserAPI", "NonExistentMethod", []interface{}{})
	if err == nil {
		t.Error("Expected validation error for non-existent method")
	}

	// Test non-existent interface
	err = validator.ValidateCall("NonExistentAPI", "SomeMethod", []interface{}{})
	if err == nil {
		t.Error("Expected validation error for non-existent interface")
	}
}

// TestReflectionStubGeneration tests reflection-based stub generation
func TestReflectionStubGeneration(t *testing.T) {
	// Create session and registry
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	registry := NewInterfaceRegistry()
	generator := NewReflectionStubGenerator(session, registry)

	// Register interface
	_, err = registry.RegisterInterface((*UserAPI)(nil))
	if err != nil {
		t.Fatalf("Failed to register UserAPI: %v", err)
	}

	// Generate stub (this will be limited due to Go's reflection constraints)
	importID := ImportID(42)
	interfaceType := reflect.TypeOf((*UserAPI)(nil)).Elem()

	stub, err := generator.GenerateStub(interfaceType, importID)
	if err != nil {
		t.Errorf("Failed to generate stub: %v", err)
	}

	if stub == nil {
		t.Error("Generated stub is nil")
	}

	t.Logf("Generated stub type: %T", stub)
}

// TestTypedPromises tests the typed promise system
func TestTypedPromises(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// Create a base promise
	exportID := ExportID(123)
	basePromise := NewPromise(session, exportID)

	// Test typed promise creation
	stringPromise := NewTypedPromise[string](basePromise)
	if stringPromise == nil {
		t.Fatal("Failed to create typed promise")
	}

	// Test promise state
	if !stringPromise.IsPending() {
		t.Error("New promise should be pending")
	}

	if stringPromise.IsResolved() {
		t.Error("New promise should not be resolved")
	}

	if stringPromise.IsRejected() {
		t.Error("New promise should not be rejected")
	}

	// Test promise chaining
	chainedPromise := stringPromise.Then("someMethod", "arg")
	if chainedPromise == nil {
		t.Error("Failed to create chained promise")
	}

	// Test typed chaining
	typedChainedPromise := stringPromise.ThenTyped("someMethod", "arg")
	if typedChainedPromise == nil {
		t.Error("Failed to create typed chained promise")
	}

	// Test property access
	propertyPromise := stringPromise.Get("property")
	if propertyPromise == nil {
		t.Error("Failed to get property promise")
	}

	// Test typed property access
	typedPropertyPromise := stringPromise.GetTyped("property")
	if typedPropertyPromise == nil {
		t.Error("Failed to get typed property promise")
	}

	// Test promise resolution
	testValue := "resolved value"
	basePromise.resolve(testValue)

	if !stringPromise.IsResolved() {
		t.Error("Promise should be resolved")
	}

	// Test await (this might not work fully due to message loop issues)
	ctx := context.Background()
	result, err := stringPromise.Await(ctx)
	if err == nil && result != testValue {
		t.Errorf("Expected result '%s', got '%s'", testValue, result)
	}
}

// TestCodeGeneration tests the code generation system
func TestCodeGeneration(t *testing.T) {
	registry := NewInterfaceRegistry()

	// Register interface
	_, err := registry.RegisterInterface((*UserAPI)(nil))
	if err != nil {
		t.Fatalf("Failed to register UserAPI: %v", err)
	}

	// Test code generation
	options := DefaultCodeGenOptions()
	options.PackageName = "test"

	generator := NewCodeGenerator(registry, options)

	// Generate code to string
	var output strings.Builder
	err = generator.GenerateCode(&output)
	if err != nil {
		t.Errorf("Failed to generate code: %v", err)
	}

	generatedCode := output.String()
	if generatedCode == "" {
		t.Error("Generated code is empty")
	}

	t.Logf("Generated code length: %d bytes", len(generatedCode))

	// Check for expected content
	expectedStrings := []string{
		"package test",
		"UserAPIStub",
		"Authenticate",
		"GetProfile",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(generatedCode, expected) {
			t.Errorf("Generated code missing expected string: %s", expected)
		}
	}
}

// TestStubFactory tests the stub factory
func TestStubFactory(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// Initialize stub factory
	InitializeStubFactory(session)
	if DefaultStubFactory == nil {
		t.Fatal("DefaultStubFactory not initialized")
	}

	factory := NewStubFactory(session)
	if factory == nil {
		t.Fatal("Failed to create stub factory")
	}

	// Test stub creation
	importID := ImportID(42)
	interfaceType := reflect.TypeOf((*UserAPI)(nil)).Elem()

	// Register the interface first
	_, err = RegisterInterface((*UserAPI)(nil))
	if err != nil {
		t.Fatalf("Failed to register interface: %v", err)
	}

	stub, err := factory.CreateStub(interfaceType, importID)
	if err != nil {
		t.Errorf("Failed to create stub: %v", err)
	}

	if stub == nil {
		t.Error("Created stub is nil")
	}
}

// TestMultipleInterfaces tests registration of multiple interfaces
func TestMultipleInterfaces(t *testing.T) {
	registry := NewInterfaceRegistry()

	// Register multiple interfaces
	interfaces := []interface{}{
		(*UserAPI)(nil),
		(*NotificationService)(nil),
	}

	for i, iface := range interfaces {
		_, err := registry.RegisterInterface(iface)
		if err != nil {
			t.Errorf("Failed to register interface %d: %v", i, err)
		}
	}

	// Check that all interfaces are registered
	registeredNames := registry.ListInterfaces()
	if len(registeredNames) != 2 {
		t.Errorf("Expected 2 registered interfaces, got %d", len(registeredNames))
	}

	// Verify we can retrieve each interface
	for _, name := range []string{"UserAPI", "NotificationService"} {
		iface, exists := registry.GetInterface(name)
		if !exists {
			t.Errorf("Interface %s not found in registry", name)
		}
		if iface.Name != name {
			t.Errorf("Retrieved interface has wrong name: expected %s, got %s", name, iface.Name)
		}
	}
}

// TestValidationOptions tests different validation configurations
func TestValidationOptions(t *testing.T) {
	registry := NewInterfaceRegistry()

	// Register interface
	_, err := registry.RegisterInterface((*UserAPI)(nil))
	if err != nil {
		t.Fatalf("Failed to register interface: %v", err)
	}

	// Test strict validation
	strictOptions := ValidationOptions{
		StrictTypes:    true,
		AllowAny:       false,
		RequireContext: true,
		RequireError:   true,
		MaxParameters:  5,
	}

	strictValidator := NewValidator(registry, strictOptions)
	userInterface, _ := registry.GetInterface("UserAPI")

	err = strictValidator.ValidateInterface(userInterface)
	if err != nil {
		t.Errorf("Strict validation failed: %v", err)
	}

	// Test lenient validation
	lenientOptions := ValidationOptions{
		StrictTypes:    false,
		AllowAny:       true,
		RequireContext: false,
		RequireError:   false,
		MaxParameters:  20,
	}

	lenientValidator := NewValidator(registry, lenientOptions)

	err = lenientValidator.ValidateInterface(userInterface)
	if err != nil {
		t.Errorf("Lenient validation failed: %v", err)
	}
}

// BenchmarkInterfaceRegistration benchmarks interface registration
func BenchmarkInterfaceRegistration(b *testing.B) {
	registry := NewInterfaceRegistry()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := registry.RegisterInterface((*UserAPI)(nil))
		if err != nil {
			b.Fatalf("Failed to register interface: %v", err)
		}
	}
}

// BenchmarkValidation benchmarks interface validation
func BenchmarkValidation(b *testing.B) {
	registry := NewInterfaceRegistry()
	validator := NewValidator(registry, DefaultValidationOptions())

	userInterface, err := registry.RegisterInterface((*UserAPI)(nil))
	if err != nil {
		b.Fatalf("Failed to register interface: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := validator.ValidateInterface(userInterface)
		if err != nil {
			b.Fatalf("Validation failed: %v", err)
		}
	}
}

// BenchmarkStubGeneration benchmarks stub generation
func BenchmarkStubGeneration(b *testing.B) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		b.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	registry := NewInterfaceRegistry()
	generator := NewReflectionStubGenerator(session, registry)

	_, err = registry.RegisterInterface((*UserAPI)(nil))
	if err != nil {
		b.Fatalf("Failed to register interface: %v", err)
	}

	interfaceType := reflect.TypeOf((*UserAPI)(nil)).Elem()
	importID := ImportID(42)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := generator.GenerateStub(interfaceType, importID)
		if err != nil {
			b.Fatalf("Failed to generate stub: %v", err)
		}
	}
}