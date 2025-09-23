package capnweb

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"
)

// ReflectionStubGenerator creates type-safe stubs using reflection at runtime
type ReflectionStubGenerator struct {
	registry *InterfaceRegistry
	session  *Session
	cache    map[reflect.Type]reflect.Value
	mutex    sync.RWMutex
}

// NewReflectionStubGenerator creates a new reflection-based stub generator
func NewReflectionStubGenerator(session *Session, registry *InterfaceRegistry) *ReflectionStubGenerator {
	if registry == nil {
		registry = DefaultRegistry
	}

	return &ReflectionStubGenerator{
		registry: registry,
		session:  session,
		cache:    make(map[reflect.Type]reflect.Value),
	}
}

// GenerateStub creates a type-safe stub for the given interface type
func (g *ReflectionStubGenerator) GenerateStub(interfaceType reflect.Type, importID ImportID) (interface{}, error) {
	// Check cache first
	g.mutex.RLock()
	if cached, exists := g.cache[interfaceType]; exists {
		g.mutex.RUnlock()
		return cached.Interface(), nil
	}
	g.mutex.RUnlock()

	// Get interface definition
	rpcInterface, exists := g.registry.GetInterfaceByType(interfaceType)
	if !exists {
		return nil, fmt.Errorf("interface %s not registered", interfaceType.Name())
	}

	// Create struct type for the stub
	stubType := g.createStubType(rpcInterface)

	// Create stub instance
	stubValue := reflect.New(stubType).Elem()

	// Set base stub
	baseStub := newStub(g.session, &importID, nil, PropertyPath{})
	stubValue.FieldByName("BaseStub").Set(reflect.ValueOf(baseStub))

	// Create wrapper that implements the interface
	wrapper := g.createWrapper(rpcInterface, stubValue)

	// Cache the result
	g.mutex.Lock()
	g.cache[interfaceType] = wrapper
	g.mutex.Unlock()

	return wrapper.Interface(), nil
}

// GenerateStubFor creates a stub for a specific interface type
func GenerateStubFor[T any](generator *ReflectionStubGenerator, importID ImportID) (T, error) {
	var zero T
	interfaceType := reflect.TypeOf((*T)(nil)).Elem()

	stub, err := generator.GenerateStub(interfaceType, importID)
	if err != nil {
		return zero, err
	}

	typedStub, ok := stub.(T)
	if !ok {
		return zero, fmt.Errorf("generated stub does not implement interface %T", zero)
	}

	return typedStub, nil
}

// createStubType creates a struct type for the stub
func (g *ReflectionStubGenerator) createStubType(rpcInterface *RpcInterface) reflect.Type {
	fields := []reflect.StructField{
		{
			Name: "BaseStub",
			Type: reflect.TypeOf((*stubImpl)(nil)),
		},
		{
			Name: "Session",
			Type: reflect.TypeOf((*Session)(nil)),
		},
		{
			Name: "ImportID",
			Type: reflect.TypeOf(ImportID(0)),
		},
	}

	return reflect.StructOf(fields)
}

// createWrapper creates a wrapper that implements the interface methods
func (g *ReflectionStubGenerator) createWrapper(rpcInterface *RpcInterface, stubValue reflect.Value) reflect.Value {
	interfaceType := rpcInterface.Type

	// Create method implementations
	methods := make([]reflect.Value, interfaceType.NumMethod())
	for i := 0; i < interfaceType.NumMethod(); i++ {
		method := interfaceType.Method(i)
		rpcMethod := rpcInterface.Methods[method.Name]
		methods[i] = g.createMethodImpl(rpcMethod, stubValue)
	}

	// Create wrapper value
	wrapperType := reflect.FuncOf([]reflect.Type{interfaceType}, []reflect.Type{interfaceType}, false)
	wrapper := reflect.MakeFunc(wrapperType, func(args []reflect.Value) []reflect.Value {
		// This is a placeholder - in a real implementation, we'd need more complex
		// proxy creation using reflect.New and method assignment
		return []reflect.Value{stubValue}
	})

	return wrapper
}

// createMethodImpl creates a method implementation that performs RPC calls
func (g *ReflectionStubGenerator) createMethodImpl(method *RpcMethod, stubValue reflect.Value) reflect.Value {
	methodType := method.Type.Type

	return reflect.MakeFunc(methodType, func(args []reflect.Value) []reflect.Value {
		// Extract arguments (skip receiver)
		callArgs := make([]interface{}, 0, len(args)-1)

		ctxIndex := -1
		for i := 1; i < len(args); i++ {
			arg := args[i]

			// Check if this is the context argument
			if arg.Type().Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) {
				ctxIndex = i
			} else {
				callArgs = append(callArgs, arg.Interface())
			}
		}

		// Get context
		var ctx context.Context
		if ctxIndex >= 0 {
			ctx = args[ctxIndex].Interface().(context.Context)
		} else {
			ctx = context.Background()
		}

		// Get base stub
		baseStub := stubValue.FieldByName("BaseStub").Interface().(*stubImpl)

		// Make RPC call
		promise, err := baseStub.Call(ctx, method.Name, callArgs...)

		// Handle return values
		results := make([]reflect.Value, methodType.NumOut())

		if err != nil {
			// Return error
			if method.HasError {
				if method.OutputType != nil {
					results[0] = reflect.Zero(method.OutputType)
					results[1] = reflect.ValueOf(err)
				} else {
					results[0] = reflect.ValueOf(err)
				}
			}
		} else {
			// Return promise or result
			if method.IsAsync && method.OutputType != nil {
				// Return typed promise
				typedPromise := g.wrapPromise(promise, method.OutputType)
				results[0] = reflect.ValueOf(typedPromise)
				if method.HasError {
					results[1] = reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())
				}
			} else if method.OutputType != nil {
				// For non-async methods, await the promise immediately
				result, err := promise.Await(ctx)
				if err != nil {
					results[0] = reflect.Zero(method.OutputType)
					if method.HasError {
						results[1] = reflect.ValueOf(err)
					}
				} else {
					results[0] = reflect.ValueOf(result)
					if method.HasError {
						results[1] = reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())
					}
				}
			} else {
				// Void method
				if method.HasError {
					results[0] = reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())
				}
			}
		}

		return results
	})
}

// wrapPromise wraps a generic promise with type information
func (g *ReflectionStubGenerator) wrapPromise(promise *Promise, resultType reflect.Type) interface{} {
	// In a full implementation, this would create a typed promise wrapper
	// For now, return the generic promise
	return promise
}

// TypedPromise is a generic promise with compile-time type safety
type TypedPromise[T any] struct {
	promise *Promise
	session *Session
}

// NewTypedPromise creates a new typed promise wrapper
func NewTypedPromise[T any](promise *Promise) *TypedPromise[T] {
	return &TypedPromise[T]{
		promise: promise,
		session: promise.session,
	}
}

// Await waits for the promise to resolve and returns the typed result
func (tp *TypedPromise[T]) Await(ctx context.Context) (T, error) {
	var zero T

	result, err := tp.promise.Await(ctx)
	if err != nil {
		return zero, err
	}

	// Type assertion with better error handling
	typedResult, ok := result.(T)
	if !ok {
		// Try to handle common type conversions
		if converted, success := tp.tryTypeConversion(result); success {
			return converted, nil
		}
		return zero, fmt.Errorf("result type mismatch: expected %T, got %T", zero, result)
	}

	return typedResult, nil
}

// tryTypeConversion attempts common type conversions
func (tp *TypedPromise[T]) tryTypeConversion(result interface{}) (T, bool) {
	var zero T

	// For now, only handle direct type assertion
	// In a full implementation, this could handle numeric conversions,
	// string conversions, etc.
	if converted, ok := result.(T); ok {
		return converted, true
	}

	return zero, false
}

// Then creates a chained promise with method call
func (tp *TypedPromise[T]) Then(method string, args ...interface{}) *Promise {
	return tp.promise.Then(method, args...)
}

// ThenTyped creates a chained typed promise
func (tp *TypedPromise[T]) ThenTyped(method string, args ...interface{}) *TypedPromise[T] {
	chainedPromise := tp.promise.Then(method, args...)
	return NewTypedPromise[T](chainedPromise)
}

// Get accesses a property on the promised value
func (tp *TypedPromise[T]) Get(property string) *Promise {
	return tp.promise.Get(property)
}

// GetTyped accesses a property and returns a typed promise
func (tp *TypedPromise[T]) GetTyped(property string) *TypedPromise[T] {
	propertyPromise := tp.promise.Get(property)
	return NewTypedPromise[T](propertyPromise)
}

// IsResolved returns true if the promise has been resolved
func (tp *TypedPromise[T]) IsResolved() bool {
	return tp.promise.IsResolved()
}

// IsPending returns true if the promise is still pending
func (tp *TypedPromise[T]) IsPending() bool {
	return tp.promise.IsPending()
}

// IsRejected returns true if the promise was rejected
func (tp *TypedPromise[T]) IsRejected() bool {
	return tp.promise.IsRejected()
}

// SetTimeout sets the timeout for this promise
func (tp *TypedPromise[T]) SetTimeout(timeout time.Duration) *TypedPromise[T] {
	tp.promise.SetTimeout(timeout)
	return tp
}

// GetTimeout returns the current timeout
func (tp *TypedPromise[T]) GetTimeout() time.Duration {
	return tp.promise.GetTimeout()
}

// GetExportID returns the export ID
func (tp *TypedPromise[T]) GetExportID() ExportID {
	return tp.promise.GetExportID()
}

// GetPath returns the property path
func (tp *TypedPromise[T]) GetPath() PropertyPath {
	return tp.promise.GetPath()
}

// String returns a string representation
func (tp *TypedPromise[T]) String() string {
	var zero T
	return fmt.Sprintf("TypedPromise[%T]%s", zero, tp.promise.String()[7:]) // Remove "Promise" prefix
}

// StubFactory creates type-safe stubs for registered interfaces
type StubFactory struct {
	generator *ReflectionStubGenerator
}

// NewStubFactory creates a new stub factory
func NewStubFactory(session *Session) *StubFactory {
	generator := NewReflectionStubGenerator(session, DefaultRegistry)
	return &StubFactory{generator: generator}
}

// CreateStub creates a type-safe stub for the given interface
func (f *StubFactory) CreateStub(interfaceType reflect.Type, importID ImportID) (interface{}, error) {
	return f.generator.GenerateStub(interfaceType, importID)
}

// CreateStubFor creates a typed stub using generics
func CreateStubFor[T any](factory *StubFactory, importID ImportID) (T, error) {
	return GenerateStubFor[T](factory.generator, importID)
}

// RuntimeValidator validates method calls at runtime
type RuntimeValidator struct {
	registry *InterfaceRegistry
}

// NewRuntimeValidator creates a new runtime validator
func NewRuntimeValidator(registry *InterfaceRegistry) *RuntimeValidator {
	if registry == nil {
		registry = DefaultRegistry
	}
	return &RuntimeValidator{registry: registry}
}

// ValidateCall validates a method call against the registered interface
func (v *RuntimeValidator) ValidateCall(interfaceName, methodName string, args []interface{}) error {
	rpcInterface, exists := v.registry.GetInterface(interfaceName)
	if !exists {
		return fmt.Errorf("interface %s not registered", interfaceName)
	}

	method, exists := rpcInterface.Methods[methodName]
	if !exists {
		return fmt.Errorf("method %s not found in interface %s", methodName, interfaceName)
	}

	// Validate argument count
	if len(args) != len(method.InputTypes) {
		return fmt.Errorf("method %s expects %d arguments, got %d",
			methodName, len(method.InputTypes), len(args))
	}

	// Validate argument types
	for i, arg := range args {
		expectedType := method.InputTypes[i]
		actualType := reflect.TypeOf(arg)

		if !actualType.AssignableTo(expectedType) {
			return fmt.Errorf("argument %d: expected %s, got %s",
				i, expectedType, actualType)
		}
	}

	return nil
}

// DefaultStubFactory is the global stub factory
var DefaultStubFactory *StubFactory

// InitializeStubFactory initializes the global stub factory
func InitializeStubFactory(session *Session) {
	DefaultStubFactory = NewStubFactory(session)
}