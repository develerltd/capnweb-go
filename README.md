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

## Next Steps

The following phases are planned:

1. **Phase 1.2-1.4**: Core types, serialization, and message protocol
2. **Phase 2**: RPC session management and stub system
3. **Phase 3**: Type system and code generation
4. **Phase 4+**: Advanced features like promise pipelining

## Architecture

This Go implementation maintains wire-protocol compatibility with the original JavaScript Cap'n Web while adapting to Go's type system and concurrency model.

Key adaptations:
- Context-based cancellation instead of Promise cancellation
- Explicit error handling instead of exceptions
- Goroutines instead of event loop
- Static typing with code generation for type safety