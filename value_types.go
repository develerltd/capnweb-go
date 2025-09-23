package capnweb

import (
	"reflect"
	"time"
)

// ValueType represents the different types of values that can be serialized
// over RPC. This closely mirrors the JavaScript implementation's type system.
type ValueType int

const (
	// Basic value types
	ValueTypeUndefined ValueType = iota
	ValueTypeNull
	ValueTypeBool
	ValueTypeNumber
	ValueTypeString
	ValueTypeBigInt
	ValueTypeDate
	ValueTypeBytes
	ValueTypeError

	// Composite types
	ValueTypeArray
	ValueTypeObject

	// RPC-specific types
	ValueTypeFunction
	ValueTypeStub
	ValueTypePromise
	ValueTypeTarget

	// Special types
	ValueTypeUnsupported
)

// String returns a string representation of the ValueType.
func (vt ValueType) String() string {
	switch vt {
	case ValueTypeUndefined:
		return "undefined"
	case ValueTypeNull:
		return "null"
	case ValueTypeBool:
		return "bool"
	case ValueTypeNumber:
		return "number"
	case ValueTypeString:
		return "string"
	case ValueTypeBigInt:
		return "bigint"
	case ValueTypeDate:
		return "date"
	case ValueTypeBytes:
		return "bytes"
	case ValueTypeError:
		return "error"
	case ValueTypeArray:
		return "array"
	case ValueTypeObject:
		return "object"
	case ValueTypeFunction:
		return "function"
	case ValueTypeStub:
		return "stub"
	case ValueTypePromise:
		return "promise"
	case ValueTypeTarget:
		return "target"
	case ValueTypeUnsupported:
		return "unsupported"
	default:
		return "unknown"
	}
}

// TypeForValue determines the ValueType for a given Go value.
// This is similar to the JavaScript implementation's typeForRpc function.
func TypeForValue(value interface{}) ValueType {
	if value == nil {
		return ValueTypeNull
	}

	rv := reflect.ValueOf(value)
	rt := rv.Type()

	switch rv.Kind() {
	case reflect.Bool:
		return ValueTypeBool

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		 reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		 reflect.Float32, reflect.Float64:
		return ValueTypeNumber

	case reflect.String:
		return ValueTypeString

	case reflect.Slice:
		// Special case for byte slices
		if rt.Elem().Kind() == reflect.Uint8 {
			return ValueTypeBytes
		}
		return ValueTypeArray

	case reflect.Array:
		// Special case for byte arrays
		if rt.Elem().Kind() == reflect.Uint8 {
			return ValueTypeBytes
		}
		return ValueTypeArray

	case reflect.Map, reflect.Struct:
		// Check for special types first
		if rt == reflect.TypeOf(time.Time{}) {
			return ValueTypeDate
		}

		return ValueTypeObject

	case reflect.Ptr:
		if rv.IsNil() {
			return ValueTypeNull
		}

		// Check interface implementations before dereferencing
		if _, ok := value.(error); ok {
			return ValueTypeError
		}

		if _, ok := value.(RpcTarget); ok {
			return ValueTypeTarget
		}

		if _, ok := value.(Stub); ok {
			return ValueTypeStub
		}

		// Dereference and check the pointed-to value
		return TypeForValue(rv.Elem().Interface())

	case reflect.Interface:
		if rv.IsNil() {
			return ValueTypeNull
		}

		// Check interface implementations first
		if _, ok := value.(error); ok {
			return ValueTypeError
		}

		if _, ok := value.(RpcTarget); ok {
			return ValueTypeTarget
		}

		if _, ok := value.(Stub); ok {
			return ValueTypeStub
		}

		// Check the concrete value
		return TypeForValue(rv.Elem().Interface())

	case reflect.Func:
		return ValueTypeFunction

	default:
		return ValueTypeUnsupported
	}
}

// IsSerializable returns true if the given value type can be serialized
// across RPC boundaries.
func (vt ValueType) IsSerializable() bool {
	switch vt {
	case ValueTypeUndefined, ValueTypeUnsupported:
		return false
	default:
		return true
	}
}

// IsPrimitive returns true if the value type is a primitive type
// (can be serialized directly without special handling).
func (vt ValueType) IsPrimitive() bool {
	switch vt {
	case ValueTypeNull, ValueTypeBool, ValueTypeNumber, ValueTypeString,
		 ValueTypeBigInt, ValueTypeDate, ValueTypeBytes, ValueTypeError:
		return true
	default:
		return false
	}
}

// IsComposite returns true if the value type is a composite type
// (contains other values that need recursive serialization).
func (vt ValueType) IsComposite() bool {
	switch vt {
	case ValueTypeArray, ValueTypeObject:
		return true
	default:
		return false
	}
}

// IsRpcType returns true if the value type is RPC-specific
// (stubs, promises, targets, functions).
func (vt ValueType) IsRpcType() bool {
	switch vt {
	case ValueTypeFunction, ValueTypeStub, ValueTypePromise, ValueTypeTarget:
		return true
	default:
		return false
	}
}

// SerializationHint provides information about how a value should be serialized.
type SerializationHint struct {
	Type        ValueType
	IsReference bool // True if this should be passed by reference
	IsPromise   bool // True if this represents a promise
	Size        int  // Estimated serialized size
}

// GetSerializationHint analyzes a value and returns serialization information.
func GetSerializationHint(value interface{}) SerializationHint {
	valueType := TypeForValue(value)

	hint := SerializationHint{
		Type: valueType,
	}

	switch valueType {
	case ValueTypeFunction, ValueTypeTarget, ValueTypeStub:
		hint.IsReference = true
		hint.Size = 32 // Estimated size of a reference

	case ValueTypePromise:
		hint.IsReference = true
		hint.IsPromise = true
		hint.Size = 32

	case ValueTypeString:
		if s, ok := value.(string); ok {
			hint.Size = len(s)
		}

	case ValueTypeBytes:
		if rv := reflect.ValueOf(value); rv.Kind() == reflect.Slice {
			hint.Size = rv.Len()
		}

	case ValueTypeArray, ValueTypeObject:
		// For composite types, we'll estimate size during serialization
		hint.Size = -1

	default:
		hint.Size = 8 // Reasonable default for primitive types
	}

	return hint
}

// ValueTypeSet represents a set of value types for efficient membership testing.
type ValueTypeSet map[ValueType]struct{}

// NewValueTypeSet creates a new set containing the given value types.
func NewValueTypeSet(types ...ValueType) ValueTypeSet {
	set := make(ValueTypeSet, len(types))
	for _, t := range types {
		set[t] = struct{}{}
	}
	return set
}

// Contains returns true if the set contains the given value type.
func (s ValueTypeSet) Contains(vt ValueType) bool {
	_, exists := s[vt]
	return exists
}

// Add adds a value type to the set.
func (s ValueTypeSet) Add(vt ValueType) {
	s[vt] = struct{}{}
}

// Remove removes a value type from the set.
func (s ValueTypeSet) Remove(vt ValueType) {
	delete(s, vt)
}

// Common value type sets for convenience
var (
	// PrimitiveTypes contains all primitive value types
	PrimitiveTypes = NewValueTypeSet(
		ValueTypeNull, ValueTypeBool, ValueTypeNumber, ValueTypeString,
		ValueTypeBigInt, ValueTypeDate, ValueTypeBytes, ValueTypeError,
	)

	// CompositeTypes contains all composite value types
	CompositeTypes = NewValueTypeSet(
		ValueTypeArray, ValueTypeObject,
	)

	// RpcTypes contains all RPC-specific value types
	RpcTypes = NewValueTypeSet(
		ValueTypeFunction, ValueTypeStub, ValueTypePromise, ValueTypeTarget,
	)

	// SerializableTypes contains all types that can be serialized
	SerializableTypes = NewValueTypeSet(
		ValueTypeNull, ValueTypeBool, ValueTypeNumber, ValueTypeString,
		ValueTypeBigInt, ValueTypeDate, ValueTypeBytes, ValueTypeError,
		ValueTypeArray, ValueTypeObject, ValueTypeFunction, ValueTypeStub,
		ValueTypePromise, ValueTypeTarget,
	)
)