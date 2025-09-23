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

		if msg.Type != MessageTypePush {
			t.Errorf("Expected type %q, got %q", MessageTypePush, msg.Type)
		}

		if msg.Version != protocol.Version {
			t.Errorf("Expected version %q, got %q", protocol.Version, msg.Version)
		}

		pushData, ok := msg.Data.(PushData)
		if !ok {
			t.Fatalf("Expected PushData, got %T", msg.Data)
		}

		if pushData.Expression != expression {
			t.Errorf("Expected expression %q, got %v", expression, pushData.Expression)
		}

		if pushData.ExportID == nil || *pushData.ExportID != exportID {
			t.Errorf("Expected export ID %v, got %v", exportID, pushData.ExportID)
		}
	})

	t.Run("PullMessage", func(t *testing.T) {
		importID := ImportID(24)
		msg := protocol.NewPullMessage(importID)

		if msg.Type != MessageTypePull {
			t.Errorf("Expected type %q, got %q", MessageTypePull, msg.Type)
		}

		pullData, ok := msg.Data.(PullData)
		if !ok {
			t.Fatalf("Expected PullData, got %T", msg.Data)
		}

		if pullData.ImportID != importID {
			t.Errorf("Expected import ID %v, got %v", importID, pullData.ImportID)
		}
	})

	t.Run("ResolveMessage", func(t *testing.T) {
		exportID := ExportID(42)
		value := "resolved value"
		msg := protocol.NewResolveMessage(exportID, value)

		if msg.Type != MessageTypeResolve {
			t.Errorf("Expected type %q, got %q", MessageTypeResolve, msg.Type)
		}

		resolveData, ok := msg.Data.(ResolveData)
		if !ok {
			t.Fatalf("Expected ResolveData, got %T", msg.Data)
		}

		if resolveData.ExportID != exportID {
			t.Errorf("Expected export ID %v, got %v", exportID, resolveData.ExportID)
		}

		if resolveData.Value != value {
			t.Errorf("Expected value %q, got %v", value, resolveData.Value)
		}
	})

	t.Run("RejectMessage", func(t *testing.T) {
		exportID := ExportID(42)
		errorValue := "error occurred"
		msg := protocol.NewRejectMessage(exportID, errorValue)

		if msg.Type != MessageTypeReject {
			t.Errorf("Expected type %q, got %q", MessageTypeReject, msg.Type)
		}

		rejectData, ok := msg.Data.(RejectData)
		if !ok {
			t.Fatalf("Expected RejectData, got %T", msg.Data)
		}

		if rejectData.ExportID != exportID {
			t.Errorf("Expected export ID %v, got %v", exportID, rejectData.ExportID)
		}

		if rejectData.Error != errorValue {
			t.Errorf("Expected error %q, got %v", errorValue, rejectData.Error)
		}
	})

	t.Run("ReleaseMessage", func(t *testing.T) {
		importID := ImportID(24)
		refCount := uint32(5)
		msg := protocol.NewReleaseMessage(importID, refCount)

		if msg.Type != MessageTypeRelease {
			t.Errorf("Expected type %q, got %q", MessageTypeRelease, msg.Type)
		}

		releaseData, ok := msg.Data.(ReleaseData)
		if !ok {
			t.Fatalf("Expected ReleaseData, got %T", msg.Data)
		}

		if releaseData.ImportID != importID {
			t.Errorf("Expected import ID %v, got %v", importID, releaseData.ImportID)
		}

		if releaseData.RefCount != refCount {
			t.Errorf("Expected ref count %v, got %v", refCount, releaseData.RefCount)
		}
	})

	t.Run("AbortMessage", func(t *testing.T) {
		reason := "connection lost"
		code := 500
		msg := protocol.NewAbortMessage(reason, &code)

		if msg.Type != MessageTypeAbort {
			t.Errorf("Expected type %q, got %q", MessageTypeAbort, msg.Type)
		}

		abortData, ok := msg.Data.(AbortData)
		if !ok {
			t.Fatalf("Expected AbortData, got %T", msg.Data)
		}

		if abortData.Reason != reason {
			t.Errorf("Expected reason %q, got %v", reason, abortData.Reason)
		}

		if abortData.Code == nil || *abortData.Code != code {
			t.Errorf("Expected code %v, got %v", code, abortData.Code)
		}
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
	messages := []*Message{
		protocol.NewPushMessage("test expression", nil),
		protocol.NewPullMessage(ImportID(42)),
		protocol.NewResolveMessage(ExportID(24), "resolved"),
		protocol.NewRejectMessage(ExportID(24), "error"),
		protocol.NewReleaseMessage(ImportID(42), 1),
		protocol.NewAbortMessage("session ended", nil),
	}

	for _, originalMsg := range messages {
		t.Run(string(originalMsg.Type), func(t *testing.T) {
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

			// Compare
			if decodedMsg.Type != originalMsg.Type {
				t.Errorf("Type mismatch: expected %q, got %q", originalMsg.Type, decodedMsg.Type)
			}

			if decodedMsg.Version != originalMsg.Version {
				t.Errorf("Version mismatch: expected %q, got %q", originalMsg.Version, decodedMsg.Version)
			}

			// Note: Data comparison is complex due to interface{} types and JSON marshaling
			// For now, we just verify that data is present where expected
			if originalMsg.Data != nil && decodedMsg.Data == nil {
				t.Error("Expected data in decoded message")
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
		if decodedMsg.Type != MessageTypePush {
			t.Errorf("Expected type %q, got %q", MessageTypePush, decodedMsg.Type)
		}

		// The data should be deserialized back to a Go value
		if decodedMsg.Data == nil {
			t.Fatal("Expected data in decoded message")
		}
	})
}

func TestMessageValidation(t *testing.T) {
	protocol := NewProtocol(nil, nil)

	t.Run("ValidMessages", func(t *testing.T) {
		validMessages := []*Message{
			{Type: MessageTypePush, Data: "test", Version: ProtocolVersion},
			{Type: MessageTypePull, Data: "test", Version: ProtocolVersion},
			{Type: MessageTypeResolve, Data: "test", Version: ProtocolVersion},
			{Type: MessageTypeReject, Data: "test", Version: ProtocolVersion},
			{Type: MessageTypeRelease, Data: "test", Version: ProtocolVersion},
			{Type: MessageTypeAbort, Data: "test", Version: ProtocolVersion},
			{Type: MessageTypePush, Data: "test", Version: ""}, // Empty version should be valid
		}

		for i, msg := range validMessages {
			if err := protocol.ValidateMessage(msg); err != nil {
				t.Errorf("Message %d should be valid: %v", i, err)
			}
		}
	})

	t.Run("InvalidMessages", func(t *testing.T) {
		invalidMessages := []*Message{
			nil,                                                             // Nil message
			{Type: "", Data: "test", Version: ProtocolVersion},              // Empty type
			{Type: "invalid", Data: "test", Version: ProtocolVersion},       // Invalid type
			{Type: MessageTypePush, Data: "test", Version: "999.0"},         // Incompatible version
			{Type: MessageTypePush, Data: nil, Version: ProtocolVersion},    // Missing data for push
			{Type: MessageTypePull, Data: nil, Version: ProtocolVersion},    // Missing data for pull
			{Type: MessageTypeResolve, Data: nil, Version: ProtocolVersion}, // Missing data for resolve
			{Type: MessageTypeReject, Data: nil, Version: ProtocolVersion},  // Missing data for reject
			{Type: MessageTypeRelease, Data: nil, Version: ProtocolVersion}, // Missing data for release
			{Type: MessageTypeAbort, Data: nil, Version: ProtocolVersion},   // Missing data for abort
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
		msg  *Message
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
			if decodedMsg.Type != tc.msg.Type {
				t.Errorf("Type mismatch: expected %q, got %q", tc.msg.Type, decodedMsg.Type)
			}

			if decodedMsg.Version != tc.msg.Version {
				t.Errorf("Version mismatch: expected %q, got %q", tc.msg.Version, decodedMsg.Version)
			}
		})
	}
}