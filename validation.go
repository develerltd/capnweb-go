package capnweb

import (
	"fmt"
	"reflect"
	"strings"
)

// Validator provides method signature and type validation for RPC interfaces
type Validator struct {
	registry *InterfaceRegistry
	options  ValidationOptions
}

// ValidationOptions configures validation behavior
type ValidationOptions struct {
	// StrictTypes requires exact type matching
	StrictTypes bool

	// AllowAny permits interface{} parameters
	AllowAny bool

	// RequireContext requires context.Context as first parameter
	RequireContext bool

	// RequireError requires error as return value
	RequireError bool

	// MaxParameters limits the number of method parameters
	MaxParameters int

	// ForbiddenTypes lists types that cannot be used in RPC
	ForbiddenTypes []reflect.Type
}

// DefaultValidationOptions returns strict validation defaults
func DefaultValidationOptions() ValidationOptions {
	return ValidationOptions{
		StrictTypes:    true,
		AllowAny:       true,
		RequireContext: true,
		RequireError:   true,
		MaxParameters:  10,
		ForbiddenTypes: []reflect.Type{
			reflect.TypeOf(make(chan int)),        // Channels
			reflect.TypeOf(func() {}),             // Functions (unless RpcTarget)
			reflect.TypeOf(make(map[interface{}]interface{})), // Maps with interface{} keys
		},
	}
}

// NewValidator creates a new validator with the given options
func NewValidator(registry *InterfaceRegistry, options ValidationOptions) *Validator {
	if registry == nil {
		registry = DefaultRegistry
	}

	return &Validator{
		registry: registry,
		options:  options,
	}
}

// ValidateInterface validates an entire RPC interface
func (v *Validator) ValidateInterface(iface *RpcInterface) error {
	if iface == nil {
		return fmt.Errorf("interface cannot be nil")
	}

	// Validate interface name
	if err := v.validateInterfaceName(iface.Name); err != nil {
		return fmt.Errorf("interface name validation failed: %w", err)
	}

	// Validate each method
	for methodName, method := range iface.Methods {
		if err := v.ValidateMethod(method); err != nil {
			return fmt.Errorf("method %s validation failed: %w", methodName, err)
		}
	}

	// Check for naming conflicts
	if err := v.checkNamingConflicts(iface); err != nil {
		return fmt.Errorf("naming conflict in interface %s: %w", iface.Name, err)
	}

	return nil
}

// ValidateMethod validates a single RPC method
func (v *Validator) ValidateMethod(method *RpcMethod) error {
	if method == nil {
		return fmt.Errorf("method cannot be nil")
	}

	// Validate method name
	if err := v.validateMethodName(method.Name); err != nil {
		return fmt.Errorf("method name validation failed: %w", err)
	}

	// Validate parameter count
	if len(method.InputTypes) > v.options.MaxParameters {
		return fmt.Errorf("method %s has too many parameters: %d (max %d)",
			method.Name, len(method.InputTypes), v.options.MaxParameters)
	}

	// Validate context requirement
	if v.options.RequireContext {
		if err := v.validateContextParameter(method); err != nil {
			return fmt.Errorf("context validation failed: %w", err)
		}
	}

	// Validate error requirement
	if v.options.RequireError && !method.HasError {
		return fmt.Errorf("method %s must return error", method.Name)
	}

	// Validate parameter types
	for i, paramType := range method.InputTypes {
		if err := v.validateParameterType(paramType, fmt.Sprintf("parameter %d", i)); err != nil {
			return fmt.Errorf("method %s parameter %d validation failed: %w", method.Name, i, err)
		}
	}

	// Validate return type
	if method.OutputType != nil {
		if err := v.validateReturnType(method.OutputType); err != nil {
			return fmt.Errorf("method %s return type validation failed: %w", method.Name, err)
		}
	}

	// Validate method signature consistency
	if err := v.validateMethodSignature(method); err != nil {
		return fmt.Errorf("method %s signature validation failed: %w", method.Name, err)
	}

	return nil
}

// ValidateCall validates a runtime method call
func (v *Validator) ValidateCall(interfaceName, methodName string, args []interface{}) error {
	iface, exists := v.registry.GetInterface(interfaceName)
	if !exists {
		return fmt.Errorf("interface %s not registered", interfaceName)
	}

	method, exists := iface.Methods[methodName]
	if !exists {
		return fmt.Errorf("method %s not found in interface %s", methodName, interfaceName)
	}

	// Validate argument count
	expectedCount := len(method.InputTypes)
	if len(args) != expectedCount {
		return fmt.Errorf("method %s expects %d arguments, got %d",
			methodName, expectedCount, len(args))
	}

	// Validate argument types
	for i, arg := range args {
		expectedType := method.InputTypes[i]
		if err := v.validateArgumentType(arg, expectedType, i); err != nil {
			return fmt.Errorf("method %s argument %d: %w", methodName, i, err)
		}
	}

	return nil
}

// validateInterfaceName validates interface naming conventions
func (v *Validator) validateInterfaceName(name string) error {
	if name == "" {
		return fmt.Errorf("interface name cannot be empty")
	}

	if !isValidIdentifier(name) {
		return fmt.Errorf("interface name %q is not a valid Go identifier", name)
	}

	if !isExported(name) {
		return fmt.Errorf("interface name %q must be exported (start with uppercase letter)", name)
	}

	// Check naming conventions
	validSuffixes := []string{"API", "Service", "Client", "Server"}
	hasValidSuffix := false
	for _, suffix := range validSuffixes {
		if strings.HasSuffix(name, suffix) {
			hasValidSuffix = true
			break
		}
	}

	if !hasValidSuffix {
		return fmt.Errorf("interface name %q should end with one of: %v", name, validSuffixes)
	}

	return nil
}

// validateMethodName validates method naming conventions
func (v *Validator) validateMethodName(name string) error {
	if name == "" {
		return fmt.Errorf("method name cannot be empty")
	}

	if !isValidIdentifier(name) {
		return fmt.Errorf("method name %q is not a valid Go identifier", name)
	}

	if !isExported(name) {
		return fmt.Errorf("method name %q must be exported (start with uppercase letter)", name)
	}

	// Discourage certain method names
	discouragedNames := []string{"String", "Error", "GoString", "MarshalJSON", "UnmarshalJSON"}
	for _, discouraged := range discouragedNames {
		if name == discouraged {
			return fmt.Errorf("method name %q conflicts with common Go interface methods", name)
		}
	}

	return nil
}

// validateContextParameter validates context.Context parameter
func (v *Validator) validateContextParameter(method *RpcMethod) error {
	methodType := method.Type.Type

	if methodType.NumIn() < 2 {
		return fmt.Errorf("method must have context.Context as first parameter")
	}

	firstParam := methodType.In(1) // Skip receiver
	// Check for context.Context interface
	if firstParam.String() != "context.Context" {
		return fmt.Errorf("first parameter must be context.Context, got %s", firstParam)
	}

	return nil
}

// validateParameterType validates that a parameter type is RPC-safe
func (v *Validator) validateParameterType(paramType reflect.Type, paramName string) error {
	return v.validateType(paramType, paramName, "parameter")
}

// validateReturnType validates that a return type is RPC-safe
func (v *Validator) validateReturnType(returnType reflect.Type) error {
	return v.validateType(returnType, "return value", "return")
}

// validateType validates that a type is safe for RPC serialization
func (v *Validator) validateType(t reflect.Type, name, context string) error {
	if t == nil {
		return nil // nil types are allowed
	}

	// Check forbidden types
	for _, forbidden := range v.options.ForbiddenTypes {
		if t == forbidden {
			return fmt.Errorf("%s %s uses forbidden type %s", context, name, t)
		}
	}

	switch t.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		 reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		 reflect.Float32, reflect.Float64, reflect.String:
		return nil // Primitive types are always safe

	case reflect.Array, reflect.Slice:
		return v.validateType(t.Elem(), name+" element", context)

	case reflect.Map:
		if err := v.validateType(t.Key(), name+" key", context); err != nil {
			return err
		}
		return v.validateType(t.Elem(), name+" value", context)

	case reflect.Ptr:
		return v.validateType(t.Elem(), name, context)

	case reflect.Struct:
		return v.validateStructType(t, name, context)

	case reflect.Interface:
		return v.validateInterfaceType(t, name, context)

	case reflect.Func:
		if v.options.StrictTypes {
			return fmt.Errorf("%s %s: functions are not allowed (use RpcTarget interface)", context, name)
		}
		return nil

	case reflect.Chan:
		return fmt.Errorf("%s %s: channels cannot be serialized", context, name)

	case reflect.UnsafePointer:
		return fmt.Errorf("%s %s: unsafe pointers cannot be serialized", context, name)

	default:
		return fmt.Errorf("%s %s: unsupported type %s", context, name, t.Kind())
	}
}

// validateStructType validates struct types for RPC safety
func (v *Validator) validateStructType(t reflect.Type, name, context string) error {
	// Allow time.Time and other common types
	commonTypes := []string{
		"time.Time",
		"time.Duration",
		"url.URL",
		"big.Int",
		"big.Float",
	}

	typeString := t.String()
	for _, common := range commonTypes {
		if typeString == common {
			return nil
		}
	}

	// Validate struct fields
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue // Skip unexported fields
		}

		fieldName := fmt.Sprintf("%s.%s", name, field.Name)
		if err := v.validateType(field.Type, fieldName, context); err != nil {
			return err
		}
	}

	return nil
}

// validateInterfaceType validates interface types for RPC safety
func (v *Validator) validateInterfaceType(t reflect.Type, name, context string) error {
	// Allow empty interface
	if t == reflect.TypeOf((*interface{})(nil)).Elem() {
		if !v.options.AllowAny {
			return fmt.Errorf("%s %s: interface{} not allowed", context, name)
		}
		return nil
	}

	// Allow error interface
	if t == reflect.TypeOf((*error)(nil)).Elem() {
		return nil
	}

	// Allow context.Context
	if t.String() == "context.Context" {
		return nil
	}

	// Check if it's a registered RPC interface
	if _, exists := v.registry.GetInterfaceByType(t); exists {
		return nil
	}

	// For strict mode, only allow known interfaces
	if v.options.StrictTypes {
		return fmt.Errorf("%s %s: interface %s is not registered for RPC", context, name, t)
	}

	return nil
}

// validateMethodSignature validates the overall method signature consistency
func (v *Validator) validateMethodSignature(method *RpcMethod) error {
	// Check for common anti-patterns

	// Methods returning multiple values (other than value + error)
	methodType := method.Type.Type
	if methodType.NumOut() > 2 {
		return fmt.Errorf("method %s returns too many values (max 2: result and error)", method.Name)
	}

	// Variadic methods
	if methodType.IsVariadic() {
		return fmt.Errorf("method %s: variadic methods are not supported", method.Name)
	}

	return nil
}

// validateArgumentType validates a runtime argument against expected type
func (v *Validator) validateArgumentType(arg interface{}, expectedType reflect.Type, argIndex int) error {
	if arg == nil {
		// Check if nil is acceptable
		switch expectedType.Kind() {
		case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Func:
			return nil // nil is acceptable
		default:
			return fmt.Errorf("nil value not acceptable for type %s", expectedType)
		}
	}

	actualType := reflect.TypeOf(arg)

	// Direct type match
	if actualType == expectedType {
		return nil
	}

	// Assignability check
	if actualType.AssignableTo(expectedType) {
		return nil
	}

	// Convertibility check
	if actualType.ConvertibleTo(expectedType) {
		return nil
	}

	return fmt.Errorf("type mismatch: expected %s, got %s", expectedType, actualType)
}

// checkNamingConflicts checks for method name conflicts
func (v *Validator) checkNamingConflicts(iface *RpcInterface) error {
	methodNames := make(map[string]bool)

	for _, method := range iface.Methods {
		lowerName := strings.ToLower(method.Name)
		if methodNames[lowerName] {
			return fmt.Errorf("case-insensitive method name conflict: %s", method.Name)
		}
		methodNames[lowerName] = true
	}

	return nil
}

// Helper functions

// isValidIdentifier checks if a string is a valid Go identifier
func isValidIdentifier(name string) bool {
	if name == "" {
		return false
	}

	// First character must be letter or underscore
	first := rune(name[0])
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_') {
		return false
	}

	// Subsequent characters must be letter, digit, or underscore
	for _, r := range name[1:] {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}

	return true
}

// isExported checks if an identifier is exported (starts with uppercase)
func isExported(name string) bool {
	if name == "" {
		return false
	}
	first := rune(name[0])
	return first >= 'A' && first <= 'Z'
}

// ValidationError represents a validation error with detailed context
type ValidationError struct {
	Type        string // "interface", "method", "parameter", "return"
	Name        string // Name of the validated item
	Rule        string // Validation rule that failed
	Message     string // Error message
	Suggestions []string // Suggested fixes
}

// Error implements the error interface
func (ve *ValidationError) Error() string {
	return fmt.Sprintf("%s %s validation failed: %s", ve.Type, ve.Name, ve.Message)
}

// NewValidationError creates a new validation error
func NewValidationError(errorType, name, rule, message string) *ValidationError {
	return &ValidationError{
		Type:    errorType,
		Name:    name,
		Rule:    rule,
		Message: message,
	}
}

// DefaultValidator is the global validator instance
var DefaultValidator = NewValidator(DefaultRegistry, DefaultValidationOptions())

// ValidateInterface validates an interface using the default validator
func ValidateInterface(iface *RpcInterface) error {
	return DefaultValidator.ValidateInterface(iface)
}

// ValidateMethod validates a method using the default validator
func ValidateMethod(method *RpcMethod) error {
	return DefaultValidator.ValidateMethod(method)
}