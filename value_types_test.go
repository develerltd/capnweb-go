package capnweb

import (
	"errors"
	"testing"
	"time"
)

func TestValueTypeString(t *testing.T) {
	tests := map[ValueType]string{
		ValueTypeUndefined:   "undefined",
		ValueTypeNull:        "null",
		ValueTypeBool:        "bool",
		ValueTypeNumber:      "number",
		ValueTypeString:      "string",
		ValueTypeBigInt:      "bigint",
		ValueTypeDate:        "date",
		ValueTypeBytes:       "bytes",
		ValueTypeError:       "error",
		ValueTypeArray:       "array",
		ValueTypeObject:      "object",
		ValueTypeFunction:    "function",
		ValueTypeStub:        "stub",
		ValueTypePromise:     "promise",
		ValueTypeTarget:      "target",
		ValueTypeUnsupported: "unsupported",
		ValueType(999):       "unknown", // Invalid type
	}

	for vt, expected := range tests {
		result := vt.String()
		if result != expected {
			t.Errorf("ValueType(%d).String() = %s, expected %s", vt, result, expected)
		}
	}
}

func TestTypeForValue(t *testing.T) {
	tests := []struct {
		value    interface{}
		expected ValueType
	}{
		// Null/nil values
		{nil, ValueTypeNull},
		{(*int)(nil), ValueTypeNull},

		// Basic types
		{true, ValueTypeBool},
		{false, ValueTypeBool},
		{42, ValueTypeNumber},
		{int8(42), ValueTypeNumber},
		{int16(42), ValueTypeNumber},
		{int32(42), ValueTypeNumber},
		{int64(42), ValueTypeNumber},
		{uint(42), ValueTypeNumber},
		{uint8(42), ValueTypeNumber},
		{uint16(42), ValueTypeNumber},
		{uint32(42), ValueTypeNumber},
		{uint64(42), ValueTypeNumber},
		{float32(3.14), ValueTypeNumber},
		{float64(3.14), ValueTypeNumber},
		{"hello", ValueTypeString},

		// Special types
		{time.Now(), ValueTypeDate},
		{[]byte("bytes"), ValueTypeBytes},
		{[4]byte{1, 2, 3, 4}, ValueTypeBytes},
		{errors.New("test error"), ValueTypeError},

		// Composite types
		{[]int{1, 2, 3}, ValueTypeArray},
		{[3]int{1, 2, 3}, ValueTypeArray},
		{map[string]int{"a": 1}, ValueTypeObject},
		{struct{ Name string }{Name: "test"}, ValueTypeObject},

		// RPC types
		{&MockRpcTarget{}, ValueTypeTarget},
		{func() {}, ValueTypeFunction},
	}

	for _, test := range tests {
		result := TypeForValue(test.value)
		if result != test.expected {
			t.Errorf("TypeForValue(%v) = %s, expected %s",
				test.value, result.String(), test.expected.String())
		}
	}
}

func TestTypeForValuePointers(t *testing.T) {
	// Test pointer handling
	value := 42
	ptr := &value

	result := TypeForValue(ptr)
	if result != ValueTypeNumber {
		t.Errorf("TypeForValue(&int) should dereference to number, got %s", result.String())
	}

	// Test nil pointer
	var nilPtr *int
	result = TypeForValue(nilPtr)
	if result != ValueTypeNull {
		t.Errorf("TypeForValue(nil pointer) should be null, got %s", result.String())
	}
}

func TestTypeForValueInterfaces(t *testing.T) {
	// Test interface{} with concrete value
	var iface interface{} = 42
	result := TypeForValue(iface)
	if result != ValueTypeNumber {
		t.Errorf("TypeForValue(interface{} with int) should be number, got %s", result.String())
	}

	// Test nil interface
	var nilIface interface{}
	result = TypeForValue(nilIface)
	if result != ValueTypeNull {
		t.Errorf("TypeForValue(nil interface{}) should be null, got %s", result.String())
	}
}

func TestValueTypePredicates(t *testing.T) {
	tests := []struct {
		vt           ValueType
		serializable bool
		primitive    bool
		composite    bool
		rpcType      bool
	}{
		{ValueTypeUndefined, false, false, false, false},
		{ValueTypeUnsupported, false, false, false, false},
		{ValueTypeNull, true, true, false, false},
		{ValueTypeBool, true, true, false, false},
		{ValueTypeNumber, true, true, false, false},
		{ValueTypeString, true, true, false, false},
		{ValueTypeDate, true, true, false, false},
		{ValueTypeBytes, true, true, false, false},
		{ValueTypeError, true, true, false, false},
		{ValueTypeArray, true, false, true, false},
		{ValueTypeObject, true, false, true, false},
		{ValueTypeFunction, true, false, false, true},
		{ValueTypeStub, true, false, false, true},
		{ValueTypePromise, true, false, false, true},
		{ValueTypeTarget, true, false, false, true},
	}

	for _, test := range tests {
		if test.vt.IsSerializable() != test.serializable {
			t.Errorf("%s.IsSerializable() = %v, expected %v",
				test.vt.String(), test.vt.IsSerializable(), test.serializable)
		}

		if test.vt.IsPrimitive() != test.primitive {
			t.Errorf("%s.IsPrimitive() = %v, expected %v",
				test.vt.String(), test.vt.IsPrimitive(), test.primitive)
		}

		if test.vt.IsComposite() != test.composite {
			t.Errorf("%s.IsComposite() = %v, expected %v",
				test.vt.String(), test.vt.IsComposite(), test.composite)
		}

		if test.vt.IsRpcType() != test.rpcType {
			t.Errorf("%s.IsRpcType() = %v, expected %v",
				test.vt.String(), test.vt.IsRpcType(), test.rpcType)
		}
	}
}

func TestGetSerializationHint(t *testing.T) {
	tests := []struct {
		value       interface{}
		expectedType ValueType
		isReference bool
		isPromise   bool
		checkSize   bool
		expectedSize int
	}{
		{"hello", ValueTypeString, false, false, true, 5},
		{[]byte("test"), ValueTypeBytes, false, false, true, 4},
		{&MockRpcTarget{}, ValueTypeTarget, true, false, true, 32},
		{func() {}, ValueTypeFunction, true, false, true, 32},
		{42, ValueTypeNumber, false, false, true, 8},
		{[]int{1, 2, 3}, ValueTypeArray, false, false, true, -1},
		{map[string]int{"a": 1}, ValueTypeObject, false, false, true, -1},
	}

	for _, test := range tests {
		hint := GetSerializationHint(test.value)

		if hint.Type != test.expectedType {
			t.Errorf("GetSerializationHint(%v).Type = %s, expected %s",
				test.value, hint.Type.String(), test.expectedType.String())
		}

		if hint.IsReference != test.isReference {
			t.Errorf("GetSerializationHint(%v).IsReference = %v, expected %v",
				test.value, hint.IsReference, test.isReference)
		}

		if hint.IsPromise != test.isPromise {
			t.Errorf("GetSerializationHint(%v).IsPromise = %v, expected %v",
				test.value, hint.IsPromise, test.isPromise)
		}

		if test.checkSize && hint.Size != test.expectedSize {
			t.Errorf("GetSerializationHint(%v).Size = %d, expected %d",
				test.value, hint.Size, test.expectedSize)
		}
	}
}

func TestValueTypeSet(t *testing.T) {
	// Test creation
	set := NewValueTypeSet(ValueTypeString, ValueTypeNumber, ValueTypeBool)

	// Test Contains
	if !set.Contains(ValueTypeString) {
		t.Error("Set should contain ValueTypeString")
	}

	if !set.Contains(ValueTypeNumber) {
		t.Error("Set should contain ValueTypeNumber")
	}

	if !set.Contains(ValueTypeBool) {
		t.Error("Set should contain ValueTypeBool")
	}

	if set.Contains(ValueTypeArray) {
		t.Error("Set should not contain ValueTypeArray")
	}

	// Test Add
	set.Add(ValueTypeArray)
	if !set.Contains(ValueTypeArray) {
		t.Error("Set should contain ValueTypeArray after Add")
	}

	// Test Remove
	set.Remove(ValueTypeString)
	if set.Contains(ValueTypeString) {
		t.Error("Set should not contain ValueTypeString after Remove")
	}
}

func TestPredefinedValueTypeSets(t *testing.T) {
	// Test PrimitiveTypes
	primitiveTypes := []ValueType{
		ValueTypeNull, ValueTypeBool, ValueTypeNumber, ValueTypeString,
		ValueTypeBigInt, ValueTypeDate, ValueTypeBytes, ValueTypeError,
	}

	for _, vt := range primitiveTypes {
		if !PrimitiveTypes.Contains(vt) {
			t.Errorf("PrimitiveTypes should contain %s", vt.String())
		}
	}

	// Test CompositeTypes
	compositeTypes := []ValueType{ValueTypeArray, ValueTypeObject}
	for _, vt := range compositeTypes {
		if !CompositeTypes.Contains(vt) {
			t.Errorf("CompositeTypes should contain %s", vt.String())
		}
	}

	// Test RpcTypes
	rpcTypes := []ValueType{
		ValueTypeFunction, ValueTypeStub, ValueTypePromise, ValueTypeTarget,
	}

	for _, vt := range rpcTypes {
		if !RpcTypes.Contains(vt) {
			t.Errorf("RpcTypes should contain %s", vt.String())
		}
	}

	// Test SerializableTypes - should not contain unsupported types
	if SerializableTypes.Contains(ValueTypeUndefined) {
		t.Error("SerializableTypes should not contain ValueTypeUndefined")
	}

	if SerializableTypes.Contains(ValueTypeUnsupported) {
		t.Error("SerializableTypes should not contain ValueTypeUnsupported")
	}

	// Should contain all others
	for _, vt := range primitiveTypes {
		if !SerializableTypes.Contains(vt) {
			t.Errorf("SerializableTypes should contain %s", vt.String())
		}
	}

	for _, vt := range compositeTypes {
		if !SerializableTypes.Contains(vt) {
			t.Errorf("SerializableTypes should contain %s", vt.String())
		}
	}

	for _, vt := range rpcTypes {
		if !SerializableTypes.Contains(vt) {
			t.Errorf("SerializableTypes should contain %s", vt.String())
		}
	}
}