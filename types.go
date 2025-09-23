package capnweb

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// RpcTarget is the interface that objects must implement to be passed by reference
// over RPC. Objects that implement this interface will be exported as stubs that
// can be called remotely.
//
// When an RpcTarget is passed in an RPC message, it is replaced with a stub
// pointing back to the original object. Method calls on the stub will result
// in RPC calls back to the original object.
type RpcTarget interface {
	// RpcMethods returns the list of method names that can be called over RPC.
	// If nil is returned, all exported methods of the object can be called.
	// This allows for fine-grained control over which methods are exposed.
	RpcMethods() []string
}

// Disposer is an optional interface that RpcTarget implementations can
// implement to be notified when all remote references to them have been
// disposed.
type Disposer interface {
	// Dispose is called when all remote stubs pointing to this object
	// have been disposed. This allows the object to clean up resources.
	Dispose() error
}

// ExportID identifies an object that has been exported from this session
// to the remote peer. Positive IDs are for exports initiated by this session,
// negative IDs are for exports initiated by the remote peer.
type ExportID int32

// ImportID identifies an object that has been imported from the remote peer
// to this session. Positive IDs are for imports initiated by this session,
// negative IDs are for imports initiated by the remote peer.
type ImportID int32

// BootstrapExportID is the export ID for the bootstrap object (main interface)
// that is automatically exported when a session starts.
const BootstrapExportID ExportID = 0

// BootstrapImportID is the import ID for the bootstrap object (main interface)
// from the remote peer.
const BootstrapImportID ImportID = 0

// PropertyPath represents a path to a property or method on an object.
// Each element can be either a string (for object properties) or an
// integer (for array indices).
//
// Examples:
//   - []string{"user", "profile", "name"} -> user.profile.name
//   - []string{"items", "0", "title"} -> items[0].title
type PropertyPath []string

// String returns a string representation of the property path.
func (p PropertyPath) String() string {
	if len(p) == 0 {
		return "(root)"
	}
	return strings.Join([]string(p), ".")
}

// Append creates a new PropertyPath with additional elements.
func (p PropertyPath) Append(elements ...string) PropertyPath {
	result := make(PropertyPath, len(p)+len(elements))
	copy(result, p)
	copy(result[len(p):], elements)
	return result
}

// IsEmpty returns true if the path is empty (points to root).
func (p PropertyPath) IsEmpty() bool {
	return len(p) == 0
}

// IsArrayIndex returns true if the given path element represents an array index.
func IsArrayIndex(element string) bool {
	if element == "" {
		return false
	}
	_, err := strconv.Atoi(element)
	return err == nil
}

// RPC-specific error types

// RpcError represents an error that occurred during RPC processing.
type RpcError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Stack   string `json:"stack,omitempty"`
	Code    int    `json:"code,omitempty"`
}

// Error implements the error interface.
func (e *RpcError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("RPC error (%s/%d): %s", e.Type, e.Code, e.Message)
	}
	return fmt.Sprintf("RPC error (%s): %s", e.Type, e.Message)
}

// NewRpcError creates a new RpcError from a regular error.
func NewRpcError(err error) *RpcError {
	if rpcErr, ok := err.(*RpcError); ok {
		return rpcErr
	}
	return &RpcError{
		Type:    "error",
		Message: err.Error(),
	}
}

// RPC error types and codes
const (
	ErrCodeMethodNotFound    = 1001
	ErrCodeInvalidArguments  = 1002
	ErrCodePermissionDenied  = 1003
	ErrCodeTimeout          = 1004
	ErrCodeCanceled         = 1005
	ErrCodeInternalError    = 1006
	ErrCodeSerializationError = 1007
	ErrCodeDisposed         = 1008
)

// Common RPC errors
var (
	// ErrMethodNotFound indicates the requested method does not exist
	ErrMethodNotFound = &RpcError{
		Type:    "MethodNotFound",
		Message: "the requested method does not exist",
		Code:    ErrCodeMethodNotFound,
	}

	// ErrInvalidArguments indicates the method arguments are invalid
	ErrInvalidArguments = &RpcError{
		Type:    "InvalidArguments",
		Message: "invalid method arguments",
		Code:    ErrCodeInvalidArguments,
	}

	// ErrPermissionDenied indicates access to the method/object is denied
	ErrPermissionDenied = &RpcError{
		Type:    "PermissionDenied",
		Message: "permission denied",
		Code:    ErrCodePermissionDenied,
	}

	// ErrTimeout indicates the RPC call timed out
	ErrTimeout = &RpcError{
		Type:    "Timeout",
		Message: "RPC call timed out",
		Code:    ErrCodeTimeout,
	}

	// ErrCanceled indicates the RPC call was canceled
	ErrCanceled = &RpcError{
		Type:    "Canceled",
		Message: "RPC call was canceled",
		Code:    ErrCodeCanceled,
	}

	// ErrInternalError indicates an internal RPC system error
	ErrInternalError = &RpcError{
		Type:    "InternalError",
		Message: "internal RPC error",
		Code:    ErrCodeInternalError,
	}

	// ErrSerializationError indicates a serialization/deserialization error
	ErrSerializationError = &RpcError{
		Type:    "SerializationError",
		Message: "serialization error",
		Code:    ErrCodeSerializationError,
	}

	// ErrDisposed indicates the object has been disposed
	ErrDisposed = &RpcError{
		Type:    "Disposed",
		Message: "object has been disposed",
		Code:    ErrCodeDisposed,
	}
)

// WrapContextError converts a context error to an appropriate RpcError.
func WrapContextError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) {
		return ErrCanceled
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return ErrTimeout
	}

	return err
}

// CallInfo contains information about an RPC call.
type CallInfo struct {
	// ObjectID is the export/import ID of the target object
	ObjectID interface{} // ExportID or ImportID

	// Path is the property path to the method being called
	Path PropertyPath

	// Method is the name of the method being called (last element of Path)
	Method string

	// IsProperty indicates if this is a property access rather than method call
	IsProperty bool
}

// NewCallInfo creates a CallInfo from an object ID and property path.
func NewCallInfo(objectID interface{}, path PropertyPath) *CallInfo {
	var method string
	isProperty := true

	if len(path) > 0 {
		method = path[len(path)-1]
		// Assume it's a method call for now - this can be refined later
		// based on reflection or interface information
		isProperty = false
	}

	return &CallInfo{
		ObjectID:   objectID,
		Path:       path,
		Method:     method,
		IsProperty: isProperty,
	}
}

// String returns a string representation of the call info.
func (c *CallInfo) String() string {
	var objectType string
	switch c.ObjectID.(type) {
	case ExportID:
		objectType = "export"
	case ImportID:
		objectType = "import"
	default:
		objectType = "unknown"
	}

	if c.IsProperty {
		return fmt.Sprintf("%s:%v.%s (property)", objectType, c.ObjectID, c.Path)
	}
	return fmt.Sprintf("%s:%v.%s() (method)", objectType, c.ObjectID, c.Path)
}

// Stub represents a reference to a remote object that can be called over RPC.
// This is a marker interface - actual stub implementations will embed this.
type Stub interface {
	// GetImportID returns the import ID of the remote object this stub points to.
	// Returns nil if this is a local stub.
	GetImportID() *ImportID

	// GetExportID returns the export ID if this stub wraps a local object.
	// Returns nil if this is a remote stub.
	GetExportID() *ExportID

	// Dispose releases the stub and notifies the remote peer that this
	// reference is no longer needed.
	Dispose() error

	// IsDisposed returns true if this stub has been disposed.
	IsDisposed() bool
}