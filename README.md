# Cap'n Web Go Library

A Go implementation of the Cap'n Web RPC system, providing object-capability RPC with promise pipelining.

## Current Status

This is an early implementation focusing on the foundation layers. Currently implemented:

### ✅ Phase 1.1: Transport Interface

- **Core Transport Interface**: Bidirectional message transport abstraction
- **Memory Transport**: In-memory transport implementation for testing
- **Transport Features**:
  - Context-based cancellation
  - Message size limits (64MB)
  - Statistics tracking
  - Graceful shutdown and abort handling
  - Thread-safe operations

### ✅ Phase 1.2: Basic Types

- **RpcTarget Interface**: Objects that can be passed by reference
- **Export/Import IDs**: Reference tracking for remote objects
- **PropertyPath**: Navigation through object properties
- **RPC Error Types**: Comprehensive error handling system
- **Value Type System**: Type classification and serialization hints
- **Type Utilities**: Sets, predicates, and analysis functions

### ✅ Phase 1.3: Serialization Foundation

- **JSON-based Serialization**: Extended JSON with RPC type support
- **Primitive Type Support**: All basic Go types (bool, numbers, strings, etc.)
- **Special Type Handling**: `big.Int`, `time.Time`, `[]byte`, errors
- **Composite Types**: Arrays, slices, maps, structs with JSON tags
- **RPC Reference System**: Export/import placeholders for remote objects
- **Round-trip Compatibility**: Full serialization/deserialization cycle
- **Stack Trace Support**: Error serialization with debugging information

### ✅ Phase 1.4: Message Protocol

- **Core Message Types**: push, pull, resolve, reject, release, abort
- **Expression System**: Pipeline, remap, import, export expressions
- **Protocol Versioning**: Version compatibility checking
- **Message Validation**: Comprehensive message structure validation
- **Size Limits**: 64MB message size protection
- **Statistics Tracking**: Encoding/decoding performance metrics
- **JSON Compatibility**: Full round-trip through standard JSON

## Transport Interface

The transport layer provides the foundation for RPC communication:

```go
type Transport interface {
    Send(ctx context.Context, message []byte) error
    Receive(ctx context.Context) ([]byte, error)
    Close() error
}
```

### Optional Interfaces

- **TransportCloser**: For handling RPC session aborts
- **StatsProvider**: For monitoring transport usage

### Memory Transport

For testing and local communication:

```go
t1, t2 := capnweb.NewMemoryTransportPair()
defer t1.Close()
defer t2.Close()

// Send message from t1 to t2
err := t1.Send(ctx, []byte("hello"))
message, err := t2.Receive(ctx)
```

## Testing

Run tests with:

```bash
go test -v ./...
```

All transport functionality is fully tested including:
- Basic send/receive operations
- Bidirectional communication
- Graceful shutdown and abort handling
- Message size limits
- Statistics tracking
- Context cancellation

## Serialization System

The serialization layer provides JSON-compatible encoding with RPC extensions:

```go
// Create serializer with export function for RPC references
serializer := capnweb.NewSerializer(func(value interface{}) (capnweb.ExportID, error) {
    return session.ExportStub(value)
})

// Serialize any Go value
data, err := serializer.Serialize(myValue)
jsonBytes, err := serializer.ToJSON(myValue)

// Deserialize back to Go values
deserializer := capnweb.NewDeserializer(func(id capnweb.ImportID, isPromise bool) (interface{}, error) {
    return session.ImportStub(id, isPromise)
})

result, err := deserializer.Deserialize(data)
result, err := deserializer.FromJSON(jsonBytes)
```

### Supported Types

- **Primitives**: `bool`, `int*`, `uint*`, `float*`, `string`, `nil`
- **Special**: `*big.Int`, `time.Time`, `[]byte`, `error` types
- **Composite**: arrays, slices, maps, structs (with JSON tags)
- **RPC Types**: functions, `RpcTarget`, stubs, promises

## Message Protocol

The protocol layer handles Cap'n Web RPC message encoding and transmission:

```go
// Create protocol with serialization support
protocol := capnweb.NewProtocol(serializer, deserializer)

// Create different message types
pushMsg := protocol.NewPushMessage(expression, &exportID)
pullMsg := protocol.NewPullMessage(importID)
resolveMsg := protocol.NewResolveMessage(exportID, value)
rejectMsg := protocol.NewRejectMessage(exportID, error)
releaseMsg := protocol.NewReleaseMessage(importID, refCount)
abortMsg := protocol.NewAbortMessage(reason, &code)

// Encode/decode messages
data, err := protocol.EncodeMessage(msg)
msg, err := protocol.DecodeMessage(data)
```

### Message Types

- **push**: New RPC call or expression evaluation
- **pull**: Request promise resolution
- **resolve**: Successful promise resolution
- **reject**: Error promise resolution
- **release**: Reference no longer needed
- **abort**: Session termination due to error

### Expression Types

- **pipeline**: Method call chains (`obj.method(args)`)
- **remap**: Map operations on arrays/objects
- **import**: Reference to imported object
- **export**: Reference to exported object

## Next Steps

The following phases are planned:

1. **Phase 2**: RPC session management and stub system
2. **Phase 3**: Type system and code generation
3. **Phase 4**: Advanced features like promise pipelining
4. **Phase 5**: Transport implementations (HTTP batch, WebSocket)
5. **Phase 6**: Advanced protocol features (streaming, map operations)
6. **Phase 7**: Developer tooling and documentation

## Architecture

This Go implementation maintains wire-protocol compatibility with the original JavaScript Cap'n Web while adapting to Go's type system and concurrency model.

Key adaptations:
- Context-based cancellation instead of Promise cancellation
- Explicit error handling instead of exceptions
- Goroutines instead of event loop
- Static typing with code generation for type safety