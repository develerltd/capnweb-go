package capnweb

import (
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Mock implementations for testing

type SerializationMockStub struct {
	importID *ImportID
	exportID *ExportID
	disposed bool
}

func (m *SerializationMockStub) GetImportID() *ImportID { return m.importID }
func (m *SerializationMockStub) GetExportID() *ExportID { return m.exportID }
func (m *SerializationMockStub) Dispose() error         { m.disposed = true; return nil }
func (m *SerializationMockStub) IsDisposed() bool       { return m.disposed }

func TestSerializerPrimitiveTypes(t *testing.T) {
	serializer := NewSerializer(nil)

	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{"nil", nil, nil},
		{"bool true", true, true},
		{"bool false", false, false},
		{"int", 42, 42},
		{"float", 3.14, 3.14},
		{"string", "hello", "hello"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := serializer.Serialize(test.input)
			if err != nil {
				t.Fatalf("Serialize failed: %v", err)
			}

			if !reflect.DeepEqual(result, test.expected) {
				t.Errorf("Expected %v, got %v", test.expected, result)
			}
		})
	}
}

func TestSerializerSpecialTypes(t *testing.T) {
	serializer := NewSerializer(nil)

	t.Run("bigint", func(t *testing.T) {
		bigInt := new(big.Int)
		bigInt.SetString("12345678901234567890", 10)
		result, err := serializer.Serialize(bigInt)
		if err != nil {
			t.Fatalf("Serialize failed: %v", err)
		}

		serialized, ok := result.([]interface{})
		if !ok {
			t.Fatalf("Expected []interface{}, got %T", result)
		}

		if len(serialized) != 2 {
			t.Fatalf("Expected 2 elements, got %d", len(serialized))
		}

		if serialized[0] != "bigint" {
			t.Errorf("Expected type 'bigint', got '%s'", serialized[0])
		}

		if serialized[1] != "12345678901234567890" {
			t.Errorf("Expected '12345678901234567890', got '%v'", serialized[1])
		}
	})

	t.Run("date", func(t *testing.T) {
		now := time.Now()
		result, err := serializer.Serialize(now)
		if err != nil {
			t.Fatalf("Serialize failed: %v", err)
		}

		serialized, ok := result.([]interface{})
		if !ok {
			t.Fatalf("Expected []interface{}, got %T", result)
		}

		if len(serialized) != 2 {
			t.Fatalf("Expected 2 elements, got %d", len(serialized))
		}

		if serialized[0] != "date" {
			t.Errorf("Expected type 'date', got '%s'", serialized[0])
		}

		expectedValue := int64(now.UnixMilli())
		if actualValue, ok := serialized[1].(int64); !ok || actualValue != expectedValue {
			t.Errorf("Expected %v, got %v", expectedValue, serialized[1])
		}
	})

	t.Run("bytes", func(t *testing.T) {
		bytes := []byte{1, 2, 3, 4, 5}
		result, err := serializer.Serialize(bytes)
		if err != nil {
			t.Fatalf("Serialize failed: %v", err)
		}

		serialized, ok := result.([]interface{})
		if !ok {
			t.Fatalf("Expected []interface{}, got %T", result)
		}

		if len(serialized) != 2 {
			t.Fatalf("Expected 2 elements, got %d", len(serialized))
		}

		if serialized[0] != "bytes" {
			t.Errorf("Expected type 'bytes', got '%s'", serialized[0])
		}

		expectedValue := "AQIDBAU="  // base64 encoding of [1,2,3,4,5]
		if serialized[1] != expectedValue {
			t.Errorf("Expected %v, got %v", expectedValue, serialized[1])
		}
	})
}

func TestSerializerError(t *testing.T) {
	serializer := NewSerializer(nil)
	// Keep IncludeStackTraces = true for RpcError test

	t.Run("regular error", func(t *testing.T) {
		err := errors.New("test error")
		result, serErr := serializer.Serialize(err)
		if serErr != nil {
			t.Fatalf("Serialize failed: %v", serErr)
		}

		serialized, ok := result.([]interface{})
		if !ok {
			t.Fatalf("Expected []interface{}, got %T", result)
		}

		if len(serialized) != 2 {
			t.Fatalf("Expected 2 elements, got %d", len(serialized))
		}

		if serialized[0] != "error" {
			t.Errorf("Expected type 'error', got '%s'", serialized[0])
		}

		valueMap, ok := serialized[1].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected map value, got %T", serialized[1])
		}

		if valueMap["message"] != "test error" {
			t.Errorf("Expected message 'test error', got '%v'", valueMap["message"])
		}
	})

	t.Run("RpcError", func(t *testing.T) {
		rpcErr := &RpcError{
			Type:    "TestError",
			Message: "test message",
			Code:    1001,
			Stack:   "test stack",
		}

		result, serErr := serializer.Serialize(rpcErr)
		if serErr != nil {
			t.Fatalf("Serialize failed: %v", serErr)
		}

		serialized, ok := result.([]interface{})
		if !ok {
			t.Fatalf("Expected []interface{}, got %T", result)
		}

		if len(serialized) != 2 {
			t.Fatalf("Expected 2 elements, got %d", len(serialized))
		}

		if serialized[0] != "error" {
			t.Errorf("Expected type 'error', got '%s'", serialized[0])
		}

		valueMap := serialized[1].(map[string]interface{})
		if valueMap["name"] != "TestError" {
			t.Errorf("Expected name 'TestError', got '%v'", valueMap["name"])
		}

		if valueMap["code"] != 1001 {
			t.Errorf("Expected code 1001, got '%v'", valueMap["code"])
		}

		if valueMap["stack"] != "test stack" {
			t.Errorf("Expected stack 'test stack', got '%v'", valueMap["stack"])
		}
	})
}

func TestSerializerArray(t *testing.T) {
	serializer := NewSerializer(nil)

	t.Run("int array", func(t *testing.T) {
		input := []int{1, 2, 3, 4, 5}
		result, err := serializer.Serialize(input)
		if err != nil {
			t.Fatalf("Serialize failed: %v", err)
		}

		array, ok := result.([]interface{})
		if !ok {
			t.Fatalf("Expected array, got %T", result)
		}

		expected := []interface{}{1, 2, 3, 4, 5}
		if !reflect.DeepEqual(array, expected) {
			t.Errorf("Expected %v, got %v", expected, array)
		}
	})

	t.Run("mixed array", func(t *testing.T) {
		input := []interface{}{1, "hello", true, nil}
		result, err := serializer.Serialize(input)
		if err != nil {
			t.Fatalf("Serialize failed: %v", err)
		}

		array, ok := result.([]interface{})
		if !ok {
			t.Fatalf("Expected array, got %T", result)
		}

		expected := []interface{}{1, "hello", true, nil}
		if !reflect.DeepEqual(array, expected) {
			t.Errorf("Expected %v, got %v", expected, array)
		}
	})
}

func TestSerializerObject(t *testing.T) {
	serializer := NewSerializer(nil)

	t.Run("map", func(t *testing.T) {
		input := map[string]interface{}{
			"name": "John",
			"age":  30,
			"active": true,
		}

		result, err := serializer.Serialize(input)
		if err != nil {
			t.Fatalf("Serialize failed: %v", err)
		}

		obj, ok := result.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected map, got %T", result)
		}

		if obj["name"] != "John" {
			t.Errorf("Expected name 'John', got '%v'", obj["name"])
		}
		if obj["age"] != 30 {
			t.Errorf("Expected age 30, got '%v'", obj["age"])
		}
		if obj["active"] != true {
			t.Errorf("Expected active true, got '%v'", obj["active"])
		}
	})

	t.Run("struct", func(t *testing.T) {
		type TestStruct struct {
			Name   string `json:"name"`
			Age    int    `json:"age"`
			Active bool   `json:"active"`
			Hidden string `json:"-"` // Should be ignored
		}

		input := TestStruct{
			Name:   "Jane",
			Age:    25,
			Active: false,
			Hidden: "secret",
		}

		result, err := serializer.Serialize(input)
		if err != nil {
			t.Fatalf("Serialize failed: %v", err)
		}

		obj, ok := result.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected map, got %T", result)
		}

		if obj["name"] != "Jane" {
			t.Errorf("Expected name 'Jane', got '%v'", obj["name"])
		}
		if obj["age"] != 25 {
			t.Errorf("Expected age 25, got '%v'", obj["age"])
		}
		if obj["active"] != false {
			t.Errorf("Expected active false, got '%v'", obj["active"])
		}
		if _, exists := obj["Hidden"]; exists {
			t.Error("Hidden field should not be serialized")
		}
	})
}

func TestSerializerRpcTypes(t *testing.T) {
	var exportedID ExportID
	exportFunc := func(value interface{}) (ExportID, error) {
		exportedID++
		return exportedID, nil
	}

	serializer := NewSerializer(exportFunc)

	t.Run("function", func(t *testing.T) {
		fn := func() string { return "hello" }
		result, err := serializer.Serialize(fn)
		if err != nil {
			t.Fatalf("Serialize failed: %v", err)
		}

		serialized, ok := result.([]interface{})
		if !ok {
			t.Fatalf("Expected []interface{}, got %T", result)
		}

		if len(serialized) != 2 {
			t.Fatalf("Expected 2 elements, got %d", len(serialized))
		}

		if serialized[0] != "export" {
			t.Errorf("Expected type 'export', got '%s'", serialized[0])
		}

		if serialized[1] != ExportID(1) {
			t.Errorf("Expected export ID 1, got %v", serialized[1])
		}
	})

	t.Run("target", func(t *testing.T) {
		target := &MockRpcTarget{}
		result, err := serializer.Serialize(target)
		if err != nil {
			t.Fatalf("Serialize failed: %v", err)
		}

		serialized, ok := result.([]interface{})
		if !ok {
			t.Fatalf("Expected []interface{}, got %T", result)
		}

		if len(serialized) != 2 {
			t.Fatalf("Expected 2 elements, got %d", len(serialized))
		}

		if serialized[0] != "export" {
			t.Errorf("Expected type 'export', got '%s'", serialized[0])
		}

		if serialized[1] != ExportID(2) {
			t.Errorf("Expected export ID 2, got %v", serialized[1])
		}

		// Note: Methods metadata is no longer part of the serialized format
		// in the new array-based format - it's handled differently
	})

	t.Run("stub", func(t *testing.T) {
		importID := ImportID(42)
		stub := &MockStub{importID: &importID}

		result, err := serializer.Serialize(stub)
		if err != nil {
			t.Fatalf("Serialize failed: %v", err)
		}

		serialized, ok := result.([]interface{})
		if !ok {
			t.Fatalf("Expected []interface{}, got %T", result)
		}

		if len(serialized) != 2 {
			t.Fatalf("Expected 2 elements, got %d", len(serialized))
		}

		if serialized[0] != "import" {
			t.Errorf("Expected type 'import', got '%s'", serialized[0])
		}

		if serialized[1] != ImportID(42) {
			t.Errorf("Expected import ID 42, got %v", serialized[1])
		}
	})
}

func TestDeserializerPrimitiveTypes(t *testing.T) {
	deserializer := NewDeserializer(nil)

	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{"nil", nil, nil},
		{"bool", true, true},
		{"number", float64(42), float64(42)},
		{"string", "hello", "hello"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := deserializer.Deserialize(test.input)
			if err != nil {
				t.Fatalf("Deserialize failed: %v", err)
			}

			if !reflect.DeepEqual(result, test.expected) {
				t.Errorf("Expected %v, got %v", test.expected, result)
			}
		})
	}
}

func TestDeserializerSpecialTypes(t *testing.T) {
	deserializer := NewDeserializer(nil)

	t.Run("bigint", func(t *testing.T) {
		input := []interface{}{
			"bigint",
			"12345678901234567890",
		}

		result, err := deserializer.Deserialize(input)
		if err != nil {
			t.Fatalf("Deserialize failed: %v", err)
		}

		bigInt, ok := result.(*big.Int)
		if !ok {
			t.Fatalf("Expected *big.Int, got %T", result)
		}

		expected := big.NewInt(0)
		expected.SetString("12345678901234567890", 10)

		if bigInt.Cmp(expected) != 0 {
			t.Errorf("Expected %v, got %v", expected, bigInt)
		}
	})

	t.Run("date", func(t *testing.T) {
		now := time.Now()
		milliseconds := float64(now.UnixMilli())

		input := []interface{}{
			"date",
			milliseconds,
		}

		result, err := deserializer.Deserialize(input)
		if err != nil {
			t.Fatalf("Deserialize failed: %v", err)
		}

		date, ok := result.(time.Time)
		if !ok {
			t.Fatalf("Expected time.Time, got %T", result)
		}

		// Compare with some tolerance due to millisecond precision
		if date.Sub(now).Abs() > time.Second {
			t.Errorf("Expected time close to %v, got %v", now, date)
		}
	})

	t.Run("bytes", func(t *testing.T) {
		input := []interface{}{
			"bytes",
			"AQIDBAU=", // base64 encoding of [1,2,3,4,5]
		}

		result, err := deserializer.Deserialize(input)
		if err != nil {
			t.Fatalf("Deserialize failed: %v", err)
		}

		bytes, ok := result.([]byte)
		if !ok {
			t.Fatalf("Expected []byte, got %T", result)
		}

		expected := []byte{1, 2, 3, 4, 5}
		if !reflect.DeepEqual(bytes, expected) {
			t.Errorf("Expected %v, got %v", expected, bytes)
		}
	})
}

func TestDeserializerError(t *testing.T) {
	deserializer := NewDeserializer(nil)

	t.Run("regular error", func(t *testing.T) {
		input := []interface{}{
			"error",
			map[string]interface{}{
				"message": "test error",
			},
		}

		result, err := deserializer.Deserialize(input)
		if err != nil {
			t.Fatalf("Deserialize failed: %v", err)
		}

		errValue, ok := result.(error)
		if !ok {
			t.Fatalf("Expected error, got %T", result)
		}

		if errValue.Error() != "test error" {
			t.Errorf("Expected 'test error', got '%s'", errValue.Error())
		}
	})

	t.Run("RpcError", func(t *testing.T) {
		input := []interface{}{
			"error",
			map[string]interface{}{
				"name":    "TestError",
				"message": "test message",
				"code":    float64(1001),
				"stack":   "test stack",
			},
		}

		result, err := deserializer.Deserialize(input)
		if err != nil {
			t.Fatalf("Deserialize failed: %v", err)
		}

		rpcErr, ok := result.(*RpcError)
		if !ok {
			t.Fatalf("Expected *RpcError, got %T", result)
		}

		if rpcErr.Type != "TestError" {
			t.Errorf("Expected type 'TestError', got '%s'", rpcErr.Type)
		}

		if rpcErr.Message != "test message" {
			t.Errorf("Expected message 'test message', got '%s'", rpcErr.Message)
		}

		if rpcErr.Code != 1001 {
			t.Errorf("Expected code 1001, got %d", rpcErr.Code)
		}

		if rpcErr.Stack != "test stack" {
			t.Errorf("Expected stack 'test stack', got '%s'", rpcErr.Stack)
		}
	})
}

func TestDeserializerRpcTypes(t *testing.T) {
	var importCalls []struct {
		id        ImportID
		isPromise bool
	}

	importFunc := func(id ImportID, isPromise bool) (interface{}, error) {
		importCalls = append(importCalls, struct {
			id        ImportID
			isPromise bool
		}{id, isPromise})
		return &MockStub{importID: &id}, nil
	}

	deserializer := NewDeserializer(importFunc)

	t.Run("import", func(t *testing.T) {
		input := []interface{}{
			"import",
			float64(42),
		}

		result, err := deserializer.Deserialize(input)
		if err != nil {
			t.Fatalf("Deserialize failed: %v", err)
		}

		stub, ok := result.(*MockStub)
		if !ok {
			t.Fatalf("Expected *MockStub, got %T", result)
		}

		if *stub.importID != ImportID(42) {
			t.Errorf("Expected import ID 42, got %v", *stub.importID)
		}

		if len(importCalls) != 1 || importCalls[0].id != ImportID(42) || importCalls[0].isPromise {
			t.Errorf("Expected import call with ID 42, isPromise=false, got %v", importCalls)
		}
	})

	t.Run("promise", func(t *testing.T) {
		importCalls = nil // Reset

		input := []interface{}{
			"promise",
			float64(24),
		}

		_, err := deserializer.Deserialize(input)
		if err != nil {
			t.Fatalf("Deserialize failed: %v", err)
		}

		if len(importCalls) != 1 || importCalls[0].id != ImportID(24) || !importCalls[0].isPromise {
			t.Errorf("Expected import call with ID 24, isPromise=true, got %v", importCalls)
		}
	})
}

func TestSerializationRoundTrip(t *testing.T) {
	var exportedItems []interface{}
	var importedItems []interface{}

	exportFunc := func(value interface{}) (ExportID, error) {
		exportedItems = append(exportedItems, value)
		return ExportID(len(exportedItems)), nil
	}

	importFunc := func(id ImportID, isPromise bool) (interface{}, error) {
		if int(id) <= len(importedItems) {
			return importedItems[int(id)-1], nil
		}
		stub := &MockStub{importID: &id}
		importedItems = append(importedItems, stub)
		return stub, nil
	}

	serializer := NewSerializer(exportFunc)
	deserializer := NewDeserializer(importFunc)

	tests := []struct {
		name  string
		input interface{}
	}{
		{"primitive", 42},
		{"string", "hello world"},
		{"bool", true},
		{"nil", nil},
		{"array", []interface{}{1, "two", true, nil}},
		{"object", map[string]interface{}{"a": 1, "b": "two"}},
		{"bigint", big.NewInt(123456789)},
		{"date", time.Now()},
		{"bytes", []byte{1, 2, 3, 4}},
		{"error", errors.New("test error")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Serialize
			serialized, err := serializer.Serialize(test.input)
			if err != nil {
				t.Fatalf("Serialize failed: %v", err)
			}

			// Convert through JSON to simulate network transmission
			jsonData, err := json.Marshal(serialized)
			if err != nil {
				t.Fatalf("JSON marshal failed: %v", err)
			}

			var intermediate interface{}
			if err := json.Unmarshal(jsonData, &intermediate); err != nil {
				t.Fatalf("JSON unmarshal failed: %v", err)
			}

			// Deserialize
			result, err := deserializer.Deserialize(intermediate)
			if err != nil {
				t.Fatalf("Deserialize failed: %v", err)
			}

			// Compare (with type-specific logic)
			switch expected := test.input.(type) {
			case *big.Int:
				if resultBig, ok := result.(*big.Int); ok {
					if expected.Cmp(resultBig) != 0 {
						t.Errorf("BigInt mismatch: expected %v, got %v", expected, resultBig)
					}
				} else {
					t.Errorf("Expected *big.Int, got %T", result)
				}

			case time.Time:
				if resultTime, ok := result.(time.Time); ok {
					if expected.Sub(resultTime).Abs() > time.Millisecond {
						t.Errorf("Time mismatch: expected %v, got %v", expected, resultTime)
					}
				} else {
					t.Errorf("Expected time.Time, got %T", result)
				}

			case error:
				if resultErr, ok := result.(error); ok {
					// After round trip, regular errors become RpcErrors with type "Error"
					// So we check if the original message is contained in the result
					expectedMsg := expected.Error()
					actualMsg := resultErr.Error()
					if !strings.Contains(actualMsg, expectedMsg) {
						t.Errorf("Error mismatch: expected message %q to be contained in %q", expectedMsg, actualMsg)
					}
				} else {
					t.Errorf("Expected error, got %T", result)
				}

			default:
				// Handle JSON number conversion for integers
				if expectedInt, ok := test.input.(int); ok {
					if resultFloat, ok := result.(float64); ok {
						if float64(expectedInt) != resultFloat {
							t.Errorf("Value mismatch: expected %v, got %v", test.input, result)
						}
						return
					}
				}

				// For complex types, use deep comparison with type conversion
				if !deepEqual(test.input, result) {
					t.Errorf("Value mismatch: expected %v, got %v", test.input, result)
				}
			}
		})
	}
}

func TestJSONUtilities(t *testing.T) {
	serializer := NewSerializer(nil)
	deserializer := NewDeserializer(nil)

	t.Run("ToJSON", func(t *testing.T) {
		input := map[string]interface{}{
			"name": "test",
			"value": 42,
		}

		jsonData, err := serializer.ToJSON(input)
		if err != nil {
			t.Fatalf("ToJSON failed: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(jsonData, &result); err != nil {
			t.Fatalf("JSON unmarshal failed: %v", err)
		}

		if result["name"] != "test" || result["value"] != float64(42) {
			t.Errorf("JSON content mismatch: %v", result)
		}
	})

	t.Run("FromJSON", func(t *testing.T) {
		jsonData := []byte(`{"name": "test", "value": 42}`)

		result, err := deserializer.FromJSON(jsonData)
		if err != nil {
			t.Fatalf("FromJSON failed: %v", err)
		}

		obj, ok := result.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected map, got %T", result)
		}

		if obj["name"] != "test" || obj["value"] != float64(42) {
			t.Errorf("Deserialized content mismatch: %v", obj)
		}
	})
}

// deepEqual performs deep comparison with type conversions for JSON round-trip compatibility
func deepEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Handle number conversions (int -> float64 from JSON)
	if aInt, ok := a.(int); ok {
		if bFloat, ok := b.(float64); ok {
			return float64(aInt) == bFloat
		}
	}
	if aFloat, ok := a.(float64); ok {
		if bInt, ok := b.(int); ok {
			return aFloat == float64(bInt)
		}
	}

	// Handle slice comparison with type conversion
	aVal := reflect.ValueOf(a)
	bVal := reflect.ValueOf(b)

	if aVal.Kind() == reflect.Slice && bVal.Kind() == reflect.Slice {
		if aVal.Len() != bVal.Len() {
			return false
		}
		for i := 0; i < aVal.Len(); i++ {
			if !deepEqual(aVal.Index(i).Interface(), bVal.Index(i).Interface()) {
				return false
			}
		}
		return true
	}

	// Handle map comparison
	if aMap, ok := a.(map[string]interface{}); ok {
		if bMap, ok := b.(map[string]interface{}); ok {
			if len(aMap) != len(bMap) {
				return false
			}
			for k, v := range aMap {
				if bv, exists := bMap[k]; !exists || !deepEqual(v, bv) {
					return false
				}
			}
			return true
		}
	}

	// Use reflect.DeepEqual for everything else
	return reflect.DeepEqual(a, b)
}