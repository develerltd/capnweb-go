package capnweb

import (
	"context"
	"errors"
	"testing"
)

// Test implementations for RpcTarget

type MockRpcTarget struct {
	disposed bool
}

func (t *MockRpcTarget) RpcMethods() []string {
	return []string{"TestMethod", "AnotherMethod"}
}

func (t *MockRpcTarget) TestMethod(arg string) string {
	return "result: " + arg
}

func (t *MockRpcTarget) AnotherMethod(x, y int) int {
	return x + y
}

func (t *MockRpcTarget) Dispose() error {
	t.disposed = true
	return nil
}

type UnrestrictedRpcTarget struct{}

func (u *UnrestrictedRpcTarget) RpcMethods() []string {
	return nil // Allow all methods
}

func (u *UnrestrictedRpcTarget) PublicMethod() string {
	return "public"
}

func (u *UnrestrictedRpcTarget) AnotherPublicMethod() string {
	return "another"
}

func TestRpcTarget(t *testing.T) {
	target := &MockRpcTarget{}

	// Test method restriction
	methods := target.RpcMethods()
	expectedMethods := []string{"TestMethod", "AnotherMethod"}

	if len(methods) != len(expectedMethods) {
		t.Errorf("Expected %d methods, got %d", len(expectedMethods), len(methods))
	}

	for i, method := range expectedMethods {
		if i >= len(methods) || methods[i] != method {
			t.Errorf("Expected method %s at index %d, got %s", method, i, methods[i])
		}
	}

	// Test disposal
	if target.disposed {
		t.Error("Target should not be disposed initially")
	}

	err := target.Dispose()
	if err != nil {
		t.Errorf("Dispose should not return error: %v", err)
	}

	if !target.disposed {
		t.Error("Target should be disposed after calling Dispose()")
	}
}

func TestUnrestrictedRpcTarget(t *testing.T) {
	target := &UnrestrictedRpcTarget{}

	methods := target.RpcMethods()
	if methods != nil {
		t.Error("Unrestricted target should return nil for RpcMethods()")
	}
}

func TestExportImportIDs(t *testing.T) {
	// Test bootstrap IDs
	if BootstrapExportID != 0 {
		t.Errorf("BootstrapExportID should be 0, got %d", BootstrapExportID)
	}

	if BootstrapImportID != 0 {
		t.Errorf("BootstrapImportID should be 0, got %d", BootstrapImportID)
	}

	// Test ID types
	var exportID ExportID = 42
	var importID ImportID = 24

	if int32(exportID) != 42 {
		t.Errorf("ExportID conversion failed: expected 42, got %d", exportID)
	}

	if int32(importID) != 24 {
		t.Errorf("ImportID conversion failed: expected 24, got %d", importID)
	}
}

func TestPropertyPath(t *testing.T) {
	// Test empty path
	var empty PropertyPath
	if !empty.IsEmpty() {
		t.Error("Empty path should return true for IsEmpty()")
	}

	if empty.String() != "(root)" {
		t.Errorf("Empty path string should be '(root)', got '%s'", empty.String())
	}

	// Test non-empty path
	path := PropertyPath{"user", "profile", "name"}
	if path.IsEmpty() {
		t.Error("Non-empty path should return false for IsEmpty()")
	}

	expected := "user.profile.name"
	if path.String() != expected {
		t.Errorf("Expected path string '%s', got '%s'", expected, path.String())
	}

	// Test append
	newPath := path.Append("firstName", "lastName")
	expectedNew := PropertyPath{"user", "profile", "name", "firstName", "lastName"}

	if len(newPath) != len(expectedNew) {
		t.Errorf("Expected path length %d, got %d", len(expectedNew), len(newPath))
	}

	for i, element := range expectedNew {
		if i >= len(newPath) || newPath[i] != element {
			t.Errorf("Expected element '%s' at index %d, got '%s'", element, i, newPath[i])
		}
	}

	// Original path should be unchanged
	if len(path) != 3 {
		t.Error("Original path should not be modified by Append()")
	}
}

func TestIsArrayIndex(t *testing.T) {
	tests := []struct {
		element  string
		expected bool
	}{
		{"0", true},
		{"1", true},
		{"42", true},
		{"123", true},
		{"", false},
		{"abc", false},
		{"1.5", false},
		{"-1", true}, // negative numbers are valid integers
		{"01", true}, // leading zeros are valid
	}

	for _, test := range tests {
		result := IsArrayIndex(test.element)
		if result != test.expected {
			t.Errorf("IsArrayIndex('%s') = %v, expected %v", test.element, result, test.expected)
		}
	}
}

func TestRpcError(t *testing.T) {
	// Test basic RpcError
	err := &RpcError{
		Type:    "TestError",
		Message: "test message",
		Code:    1001,
	}

	expected := "RPC error (TestError/1001): test message"
	if err.Error() != expected {
		t.Errorf("Expected error string '%s', got '%s'", expected, err.Error())
	}

	// Test RpcError without code
	err2 := &RpcError{
		Type:    "TestError",
		Message: "test message",
	}

	expected2 := "RPC error (TestError): test message"
	if err2.Error() != expected2 {
		t.Errorf("Expected error string '%s', got '%s'", expected2, err2.Error())
	}
}

func TestNewRpcError(t *testing.T) {
	// Test with regular error
	regularErr := errors.New("regular error")
	rpcErr := NewRpcError(regularErr)

	if rpcErr.Type != "error" {
		t.Errorf("Expected type 'error', got '%s'", rpcErr.Type)
	}

	if rpcErr.Message != "regular error" {
		t.Errorf("Expected message 'regular error', got '%s'", rpcErr.Message)
	}

	// Test with existing RpcError (should return same instance)
	existingRpcErr := &RpcError{Type: "existing", Message: "existing message"}
	result := NewRpcError(existingRpcErr)

	if result != existingRpcErr {
		t.Error("NewRpcError should return the same instance for existing RpcError")
	}
}

func TestCommonRpcErrors(t *testing.T) {
	errors := []*RpcError{
		ErrMethodNotFound,
		ErrInvalidArguments,
		ErrPermissionDenied,
		ErrTimeout,
		ErrCanceled,
		ErrInternalError,
		ErrSerializationError,
		ErrDisposed,
	}

	for _, err := range errors {
		if err.Type == "" {
			t.Errorf("Error %v should have a type", err)
		}
		if err.Message == "" {
			t.Errorf("Error %v should have a message", err)
		}
		if err.Code == 0 {
			t.Errorf("Error %v should have a non-zero code", err)
		}
	}
}

func TestWrapContextError(t *testing.T) {
	// Test nil error
	result := WrapContextError(nil)
	if result != nil {
		t.Error("WrapContextError(nil) should return nil")
	}

	// Test context.Canceled
	result = WrapContextError(context.Canceled)
	if result != ErrCanceled {
		t.Errorf("Expected ErrCanceled, got %v", result)
	}

	// Test context.DeadlineExceeded
	result = WrapContextError(context.DeadlineExceeded)
	if result != ErrTimeout {
		t.Errorf("Expected ErrTimeout, got %v", result)
	}

	// Test regular error (should pass through)
	regularErr := errors.New("regular error")
	result = WrapContextError(regularErr)
	if result != regularErr {
		t.Errorf("Regular error should pass through unchanged, got %v", result)
	}
}

func TestCallInfo(t *testing.T) {
	// Test with method call
	exportID := ExportID(42)
	path := PropertyPath{"user", "getName"}

	callInfo := NewCallInfo(exportID, path)

	if callInfo.ObjectID != exportID {
		t.Errorf("Expected ObjectID %v, got %v", exportID, callInfo.ObjectID)
	}

	if len(callInfo.Path) != len(path) {
		t.Errorf("Expected path length %d, got %d", len(path), len(callInfo.Path))
	}

	if callInfo.Method != "getName" {
		t.Errorf("Expected method 'getName', got '%s'", callInfo.Method)
	}

	if callInfo.IsProperty {
		t.Error("Method call should not be marked as property")
	}

	// Test string representation
	str := callInfo.String()
	expected := "export:42.user.getName() (method)"
	if str != expected {
		t.Errorf("Expected string '%s', got '%s'", expected, str)
	}

	// Test with import ID
	importID := ImportID(24)
	callInfo2 := NewCallInfo(importID, PropertyPath{"value"})

	str2 := callInfo2.String()
	if !contains(str2, "import:24") {
		t.Errorf("String should contain 'import:24', got '%s'", str2)
	}

	// Test with empty path
	callInfo3 := NewCallInfo(exportID, PropertyPath{})
	if callInfo3.Method != "" {
		t.Errorf("Empty path should result in empty method, got '%s'", callInfo3.Method)
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		   (len(substr) == 0 || indexOfString(s, substr) >= 0)
}

func indexOfString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}