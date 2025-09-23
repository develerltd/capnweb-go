package capnweb

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMessageTypes(t *testing.T) {
	expectedTypes := map[MessageType]string{
		MessageTypePush:    "push",
		MessageTypePull:    "pull",
		MessageTypeResolve: "resolve",
		MessageTypeReject:  "reject",
		MessageTypeRelease: "release",
		MessageTypeAbort:   "abort",
	}

	for msgType, expected := range expectedTypes {
		if string(msgType) != expected {
			t.Errorf("MessageType %v should be %q, got %q", msgType, expected, string(msgType))
		}
	}
}

func TestExpressionTypes(t *testing.T) {
	expectedTypes := map[ExpressionType]string{
		ExpressionTypePipeline: "pipeline",
		ExpressionTypeRemap:    "remap",
		ExpressionTypeImport:   "import",
		ExpressionTypeExport:   "export",
	}

	for exprType, expected := range expectedTypes {
		if string(exprType) != expected {
			t.Errorf("ExpressionType %v should be %q, got %q", exprType, expected, string(exprType))
		}
	}
}

func TestProtocolCreation(t *testing.T) {
	protocol := NewProtocol(nil, nil)

	if protocol.Version != ProtocolVersion {
		t.Errorf("Expected version %q, got %q", ProtocolVersion, protocol.Version)
	}

	if protocol.Serializer != nil {
		t.Error("Expected nil serializer")
	}

	if protocol.Deserializer != nil {
		t.Error("Expected nil deserializer")
	}
}

func TestMessageCreationHelpers(t *testing.T) {
	protocol := NewProtocol(nil, nil)

	t.Run("PushMessage", func(t *testing.T) {
		exportID := ExportID(42)
		expression := "test expression"
		msg := protocol.NewPushMessage(expression, &exportID)

		if len(msg) < 2 {
			t.Fatalf("Expected at least 2 elements in message, got %d", len(msg))
		}

		if msg[0] != "push" {
			t.Errorf("Expected type %q, got %q", "push", msg[0])
		}

		if msg[1] != expression {
			t.Errorf("Expected expression %q, got %v", expression, msg[1])
		}
	})

	t.Run("PullMessage", func(t *testing.T) {
		importID := ImportID(24)
		msg := protocol.NewPullMessage(importID)

		if len(msg) < 2 {
			t.Fatalf("Expected at least 2 elements in message, got %d", len(msg))
		}

		if msg[0] != "pull" {
			t.Errorf("Expected type %q, got %q", "pull", msg[0])
		}

		if msg[1] != importID {
			t.Errorf("Expected import ID %v, got %v", importID, msg[1])
		}
	})

	t.Run("ResolveMessage", func(t *testing.T) {
		exportID := ExportID(42)
		value := "resolved value"
		msg := protocol.NewResolveMessage(exportID, value)

		if len(msg) < 3 {
			t.Fatalf("Expected at least 3 elements in message, got %d", len(msg))
		}

		if msg[0] != "resolve" {
			t.Errorf("Expected type %q, got %q", "resolve", msg[0])
		}

		if msg[1] != exportID {
			t.Errorf("Expected export ID %v, got %v", exportID, msg[1])
		}

		if msg[2] != value {
			t.Errorf("Expected value %q, got %v", value, msg[2])
		}
	})

	t.Run("RejectMessage", func(t *testing.T) {
		exportID := ExportID(42)
		errorValue := "error occurred"
		msg := protocol.NewRejectMessage(exportID, errorValue)

		if len(msg) < 3 {
			t.Fatalf("Expected at least 3 elements in message, got %d", len(msg))
		}

		if msg[0] != "reject" {
			t.Errorf("Expected type %q, got %q", "reject", msg[0])
		}

		if msg[1] != exportID {
			t.Errorf("Expected export ID %v, got %v", exportID, msg[1])
		}

		if msg[2] != errorValue {
			t.Errorf("Expected error %q, got %v", errorValue, msg[2])
		}
	})

	t.Run("ReleaseMessage", func(t *testing.T) {
		importID := ImportID(24)
		refCount := uint32(5)
		msg := protocol.NewReleaseMessage(importID, refCount)

		if len(msg) < 3 {
			t.Fatalf("Expected at least 3 elements in message, got %d", len(msg))
		}

		if msg[0] != "release" {
			t.Errorf("Expected type %q, got %q", "release", msg[0])
		}

		if msg[1] != importID {
			t.Errorf("Expected import ID %v, got %v", importID, msg[1])
		}

		if msg[2] != refCount {
			t.Errorf("Expected ref count %v, got %v", refCount, msg[2])
		}
	})

	t.Run("AbortMessage", func(t *testing.T) {
		reason := "connection lost"
		code := 500
		msg := protocol.NewAbortMessage(reason, &code)

		if len(msg) < 2 {
			t.Fatalf("Expected at least 2 elements in message, got %d", len(msg))
		}

		if msg[0] != "abort" {
			t.Errorf("Expected type %q, got %q", "abort", msg[0])
		}

		if msg[1] != reason {
			t.Errorf("Expected reason %q, got %v", reason, msg[1])
		}

		// Note: code parameter is ignored in the new array format per protocol.go
	})
}

func TestExpressionCreationHelpers(t *testing.T) {
	t.Run("PipelineExpression", func(t *testing.T) {
		importID := ImportID(42)
		path := PropertyPath{"user", "getName"}
		args := []interface{}{"arg1", "arg2"}

		expr := NewPipelineExpression(importID, path, args)

		if expr.Type != ExpressionTypePipeline {
			t.Errorf("Expected type %q, got %q", ExpressionTypePipeline, expr.Type)
		}

		pipelineData, ok := expr.Data.(PipelineExpression)
		if !ok {
			t.Fatalf("Expected PipelineExpression, got %T", expr.Data)
		}

		if pipelineData.ImportID != importID {
			t.Errorf("Expected import ID %v, got %v", importID, pipelineData.ImportID)
		}

		if !reflect.DeepEqual(pipelineData.Path, path) {
			t.Errorf("Expected path %v, got %v", path, pipelineData.Path)
		}

		if !reflect.DeepEqual(pipelineData.Args, args) {
			t.Errorf("Expected args %v, got %v", args, pipelineData.Args)
		}
	})

	t.Run("RemapExpression", func(t *testing.T) {
		importID := ImportID(24)
		path := PropertyPath{"items"}
		captures := []interface{}{"capture1", "capture2"}
		instructions := []interface{}{"instruction1", "instruction2"}

		expr := NewRemapExpression(importID, path, captures, instructions)

		if expr.Type != ExpressionTypeRemap {
			t.Errorf("Expected type %q, got %q", ExpressionTypeRemap, expr.Type)
		}

		remapData, ok := expr.Data.(RemapExpression)
		if !ok {
			t.Fatalf("Expected RemapExpression, got %T", expr.Data)
		}

		if remapData.ImportID != importID {
			t.Errorf("Expected import ID %v, got %v", importID, remapData.ImportID)
		}

		if !reflect.DeepEqual(remapData.Path, path) {
			t.Errorf("Expected path %v, got %v", path, remapData.Path)
		}

		if !reflect.DeepEqual(remapData.Captures, captures) {
			t.Errorf("Expected captures %v, got %v", captures, remapData.Captures)
		}

		if !reflect.DeepEqual(remapData.Instructions, instructions) {
			t.Errorf("Expected instructions %v, got %v", instructions, remapData.Instructions)
		}
	})

	t.Run("ImportExpression", func(t *testing.T) {
		importID := ImportID(42)
		expr := NewImportExpression(importID)

		if expr.Type != ExpressionTypeImport {
			t.Errorf("Expected type %q, got %q", ExpressionTypeImport, expr.Type)
		}

		importData, ok := expr.Data.(ImportExpression)
		if !ok {
			t.Fatalf("Expected ImportExpression, got %T", expr.Data)
		}

		if importData.ImportID != importID {
			t.Errorf("Expected import ID %v, got %v", importID, importData.ImportID)
		}
	})

	t.Run("ExportExpression", func(t *testing.T) {
		exportID := ExportID(24)
		expr := NewExportExpression(exportID)

		if expr.Type != ExpressionTypeExport {
			t.Errorf("Expected type %q, got %q", ExpressionTypeExport, expr.Type)
		}

		exportData, ok := expr.Data.(ExportExpression)
		if !ok {
			t.Fatalf("Expected ExportExpression, got %T", expr.Data)
		}

		if exportData.ExportID != exportID {
			t.Errorf("Expected export ID %v, got %v", exportID, exportData.ExportID)
		}
	})
}

func TestMessageEncodeDecodeWithoutSerialization(t *testing.T) {
	protocol := NewProtocol(nil, nil)

	// Test each message type
	messages := []Message{
		protocol.NewPushMessage("test expression", nil),
		protocol.NewPullMessage(ImportID(42)),
		protocol.NewResolveMessage(ExportID(24), "resolved"),
		protocol.NewRejectMessage(ExportID(24), "error"),
		protocol.NewReleaseMessage(ImportID(42), 1),
		protocol.NewAbortMessage("session ended", nil),
	}

	for _, originalMsg := range messages {
		msgType := originalMsg[0].(string)
		t.Run(msgType, func(t *testing.T) {
			// Encode
			data, err := protocol.EncodeMessage(originalMsg)
			if err != nil {
				t.Fatalf("Failed to encode message: %v", err)
			}

			// Decode
			decodedMsg, err := protocol.DecodeMessage(data)
			if err != nil {
				t.Fatalf("Failed to decode message: %v", err)
			}

			// Compare message type
			if decodedMsg[0] != originalMsg[0] {
				t.Errorf("Type mismatch: expected %q, got %q", originalMsg[0], decodedMsg[0])
			}

			// Compare message length
			if len(decodedMsg) != len(originalMsg) {
				t.Errorf("Length mismatch: expected %d, got %d", len(originalMsg), len(decodedMsg))
			}

			// Basic comparison of message contents
			// Note: Deep comparison is complex due to JSON marshaling effects on numeric types
			for i := 0; i < len(originalMsg) && i < len(decodedMsg); i++ {
				if originalMsg[i] != nil && decodedMsg[i] == nil {
					t.Errorf("Expected data at index %d in decoded message", i)
				}
			}
		})
	}
}

func TestMessageEncodeDecodeWithSerialization(t *testing.T) {
	var exportedID ExportID
	serializer := NewSerializer(func(value interface{}) (ExportID, error) {
		exportedID++
		return exportedID, nil
	})

	var importedStubs []interface{}
	deserializer := NewDeserializer(func(id ImportID, isPromise bool) (interface{}, error) {
		if int(id) <= len(importedStubs) {
			return importedStubs[int(id)-1], nil
		}
		stub := &MockStub{importID: &id}
		importedStubs = append(importedStubs, stub)
		return stub, nil
	})

	protocol := NewProtocol(serializer, deserializer)

	t.Run("PushMessageWithComplexData", func(t *testing.T) {
		complexData := map[string]interface{}{
			"number": 42,
			"string": "hello",
			"array":  []interface{}{1, 2, 3},
			"nested": map[string]interface{}{
				"value": true,
			},
		}

		originalMsg := protocol.NewPushMessage(complexData, nil)

		// Encode
		data, err := protocol.EncodeMessage(originalMsg)
		if err != nil {
			t.Fatalf("Failed to encode message: %v", err)
		}

		// Decode
		decodedMsg, err := protocol.DecodeMessage(data)
		if err != nil {
			t.Fatalf("Failed to decode message: %v", err)
		}

		// Verify structure
		if len(decodedMsg) < 2 {
			t.Fatalf("Expected at least 2 elements in message, got %d", len(decodedMsg))
		}

		if decodedMsg[0] != "push" {
			t.Errorf("Expected type %q, got %q", "push", decodedMsg[0])
		}

		// The data should be deserialized back to a Go value
		if decodedMsg[1] == nil {
			t.Fatal("Expected data in decoded message")
		}
	})
}

func TestMessageValidation(t *testing.T) {
	protocol := NewProtocol(nil, nil)

	t.Run("ValidMessages", func(t *testing.T) {
		validMessages := []Message{
			{"push", "test"},
			{"pull", ImportID(42)},
			{"resolve", ExportID(24), "resolved"},
			{"reject", ExportID(24), "error"},
			{"release", ImportID(42), uint32(1)},
			{"abort", "session ended"},
		}

		for i, msg := range validMessages {
			if err := protocol.ValidateMessage(msg); err != nil {
				t.Errorf("Message %d should be valid: %v", i, err)
			}
		}
	})

	t.Run("InvalidMessages", func(t *testing.T) {
		invalidMessages := []Message{
			nil,                    // Nil message
			{},                     // Empty message
			{""},                   // Empty type
			{"invalid"},            // Invalid type with no data
			{"push"},               // Missing data for push
			{"pull"},               // Missing data for pull
			{"resolve", ExportID(24)}, // Missing value for resolve
			{"reject", ExportID(24)},  // Missing error for reject
			{"release", ImportID(42)}, // Missing refCount for release
			{"abort"},              // Missing reason for abort
			{123, "test"},          // Non-string type
		}

		for i, msg := range invalidMessages {
			if err := protocol.ValidateMessage(msg); err == nil {
				t.Errorf("Message %d should be invalid: %+v", i, msg)
			}
		}
	})
}

func TestVersionCompatibility(t *testing.T) {
	protocol := NewProtocol(nil, nil)

	t.Run("CompatibleVersions", func(t *testing.T) {
		compatibleVersions := []string{
			ProtocolVersion,
			"", // Empty version should be compatible
		}

		for _, version := range compatibleVersions {
			if !protocol.IsVersionCompatible(version) {
				t.Errorf("Version %q should be compatible", version)
			}
		}
	})

	t.Run("IncompatibleVersions", func(t *testing.T) {
		incompatibleVersions := []string{
			"999.0",
			"0.5",
			"2.0",
			"invalid",
		}

		for _, version := range incompatibleVersions {
			if protocol.IsVersionCompatible(version) {
				t.Errorf("Version %q should be incompatible", version)
			}
		}
	})
}

func TestMessageSizeLimit(t *testing.T) {
	protocol := NewProtocol(nil, nil)

	t.Run("MessageTooLarge", func(t *testing.T) {
		// Create a large message
		largeData := make([]byte, MaxMessageSize+1)
		for i := range largeData {
			largeData[i] = 'A'
		}

		msg := protocol.NewPushMessage(string(largeData), nil)

		_, err := protocol.EncodeMessage(msg)
		if err == nil {
			t.Error("Expected error for message too large")
		}
	})

	t.Run("MessageAtLimit", func(t *testing.T) {
		// Create a message close to the limit (but account for JSON overhead)
		data := make([]byte, MaxMessageSize/2) // Safe size accounting for JSON structure
		for i := range data {
			data[i] = 'A'
		}

		msg := protocol.NewPushMessage(string(data), nil)

		encodedData, err := protocol.EncodeMessage(msg)
		if err != nil {
			t.Errorf("Message should encode successfully: %v", err)
		}

		_, err = protocol.DecodeMessage(encodedData)
		if err != nil {
			t.Errorf("Message should decode successfully: %v", err)
		}
	})
}

func TestMessageStats(t *testing.T) {
	stats := NewMessageStats()

	// Test initial state
	if stats.MessagesEncoded != 0 {
		t.Errorf("Expected 0 messages encoded, got %d", stats.MessagesEncoded)
	}

	if stats.MessagesDecoded != 0 {
		t.Errorf("Expected 0 messages decoded, got %d", stats.MessagesDecoded)
	}

	if len(stats.MessagesByType) != 0 {
		t.Errorf("Expected empty messages by type, got %v", stats.MessagesByType)
	}

	// Record some statistics
	stats.RecordEncoded(MessageTypePush, 100)
	stats.RecordDecoded(MessageTypePull, 50)
	stats.RecordEncodingError()
	stats.RecordDecodingError()
	stats.RecordValidationError()

	// Verify statistics
	if stats.MessagesEncoded != 1 {
		t.Errorf("Expected 1 message encoded, got %d", stats.MessagesEncoded)
	}

	if stats.BytesEncoded != 100 {
		t.Errorf("Expected 100 bytes encoded, got %d", stats.BytesEncoded)
	}

	if stats.MessagesDecoded != 1 {
		t.Errorf("Expected 1 message decoded, got %d", stats.MessagesDecoded)
	}

	if stats.BytesDecoded != 50 {
		t.Errorf("Expected 50 bytes decoded, got %d", stats.BytesDecoded)
	}

	if stats.EncodingErrors != 1 {
		t.Errorf("Expected 1 encoding error, got %d", stats.EncodingErrors)
	}

	if stats.DecodingErrors != 1 {
		t.Errorf("Expected 1 decoding error, got %d", stats.DecodingErrors)
	}

	if stats.ValidationErrors != 1 {
		t.Errorf("Expected 1 validation error, got %d", stats.ValidationErrors)
	}

	if stats.MessagesByType[MessageTypePush] != 1 {
		t.Errorf("Expected 1 push message, got %d", stats.MessagesByType[MessageTypePush])
	}

	if stats.MessagesByType[MessageTypePull] != 1 {
		t.Errorf("Expected 1 pull message, got %d", stats.MessagesByType[MessageTypePull])
	}
}

func TestStatsProtocol(t *testing.T) {
	statsProtocol := NewStatsProtocol(nil, nil)

	msg := statsProtocol.NewPushMessage("test", nil)

	// Encode and verify stats
	data, err := statsProtocol.EncodeMessage(msg)
	if err != nil {
		t.Fatalf("Failed to encode message: %v", err)
	}

	if statsProtocol.Stats.MessagesEncoded != 1 {
		t.Errorf("Expected 1 encoded message, got %d", statsProtocol.Stats.MessagesEncoded)
	}

	if statsProtocol.Stats.BytesEncoded != uint64(len(data)) {
		t.Errorf("Expected %d bytes encoded, got %d", len(data), statsProtocol.Stats.BytesEncoded)
	}

	// Decode and verify stats
	_, err = statsProtocol.DecodeMessage(data)
	if err != nil {
		t.Fatalf("Failed to decode message: %v", err)
	}

	if statsProtocol.Stats.MessagesDecoded != 1 {
		t.Errorf("Expected 1 decoded message, got %d", statsProtocol.Stats.MessagesDecoded)
	}

	if statsProtocol.Stats.BytesDecoded != uint64(len(data)) {
		t.Errorf("Expected %d bytes decoded, got %d", len(data), statsProtocol.Stats.BytesDecoded)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	// Test that our message structures can round-trip through JSON
	protocol := NewProtocol(nil, nil)

	testCases := []struct {
		name string
		msg  Message
	}{
		{
			name: "PushMessage",
			msg:  protocol.NewPushMessage(map[string]interface{}{"test": "value"}, nil),
		},
		{
			name: "PullMessage",
			msg:  protocol.NewPullMessage(ImportID(42)),
		},
		{
			name: "ResolveMessage",
			msg:  protocol.NewResolveMessage(ExportID(24), "resolved value"),
		},
		{
			name: "RejectMessage",
			msg:  protocol.NewRejectMessage(ExportID(24), "error message"),
		},
		{
			name: "ReleaseMessage",
			msg:  protocol.NewReleaseMessage(ImportID(42), 3),
		},
		{
			name: "AbortMessage",
			msg:  protocol.NewAbortMessage("connection lost", nil),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Encode to JSON manually (to test raw JSON compatibility)
			jsonData, err := json.Marshal(tc.msg)
			if err != nil {
				t.Fatalf("Failed to marshal to JSON: %v", err)
			}

			// Decode from JSON manually
			var decodedMsg Message
			if err := json.Unmarshal(jsonData, &decodedMsg); err != nil {
				t.Fatalf("Failed to unmarshal from JSON: %v", err)
			}

			// Basic structure should match
			if len(decodedMsg) != len(tc.msg) {
				t.Errorf("Length mismatch: expected %d, got %d", len(tc.msg), len(decodedMsg))
			}

			if len(decodedMsg) > 0 && decodedMsg[0] != tc.msg[0] {
				t.Errorf("Type mismatch: expected %q, got %q", tc.msg[0], decodedMsg[0])
			}
		})
	}
}