package capnweb

import (
	"context"
	"fmt"
	"reflect"
	"strings"
)

// RpcService marks an interface as an RPC service
// Interfaces implementing this can be automatically registered and stub-generated
type RpcService interface {
	// ServiceName returns the name of this RPC service
	ServiceName() string
}

// RpcMethod represents metadata about an RPC method
type RpcMethod struct {
	// Name is the method name
	Name string

	// Type is the reflect.Method for the method
	Type reflect.Method

	// InputTypes are the parameter types (excluding receiver and context)
	InputTypes []reflect.Type

	// OutputType is the return type (excluding error)
	OutputType reflect.Type

	// HasError indicates if the method returns an error
	HasError bool

	// IsAsync indicates if the method should return a Promise
	IsAsync bool

	// Tags contain metadata from struct tags
	Tags RpcMethodTags
}

// RpcMethodTags contains parsed struct tag information
type RpcMethodTags struct {
	// Name is the RPC method name (default: method name)
	Name string

	// Async indicates the method should return a Promise
	Async bool

	// Timeout sets a custom timeout for this method
	Timeout string

	// Description provides method documentation
	Description string

	// Deprecated marks the method as deprecated
	Deprecated bool
}

// RpcInterface represents a parsed RPC interface definition
type RpcInterface struct {
	// Name is the interface name
	Name string

	// Type is the reflect.Type of the interface
	Type reflect.Type

	// Methods contains all RPC methods
	Methods map[string]*RpcMethod

	// ServiceName is the RPC service name
	ServiceName string

	// Package is the Go package name
	Package string

	// Tags contain interface-level metadata
	Tags RpcInterfaceTags
}

// RpcInterfaceTags contains interface-level metadata
type RpcInterfaceTags struct {
	// Service name override
	Service string

	// Version of the service
	Version string

	// Description of the service
	Description string

	// Deprecated marks the entire service as deprecated
	Deprecated bool
}

// InterfaceRegistry manages RPC interface definitions
type InterfaceRegistry struct {
	interfaces map[string]*RpcInterface
	types      map[reflect.Type]*RpcInterface
}

// NewInterfaceRegistry creates a new interface registry
func NewInterfaceRegistry() *InterfaceRegistry {
	return &InterfaceRegistry{
		interfaces: make(map[string]*RpcInterface),
		types:      make(map[reflect.Type]*RpcInterface),
	}
}

// RegisterInterface registers an RPC interface for code generation and validation
func (r *InterfaceRegistry) RegisterInterface(iface interface{}) (*RpcInterface, error) {
	t := reflect.TypeOf(iface)
	if t == nil {
		return nil, fmt.Errorf("interface cannot be nil")
	}

	// Handle pointer to interface
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Interface {
		return nil, fmt.Errorf("expected interface, got %s", t.Kind())
	}

	rpcIface, err := r.parseInterface(t)
	if err != nil {
		return nil, fmt.Errorf("failed to parse interface %s: %w", t.Name(), err)
	}

	r.interfaces[rpcIface.Name] = rpcIface
	r.types[t] = rpcIface

	return rpcIface, nil
}

// GetInterface retrieves a registered interface by name
func (r *InterfaceRegistry) GetInterface(name string) (*RpcInterface, bool) {
	iface, ok := r.interfaces[name]
	return iface, ok
}

// GetInterfaceByType retrieves a registered interface by reflection type
func (r *InterfaceRegistry) GetInterfaceByType(t reflect.Type) (*RpcInterface, bool) {
	iface, ok := r.types[t]
	return iface, ok
}

// ListInterfaces returns all registered interface names
func (r *InterfaceRegistry) ListInterfaces() []string {
	names := make([]string, 0, len(r.interfaces))
	for name := range r.interfaces {
		names = append(names, name)
	}
	return names
}

// parseInterface extracts RPC metadata from an interface type
func (r *InterfaceRegistry) parseInterface(t reflect.Type) (*RpcInterface, error) {
	iface := &RpcInterface{
		Name:    t.Name(),
		Type:    t,
		Methods: make(map[string]*RpcMethod),
		Package: t.PkgPath(),
	}

	// Parse interface-level tags if available
	// Note: Go interfaces don't have struct tags, but we can use comments or naming conventions
	iface.ServiceName = iface.Name
	if strings.HasSuffix(iface.Name, "API") {
		iface.ServiceName = strings.TrimSuffix(iface.Name, "API")
	} else if strings.HasSuffix(iface.Name, "Service") {
		iface.ServiceName = strings.TrimSuffix(iface.Name, "Service")
	}

	// Parse methods
	for i := 0; i < t.NumMethod(); i++ {
		method := t.Method(i)
		rpcMethod, err := r.parseMethod(method)
		if err != nil {
			return nil, fmt.Errorf("failed to parse method %s: %w", method.Name, err)
		}

		iface.Methods[method.Name] = rpcMethod
	}

	return iface, nil
}

// parseMethod extracts RPC metadata from a method
func (r *InterfaceRegistry) parseMethod(method reflect.Method) (*RpcMethod, error) {
	methodType := method.Type

	// Validate method signature
	if methodType.NumIn() < 1 {
		return nil, fmt.Errorf("method must have at least a receiver")
	}

	// Check for context parameter
	hasContext := false
	inputStart := 1 // Skip receiver
	if methodType.NumIn() > 1 {
		firstParam := methodType.In(1)
		if firstParam == reflect.TypeOf((*context.Context)(nil)).Elem() {
			hasContext = true
			inputStart = 2
		}
	}

	// Extract input types (excluding receiver and context)
	var inputTypes []reflect.Type
	for i := inputStart; i < methodType.NumIn(); i++ {
		inputTypes = append(inputTypes, methodType.In(i))
	}

	// Extract output types
	var outputType reflect.Type
	hasError := false

	if methodType.NumOut() == 0 {
		return nil, fmt.Errorf("method must return at least an error")
	}

	// Check if last return type is error
	lastOut := methodType.Out(methodType.NumOut() - 1)
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if lastOut.Implements(errorType) {
		hasError = true
		if methodType.NumOut() > 1 {
			outputType = methodType.Out(0)
		}
	} else if methodType.NumOut() == 1 {
		outputType = methodType.Out(0)
	} else {
		return nil, fmt.Errorf("method must return (result, error) or just error")
	}

	rpcMethod := &RpcMethod{
		Name:       method.Name,
		Type:       method,
		InputTypes: inputTypes,
		OutputType: outputType,
		HasError:   hasError,
		IsAsync:    true, // Default to async for RPC methods
		Tags: RpcMethodTags{
			Name: method.Name,
		},
	}

	// Apply naming conventions
	if !hasContext {
		// Methods without context.Context are considered synchronous (not recommended)
		rpcMethod.IsAsync = false
	}

	return rpcMethod, nil
}

// ValidateMethod checks if a method signature is valid for RPC
func (r *InterfaceRegistry) ValidateMethod(method reflect.Method) error {
	methodType := method.Type

	// Must have at least receiver
	if methodType.NumIn() < 1 {
		return fmt.Errorf("method must have a receiver")
	}

	// Recommend context.Context as first parameter
	if methodType.NumIn() < 2 {
		return fmt.Errorf("method should have context.Context as first parameter")
	}

	firstParam := methodType.In(1)
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	if !firstParam.Implements(contextType) {
		return fmt.Errorf("first parameter should be context.Context, got %s", firstParam)
	}

	// Must return error
	if methodType.NumOut() == 0 {
		return fmt.Errorf("method must return at least an error")
	}

	lastOut := methodType.Out(methodType.NumOut() - 1)
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if !lastOut.Implements(errorType) {
		return fmt.Errorf("last return value must be error, got %s", lastOut)
	}

	// Validate parameter types are serializable
	for i := 2; i < methodType.NumIn(); i++ {
		paramType := methodType.In(i)
		if err := r.validateSerializableType(paramType); err != nil {
			return fmt.Errorf("parameter %d is not serializable: %w", i-1, err)
		}
	}

	// Validate return type is serializable
	if methodType.NumOut() > 1 {
		returnType := methodType.Out(0)
		if err := r.validateSerializableType(returnType); err != nil {
			return fmt.Errorf("return type is not serializable: %w", err)
		}
	}

	return nil
}

// validateSerializableType checks if a type can be serialized for RPC
func (r *InterfaceRegistry) validateSerializableType(t reflect.Type) error {
	switch t.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		 reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		 reflect.Float32, reflect.Float64, reflect.String:
		return nil

	case reflect.Slice, reflect.Array:
		return r.validateSerializableType(t.Elem())

	case reflect.Map:
		if err := r.validateSerializableType(t.Key()); err != nil {
			return fmt.Errorf("map key: %w", err)
		}
		return r.validateSerializableType(t.Elem())

	case reflect.Ptr:
		return r.validateSerializableType(t.Elem())

	case reflect.Struct:
		// Check for special types
		if t == reflect.TypeOf((*context.Context)(nil)).Elem() {
			return fmt.Errorf("context.Context cannot be serialized")
		}
		// Allow all struct types - validation will happen at runtime
		return nil

	case reflect.Interface:
		// Allow interface{} and specific interfaces
		if t == reflect.TypeOf((*interface{})(nil)).Elem() {
			return nil
		}
		// Other interfaces should implement RpcTarget or be serializable
		return nil

	case reflect.Func:
		return fmt.Errorf("functions cannot be serialized (use RpcTarget interface)")

	case reflect.Chan:
		return fmt.Errorf("channels cannot be serialized")

	default:
		return fmt.Errorf("unsupported type: %s", t.Kind())
	}
}

// GenerateStubInterface creates a stub interface type for an RPC interface
func (r *InterfaceRegistry) GenerateStubInterface(iface *RpcInterface) string {
	var sb strings.Builder

	// Generate stub interface
	sb.WriteString(fmt.Sprintf("// %sStub provides RPC access to %s\n", iface.Name, iface.Name))
	sb.WriteString(fmt.Sprintf("type %sStub interface {\n", iface.Name))

	for _, method := range iface.Methods {
		sb.WriteString(r.generateStubMethod(method))
	}

	sb.WriteString("}\n\n")

	return sb.String()
}

// generateStubMethod generates a stub method signature
func (r *InterfaceRegistry) generateStubMethod(method *RpcMethod) string {
	var sb strings.Builder

	// Method signature for stub
	sb.WriteString(fmt.Sprintf("\t// %s calls the remote %s method\n", method.Name, method.Name))
	sb.WriteString(fmt.Sprintf("\t%s(ctx context.Context", method.Name))

	// Add parameters
	for i, paramType := range method.InputTypes {
		sb.WriteString(fmt.Sprintf(", arg%d %s", i, paramType.String()))
	}

	sb.WriteString(")")

	// Return type
	if method.OutputType != nil {
		if method.IsAsync {
			sb.WriteString(fmt.Sprintf(" (*Promise[%s], error)\n", method.OutputType.String()))
		} else {
			sb.WriteString(fmt.Sprintf(" (%s, error)\n", method.OutputType.String()))
		}
	} else {
		if method.IsAsync {
			sb.WriteString(" (*Promise[any], error)\n")
		} else {
			sb.WriteString(" error\n")
		}
	}

	return sb.String()
}

// DefaultRegistry is the global interface registry
var DefaultRegistry = NewInterfaceRegistry()

// RegisterInterface registers an interface in the default registry
func RegisterInterface(iface interface{}) (*RpcInterface, error) {
	return DefaultRegistry.RegisterInterface(iface)
}

// GetInterface retrieves an interface from the default registry
func GetInterface(name string) (*RpcInterface, bool) {
	return DefaultRegistry.GetInterface(name)
}