package capnweb

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"runtime"
	"time"
)

// Serializer handles conversion of Go values to JSON-serializable forms
// with RPC extensions for references, promises, and special types.
type Serializer struct {
	// ExportFunc is called when an RpcTarget or function needs to be exported
	ExportFunc func(value interface{}) (ExportID, error)

	// Options for customizing serialization behavior
	Options SerializerOptions
}

// SerializerOptions controls serialization behavior
type SerializerOptions struct {
	// IncludeStackTraces controls whether error stack traces are included
	IncludeStackTraces bool

	// MaxDepth limits recursion depth to prevent infinite loops
	MaxDepth int

	// DateFormat specifies how to serialize time.Time values
	DateFormat string

	// StrictTypes requires exact type matching for deserialization
	StrictTypes bool
}

// DefaultSerializerOptions returns reasonable defaults
func DefaultSerializerOptions() SerializerOptions {
	return SerializerOptions{
		IncludeStackTraces: true,
		MaxDepth:          100,
		DateFormat:        time.RFC3339Nano,
		StrictTypes:       false,
	}
}

// NewSerializer creates a new serializer with the given export function
func NewSerializer(exportFunc func(interface{}) (ExportID, error)) *Serializer {
	return &Serializer{
		ExportFunc: exportFunc,
		Options:    DefaultSerializerOptions(),
	}
}

// SerializedValue represents a value that has been serialized for RPC transmission
type SerializedValue struct {
	// Type indicates the RPC type of the serialized value
	Type string `json:"type"`

	// Value contains the actual serialized data
	Value interface{} `json:"value"`

	// Meta contains additional metadata for special types
	Meta map[string]interface{} `json:"meta,omitempty"`
}

// Serialize converts a Go value to a JSON-serializable form
func (s *Serializer) Serialize(value interface{}) (interface{}, error) {
	return s.serializeWithDepth(value, 0)
}

func (s *Serializer) serializeWithDepth(value interface{}, depth int) (interface{}, error) {
	if depth > s.Options.MaxDepth {
		return nil, fmt.Errorf("serialization depth limit exceeded (%d)", s.Options.MaxDepth)
	}

	valueType := TypeForValue(value)

	switch valueType {
	case ValueTypeNull:
		return nil, nil

	case ValueTypeBool, ValueTypeNumber, ValueTypeString:
		// These types serialize directly to JSON
		return value, nil

	case ValueTypeBigInt:
		// Convert big.Int to JavaScript-compatible array format: ["bigint", "value"]
		if bigInt, ok := value.(*big.Int); ok {
			return []interface{}{"bigint", bigInt.String()}, nil
		}
		return nil, fmt.Errorf("invalid bigint value: %T", value)

	case ValueTypeDate:
		// Serialize time.Time as JavaScript-compatible array format: ["date", milliseconds]
		if t, ok := value.(time.Time); ok {
			return []interface{}{"date", t.UnixMilli()}, nil
		}
		return nil, fmt.Errorf("invalid date value: %T", value)

	case ValueTypeBytes:
		// Encode byte slices as JavaScript-compatible array format: ["bytes", "base64"]
		if bytes, ok := value.([]byte); ok {
			encoded := base64.StdEncoding.EncodeToString(bytes)
			return []interface{}{"bytes", encoded}, nil
		}
		return nil, fmt.Errorf("invalid bytes value: %T", value)

	case ValueTypeError:
		return s.serializeError(value.(error))

	case ValueTypeArray:
		return s.serializeArray(value, depth)

	case ValueTypeObject:
		return s.serializeObject(value, depth)

	case ValueTypeFunction:
		return s.serializeFunction(value)

	case ValueTypeTarget:
		return s.serializeTarget(value)

	case ValueTypeStub:
		return s.serializeStub(value)

	case ValueTypePromise:
		return s.serializePromise(value)

	case ValueTypeUnsupported:
		return nil, fmt.Errorf("unsupported type for serialization: %T", value)

	default:
		return nil, fmt.Errorf("unknown value type: %s", valueType.String())
	}
}

func (s *Serializer) serializeError(err error) (interface{}, error) {
	// JavaScript-compatible error format: ["error", errorObject]
	errorObj := map[string]interface{}{
		"message": err.Error(),
	}

	// Handle RpcError specifically to preserve structure
	if rpcErr, ok := err.(*RpcError); ok {
		errorObj["name"] = rpcErr.Type
		errorObj["message"] = rpcErr.Message
		if rpcErr.Code != 0 {
			errorObj["code"] = rpcErr.Code
		}
		if s.Options.IncludeStackTraces && rpcErr.Stack != "" {
			errorObj["stack"] = rpcErr.Stack
		}
	} else {
		// Regular error
		errorObj["name"] = "Error"
		if s.Options.IncludeStackTraces {
			// Try to get stack trace from the error if it supports it
			if stackTracer, ok := err.(interface{ StackTrace() string }); ok {
				errorObj["stack"] = stackTracer.StackTrace()
			} else {
				// Fall back to current stack trace
				buf := make([]byte, 1024*4)
				n := runtime.Stack(buf, false)
				errorObj["stack"] = string(buf[:n])
			}
		}
	}

	return []interface{}{"error", errorObj}, nil
}

func (s *Serializer) serializeArray(value interface{}, depth int) (interface{}, error) {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, fmt.Errorf("expected slice or array, got %T", value)
	}

	length := rv.Len()
	result := make([]interface{}, length)

	for i := 0; i < length; i++ {
		elem := rv.Index(i).Interface()
		serialized, err := s.serializeWithDepth(elem, depth+1)
		if err != nil {
			return nil, fmt.Errorf("error serializing array element %d: %w", i, err)
		}
		result[i] = serialized
	}

	return result, nil
}

func (s *Serializer) serializeObject(value interface{}, depth int) (interface{}, error) {
	rv := reflect.ValueOf(value)
	rt := rv.Type()

	switch rv.Kind() {
	case reflect.Map:
		return s.serializeMap(rv, depth)

	case reflect.Struct:
		return s.serializeStruct(rv, rt, depth)

	case reflect.Ptr:
		if rv.IsNil() {
			return nil, nil
		}
		return s.serializeWithDepth(rv.Elem().Interface(), depth)

	default:
		return nil, fmt.Errorf("cannot serialize object type: %T", value)
	}
}

func (s *Serializer) serializeMap(rv reflect.Value, depth int) (interface{}, error) {
	result := make(map[string]interface{})

	for _, key := range rv.MapKeys() {
		keyStr := fmt.Sprintf("%v", key.Interface())
		value := rv.MapIndex(key).Interface()

		serialized, err := s.serializeWithDepth(value, depth+1)
		if err != nil {
			return nil, fmt.Errorf("error serializing map key %s: %w", keyStr, err)
		}
		result[keyStr] = serialized
	}

	return result, nil
}

func (s *Serializer) serializeStruct(rv reflect.Value, rt reflect.Type, depth int) (interface{}, error) {
	result := make(map[string]interface{})

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		fieldValue := rv.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Check for json tag
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		fieldName := field.Name
		if jsonTag != "" && jsonTag != "-" {
			// Use the json tag name if present
			if commaIdx := len(jsonTag); commaIdx > 0 {
				for j, c := range jsonTag {
					if c == ',' {
						commaIdx = j
						break
					}
				}
				if commaIdx > 0 {
					fieldName = jsonTag[:commaIdx]
				} else {
					fieldName = jsonTag
				}
			}
		}

		serialized, err := s.serializeWithDepth(fieldValue.Interface(), depth+1)
		if err != nil {
			return nil, fmt.Errorf("error serializing field %s: %w", fieldName, err)
		}
		result[fieldName] = serialized
	}

	return result, nil
}

func (s *Serializer) serializeFunction(value interface{}) (interface{}, error) {
	if s.ExportFunc == nil {
		return nil, fmt.Errorf("cannot serialize function: no export function provided")
	}

	exportID, err := s.ExportFunc(value)
	if err != nil {
		return nil, fmt.Errorf("failed to export function: %w", err)
	}

	// JavaScript-compatible format: ["export", exportID]
	return []interface{}{"export", exportID}, nil
}

func (s *Serializer) serializeTarget(value interface{}) (interface{}, error) {
	if s.ExportFunc == nil {
		return nil, fmt.Errorf("cannot serialize RpcTarget: no export function provided")
	}

	exportID, err := s.ExportFunc(value)
	if err != nil {
		return nil, fmt.Errorf("failed to export RpcTarget: %w", err)
	}

	// JavaScript-compatible format: ["export", exportID]
	// Note: JavaScript doesn't seem to distinguish between functions and targets in serialization
	return []interface{}{"export", exportID}, nil
}

func (s *Serializer) serializeStub(value interface{}) (interface{}, error) {
	stub := value.(Stub)

	// Check if it's a local or remote stub
	if importID := stub.GetImportID(); importID != nil {
		// JavaScript-compatible format: ["import", importID]
		return []interface{}{"import", *importID}, nil
	}

	if exportID := stub.GetExportID(); exportID != nil {
		// JavaScript-compatible format: ["export", exportID]
		return []interface{}{"export", *exportID}, nil
	}

	return nil, fmt.Errorf("stub has neither import nor export ID")
}

func (s *Serializer) serializePromise(value interface{}) (interface{}, error) {
	// Promises are handled similarly to stubs in JavaScript
	if stub, ok := value.(Stub); ok {
		if importID := stub.GetImportID(); importID != nil {
			// JavaScript-compatible format: ["import", importID]
			// Note: JavaScript doesn't seem to distinguish promises from regular imports in serialization
			return []interface{}{"import", *importID}, nil
		}
	}

	return nil, fmt.Errorf("cannot serialize promise: invalid type %T", value)
}

// Deserializer handles conversion from JSON-serializable forms back to Go values
type Deserializer struct {
	// ImportFunc is called when a reference needs to be imported
	ImportFunc func(id ImportID, isPromise bool) (interface{}, error)

	// Options for customizing deserialization behavior
	Options DeserializerOptions
}

// DeserializerOptions controls deserialization behavior
type DeserializerOptions struct {
	// StrictTypes requires exact type matching
	StrictTypes bool

	// CreateUnknownTypes controls whether unknown types create errors
	CreateUnknownTypes bool
}

// NewDeserializer creates a new deserializer with the given import function
func NewDeserializer(importFunc func(ImportID, bool) (interface{}, error)) *Deserializer {
	return &Deserializer{
		ImportFunc: importFunc,
		Options: DeserializerOptions{
			StrictTypes:        false,
			CreateUnknownTypes: true,
		},
	}
}

// Deserialize converts a JSON-serializable value back to a Go value
func (d *Deserializer) Deserialize(data interface{}) (interface{}, error) {
	return d.deserializeValue(data)
}

func (d *Deserializer) deserializeValue(data interface{}) (interface{}, error) {
	if data == nil {
		return nil, nil
	}

	// Handle primitive JSON types directly
	switch v := data.(type) {
	case bool, float64, string:
		return v, nil

	case []interface{}:
		// Check if this is a special RPC type (array format: ["type", value])
		if len(v) >= 2 {
			if typeStr, ok := v[0].(string); ok {
				switch typeStr {
				case "bigint", "date", "bytes", "error", "import", "export", "promise":
					return d.deserializeArrayType(v)
				}
			}
		}
		// Regular array
		return d.deserializeArray(v)

	case map[string]interface{}:
		// Regular object
		return d.deserializeObject(v)

	default:
		return v, nil
	}
}

func (d *Deserializer) deserializeArrayType(data []interface{}) (interface{}, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("invalid array type format: need at least 2 elements")
	}

	typeStr, ok := data[0].(string)
	if !ok {
		return nil, fmt.Errorf("invalid array type format: first element must be string")
	}

	value := data[1]

	switch typeStr {
	case "bigint":
		if str, ok := value.(string); ok {
			bigInt := new(big.Int)
			if _, success := bigInt.SetString(str, 10); success {
				return bigInt, nil
			}
		}
		return nil, fmt.Errorf("invalid bigint value: %v", value)

	case "date":
		if millis, ok := value.(float64); ok {
			return time.UnixMilli(int64(millis)), nil
		}
		return nil, fmt.Errorf("invalid date value: %v", value)

	case "bytes":
		if str, ok := value.(string); ok {
			decoded, err := base64.StdEncoding.DecodeString(str)
			if err != nil {
				return nil, fmt.Errorf("failed to decode base64 bytes: %w", err)
			}
			return decoded, nil
		}
		return nil, fmt.Errorf("invalid bytes value: %v", value)

	case "error":
		return d.deserializeError(value, nil)

	case "import":
		if d.ImportFunc == nil {
			return nil, fmt.Errorf("cannot deserialize import: no import function provided")
		}
		if id, ok := value.(float64); ok {
			return d.ImportFunc(ImportID(id), false)
		}
		return nil, fmt.Errorf("invalid import ID: %v", value)

	case "export":
		// Exports become import references from our perspective
		if d.ImportFunc == nil {
			return nil, fmt.Errorf("cannot deserialize export: no import function provided")
		}
		if id, ok := value.(float64); ok {
			return d.ImportFunc(ImportID(id), false)
		}
		return nil, fmt.Errorf("invalid export ID: %v", value)

	case "promise":
		// Promises become import references with isPromise=true
		if d.ImportFunc == nil {
			return nil, fmt.Errorf("cannot deserialize promise: no import function provided")
		}
		if id, ok := value.(float64); ok {
			return d.ImportFunc(ImportID(id), true)
		}
		return nil, fmt.Errorf("invalid promise ID: %v", value)

	default:
		if d.Options.CreateUnknownTypes {
			return fmt.Errorf("unknown serialized type: %s", typeStr), nil
		}
		return nil, fmt.Errorf("unknown serialized type: %s", typeStr)
	}
}

func (d *Deserializer) deserializeError(value interface{}, meta interface{}) (interface{}, error) {
	if valueMap, ok := value.(map[string]interface{}); ok {
		message := ""
		if msg, ok := valueMap["message"].(string); ok {
			message = msg
		}

		// Check if it's an RpcError (with "name" field indicating type)
		if errorName, ok := valueMap["name"].(string); ok {
			rpcErr := &RpcError{
				Type:    errorName,
				Message: message,
			}

			if code, ok := valueMap["code"].(float64); ok {
				rpcErr.Code = int(code)
			}

			if stack, ok := valueMap["stack"].(string); ok {
				rpcErr.Stack = stack
			}

			return rpcErr, nil
		}

		// Regular error
		return fmt.Errorf("%s", message), nil
	}

	return fmt.Errorf("invalid error value: %v", value), nil
}

func (d *Deserializer) deserializeObject(data map[string]interface{}) (interface{}, error) {
	result := make(map[string]interface{})

	for key, value := range data {
		deserialized, err := d.deserializeValue(value)
		if err != nil {
			return nil, fmt.Errorf("error deserializing object key %s: %w", key, err)
		}
		result[key] = deserialized
	}

	return result, nil
}

func (d *Deserializer) deserializeArray(data []interface{}) (interface{}, error) {
	result := make([]interface{}, len(data))

	for i, item := range data {
		deserialized, err := d.deserializeValue(item)
		if err != nil {
			return nil, fmt.Errorf("error deserializing array element %d: %w", i, err)
		}
		result[i] = deserialized
	}

	return result, nil
}

// JSON utility functions

// ToJSON converts a value to JSON using the serializer
func (s *Serializer) ToJSON(value interface{}) ([]byte, error) {
	serialized, err := s.Serialize(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(serialized)
}

// FromJSON converts JSON data to a Go value using the deserializer
func (d *Deserializer) FromJSON(data []byte) (interface{}, error) {
	var intermediate interface{}
	if err := json.Unmarshal(data, &intermediate); err != nil {
		return nil, fmt.Errorf("JSON unmarshal error: %w", err)
	}
	return d.Deserialize(intermediate)
}