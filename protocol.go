package capnweb

import (
	"encoding/json"
	"fmt"
)

// Protocol constants
const (
	// ProtocolVersion is the current version of the Cap'n Web protocol
	ProtocolVersion = "1.0"

	// MaxMessageSize is the maximum allowed message size (64MB)
	MaxMessageSize = 64 * 1024 * 1024
)

// MessageType represents the different types of RPC messages
type MessageType string

const (
	// MessageTypePush represents a new RPC call or expression evaluation
	MessageTypePush MessageType = "push"

	// MessageTypePull requests the resolution of a promise
	MessageTypePull MessageType = "pull"

	// MessageTypeResolve provides the successful resolution of a promise
	MessageTypeResolve MessageType = "resolve"

	// MessageTypeReject provides the error resolution of a promise
	MessageTypeReject MessageType = "reject"

	// MessageTypeRelease indicates that a reference is no longer needed
	MessageTypeRelease MessageType = "release"

	// MessageTypeAbort indicates that the session should be terminated due to an error
	MessageTypeAbort MessageType = "abort"
)

// Message represents a Cap'n Web protocol message in JavaScript-compatible array format
// Format: [messageType, ...args]
type Message []interface{}

// Note: Message data structures are no longer needed since we use array format
// Messages follow JavaScript format:
// ["push", expression]
// ["pull", importId]
// ["resolve", exportId, value]
// ["reject", exportId, error]
// ["release", importId, refCount]
// ["abort", reason]

// RPC Expression types for the push message

// ExpressionType represents the type of RPC expression
type ExpressionType string

const (
	// ExpressionTypePipeline represents a method call pipeline
	ExpressionTypePipeline ExpressionType = "pipeline"

	// ExpressionTypeRemap represents a map operation
	ExpressionTypeRemap ExpressionType = "remap"

	// ExpressionTypeImport represents an import reference
	ExpressionTypeImport ExpressionType = "import"

	// ExpressionTypeExport represents an export reference
	ExpressionTypeExport ExpressionType = "export"
)

// Expression represents an RPC expression
type Expression struct {
	// Type indicates the expression type
	Type ExpressionType `json:"type"`

	// Data contains the expression-specific data
	Data interface{} `json:"data"`
}

// PipelineExpression represents a method call pipeline
type PipelineExpression struct {
	// ImportID is the target object
	ImportID ImportID `json:"importId"`

	// Path is the property path to the method
	Path PropertyPath `json:"path"`

	// Args are the method arguments (optional)
	Args interface{} `json:"args,omitempty"`
}

// RemapExpression represents a map operation
type RemapExpression struct {
	// ImportID is the target array/object
	ImportID ImportID `json:"importId"`

	// Path is the property path (optional)
	Path PropertyPath `json:"path,omitempty"`

	// Captures are the captured references
	Captures []interface{} `json:"captures"`

	// Instructions are the map operation instructions
	Instructions []interface{} `json:"instructions"`
}

// ImportExpression represents an import reference
type ImportExpression struct {
	// ImportID is the reference ID
	ImportID ImportID `json:"importId"`
}

// ExportExpression represents an export reference
type ExportExpression struct {
	// ExportID is the reference ID
	ExportID ExportID `json:"exportId"`
}

// Protocol handles encoding and decoding of Cap'n Web messages
type Protocol struct {
	// Version is the protocol version to use
	Version string

	// Serializer handles value serialization
	Serializer *Serializer

	// Deserializer handles value deserialization
	Deserializer *Deserializer
}

// NewProtocol creates a new protocol handler
func NewProtocol(serializer *Serializer, deserializer *Deserializer) *Protocol {
	return &Protocol{
		Version:      ProtocolVersion,
		Serializer:   serializer,
		Deserializer: deserializer,
	}
}

// EncodeMessage encodes a message to JSON bytes
func (p *Protocol) EncodeMessage(msg Message) ([]byte, error) {
	if len(msg) == 0 {
		return nil, fmt.Errorf("empty message")
	}

	// Create a copy for serialization
	encodedMsg := make(Message, len(msg))
	copy(encodedMsg, msg)

	// Serialize message arguments if we have a serializer
	if p.Serializer != nil {
		for i := 1; i < len(encodedMsg); i++ { // Skip the message type at index 0
			if encodedMsg[i] != nil {
				serialized, err := p.Serializer.Serialize(encodedMsg[i])
				if err != nil {
					return nil, fmt.Errorf("failed to serialize message argument %d: %w", i, err)
				}
				encodedMsg[i] = serialized
			}
		}
	}

	data, err := json.Marshal(encodedMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	if len(data) > MaxMessageSize {
		return nil, fmt.Errorf("message too large: %d bytes (max %d)", len(data), MaxMessageSize)
	}

	return data, nil
}

// DecodeMessage decodes JSON bytes to a message
func (p *Protocol) DecodeMessage(data []byte) (Message, error) {
	if len(data) > MaxMessageSize {
		return nil, fmt.Errorf("message too large: %d bytes (max %d)", len(data), MaxMessageSize)
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	// Validate message
	if err := p.ValidateMessage(msg); err != nil {
		return nil, fmt.Errorf("invalid message: %w", err)
	}

	// Deserialize message arguments if we have a deserializer
	if p.Deserializer != nil {
		for i := 1; i < len(msg); i++ { // Skip the message type at index 0
			if msg[i] != nil {
				deserialized, err := p.Deserializer.Deserialize(msg[i])
				if err != nil {
					return nil, fmt.Errorf("failed to deserialize message argument %d: %w", i, err)
				}
				msg[i] = deserialized
			}
		}
	}

	return msg, nil
}

// ValidateMessage validates a message structure
func (p *Protocol) ValidateMessage(msg Message) error {
	if msg == nil || len(msg) == 0 {
		return fmt.Errorf("message is nil or empty")
	}

	// First element must be the message type
	msgType, ok := msg[0].(string)
	if !ok {
		return fmt.Errorf("message type must be a string")
	}

	// Validate message type
	switch MessageType(msgType) {
	case MessageTypePush, MessageTypePull, MessageTypeResolve,
		 MessageTypeReject, MessageTypeRelease, MessageTypeAbort:
		// Valid types
	default:
		return fmt.Errorf("unknown message type: %s", msgType)
	}

	// Type-specific validation
	if err := p.validateArrayMessageData(msg); err != nil {
		return fmt.Errorf("invalid message data: %w", err)
	}

	return nil
}

// validateArrayMessageData validates type-specific message data for array format
func (p *Protocol) validateArrayMessageData(msg Message) error {
	msgType := MessageType(msg[0].(string))

	switch msgType {
	case MessageTypePush:
		// Format: ["push", expression]
		if len(msg) < 2 {
			return fmt.Errorf("push message requires expression")
		}
		return nil

	case MessageTypePull:
		// Format: ["pull", importID]
		if len(msg) < 2 {
			return fmt.Errorf("pull message requires importID")
		}
		return nil

	case MessageTypeResolve:
		// Format: ["resolve", exportID, value]
		if len(msg) < 3 {
			return fmt.Errorf("resolve message requires exportID and value")
		}
		return nil

	case MessageTypeReject:
		// Format: ["reject", exportID, error]
		if len(msg) < 3 {
			return fmt.Errorf("reject message requires exportID and error")
		}
		return nil

	case MessageTypeRelease:
		// Format: ["release", importID, refCount]
		if len(msg) < 3 {
			return fmt.Errorf("release message requires importID and refCount")
		}
		return nil

	case MessageTypeAbort:
		// Format: ["abort", reason]
		if len(msg) < 2 {
			return fmt.Errorf("abort message requires reason")
		}
		return nil

	default:
		return fmt.Errorf("unknown message type for validation: %s", msgType)
	}
}

// IsVersionCompatible checks if a protocol version is compatible
func (p *Protocol) IsVersionCompatible(version string) bool {
	// For now, we only support exact version matching
	// In the future, this could implement semantic versioning compatibility
	return version == p.Version || version == ""
}

// Message creation helpers

// NewPushMessage creates a new push message
// JavaScript format: ["push", expression]
func (p *Protocol) NewPushMessage(expression interface{}, exportID *ExportID) Message {
	return Message{"push", expression}
}

// NewPullMessage creates a new pull message
// JavaScript format: ["pull", importID]
func (p *Protocol) NewPullMessage(importID ImportID) Message {
	return Message{"pull", importID}
}

// NewResolveMessage creates a new resolve message
// JavaScript format: ["resolve", exportID, value]
func (p *Protocol) NewResolveMessage(exportID ExportID, value interface{}) Message {
	return Message{"resolve", exportID, value}
}

// NewRejectMessage creates a new reject message
// JavaScript format: ["reject", exportID, error]
func (p *Protocol) NewRejectMessage(exportID ExportID, error interface{}) Message {
	return Message{"reject", exportID, error}
}

// NewReleaseMessage creates a new release message
// JavaScript format: ["release", importID, refCount]
func (p *Protocol) NewReleaseMessage(importID ImportID, refCount uint32) Message {
	return Message{"release", importID, refCount}
}

// NewAbortMessage creates a new abort message
// JavaScript format: ["abort", reason]
func (p *Protocol) NewAbortMessage(reason interface{}, code *int) Message {
	return Message{"abort", reason}
}

// Expression creation helpers

// NewPipelineExpression creates a pipeline expression
func NewPipelineExpression(importID ImportID, path PropertyPath, args interface{}) *Expression {
	return &Expression{
		Type: ExpressionTypePipeline,
		Data: PipelineExpression{
			ImportID: importID,
			Path:     path,
			Args:     args,
		},
	}
}

// NewRemapExpression creates a remap expression
func NewRemapExpression(importID ImportID, path PropertyPath, captures []interface{}, instructions []interface{}) *Expression {
	return &Expression{
		Type: ExpressionTypeRemap,
		Data: RemapExpression{
			ImportID:     importID,
			Path:         path,
			Captures:     captures,
			Instructions: instructions,
		},
	}
}

// NewImportExpression creates an import expression
func NewImportExpression(importID ImportID) *Expression {
	return &Expression{
		Type: ExpressionTypeImport,
		Data: ImportExpression{
			ImportID: importID,
		},
	}
}

// NewExportExpression creates an export expression
func NewExportExpression(exportID ExportID) *Expression {
	return &Expression{
		Type: ExpressionTypeExport,
		Data: ExportExpression{
			ExportID: exportID,
		},
	}
}

// Protocol statistics

// MessageStats contains statistics about protocol message processing
type MessageStats struct {
	// MessagesEncoded is the total number of messages encoded
	MessagesEncoded uint64 `json:"messagesEncoded"`

	// MessagesDecoded is the total number of messages decoded
	MessagesDecoded uint64 `json:"messagesDecoded"`

	// BytesEncoded is the total bytes encoded
	BytesEncoded uint64 `json:"bytesEncoded"`

	// BytesDecoded is the total bytes decoded
	BytesDecoded uint64 `json:"bytesDecoded"`

	// EncodingErrors is the number of encoding errors
	EncodingErrors uint64 `json:"encodingErrors"`

	// DecodingErrors is the number of decoding errors
	DecodingErrors uint64 `json:"decodingErrors"`

	// ValidationErrors is the number of validation errors
	ValidationErrors uint64 `json:"validationErrors"`

	// MessagesByType contains counts by message type
	MessagesByType map[MessageType]uint64 `json:"messagesByType"`
}

// NewMessageStats creates a new MessageStats instance
func NewMessageStats() *MessageStats {
	return &MessageStats{
		MessagesByType: make(map[MessageType]uint64),
	}
}

// RecordEncoded records a successful encoding
func (s *MessageStats) RecordEncoded(msgType MessageType, bytes int) {
	s.MessagesEncoded++
	s.BytesEncoded += uint64(bytes)
	s.MessagesByType[msgType]++
}

// RecordDecoded records a successful decoding
func (s *MessageStats) RecordDecoded(msgType MessageType, bytes int) {
	s.MessagesDecoded++
	s.BytesDecoded += uint64(bytes)
	s.MessagesByType[msgType]++
}

// RecordEncodingError records an encoding error
func (s *MessageStats) RecordEncodingError() {
	s.EncodingErrors++
}

// RecordDecodingError records a decoding error
func (s *MessageStats) RecordDecodingError() {
	s.DecodingErrors++
}

// RecordValidationError records a validation error
func (s *MessageStats) RecordValidationError() {
	s.ValidationErrors++
}

// StatsProtocol wraps a Protocol with statistics tracking
type StatsProtocol struct {
	*Protocol
	Stats *MessageStats
}

// NewStatsProtocol creates a new protocol with statistics tracking
func NewStatsProtocol(serializer *Serializer, deserializer *Deserializer) *StatsProtocol {
	return &StatsProtocol{
		Protocol: NewProtocol(serializer, deserializer),
		Stats:    NewMessageStats(),
	}
}

// EncodeMessage encodes a message and tracks statistics
func (sp *StatsProtocol) EncodeMessage(msg Message) ([]byte, error) {
	data, err := sp.Protocol.EncodeMessage(msg)
	if err != nil {
		sp.Stats.RecordEncodingError()
		return nil, err
	}

	msgType := MessageType("unknown")
	if len(msg) > 0 {
		if typeStr, ok := msg[0].(string); ok {
			msgType = MessageType(typeStr)
		}
	}

	sp.Stats.RecordEncoded(msgType, len(data))
	return data, nil
}

// DecodeMessage decodes a message and tracks statistics
func (sp *StatsProtocol) DecodeMessage(data []byte) (Message, error) {
	msg, err := sp.Protocol.DecodeMessage(data)
	if err != nil {
		sp.Stats.RecordDecodingError()
		return nil, err
	}

	msgType := MessageType("unknown")
	if len(msg) > 0 {
		if typeStr, ok := msg[0].(string); ok {
			msgType = MessageType(typeStr)
		}
	}

	sp.Stats.RecordDecoded(msgType, len(data))
	return msg, nil
}