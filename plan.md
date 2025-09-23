# Cap'n Web Go Library Implementation Plan

## Overview

This document outlines the technical approach for porting the JavaScript Cap'n Web RPC system to Go. The implementation will maintain API compatibility where possible while leveraging Go's type system and concurrency features.

## Architecture Analysis

### Core Concepts to Port

1. **Object-Capability RPC**: Bidirectional RPC with capability-based security
2. **Promise Pipelining**: Chain dependent calls in single network round trip
3. **Reference Passing**: Functions and objects passed by reference become remote stubs
4. **Resource Management**: Explicit disposal of remote references
5. **Multiple Transports**: HTTP batch, WebSocket, custom transports

### Key Challenges in Go

1. **Dynamic Typing**: JavaScript's dynamic nature vs Go's static typing
2. **Proxy Objects**: No native proxy support like JavaScript
3. **Reflection**: Heavy use of reflection for stub generation
4. **Generics**: Type-safe stubs without losing flexibility
5. **Memory Management**: Manual resource disposal in GC environment
6. **Concurrency**: Go routines vs JavaScript event loop

## Implementation Phases

### Phase 1: Core Foundation

**1.1 Transport Interface**
```go
type Transport interface {
    Send(ctx context.Context, message []byte) error
    Receive(ctx context.Context) ([]byte, error)
    Close() error
}
```

**1.2 Basic Types**
- `RpcTarget` interface for objects passed by reference
- `ExportID` and `ImportID` types for reference tracking
- `PropertyPath` type for object property navigation
- Error types for RPC-specific errors

**1.3 Serialization Foundation**
- JSON-based serialization with RPC extensions
- Support for primitive types, objects, arrays
- Placeholder system for remote references
- Error serialization with stack trace handling

**1.4 Message Protocol**
- Define message types: push, pull, resolve, reject, release, abort
- Message encoding/decoding
- Protocol version handling

### Phase 2: Core RPC Engine

**2.1 Session Management**
```go
type Session struct {
    transport Transport
    exports   map[ExportID]*exportEntry
    imports   map[ImportID]*importEntry
    // ... other fields
}
```

**2.2 Import/Export Tables**
- Export table for local objects exposed to remote
- Import table for remote object references
- Reference counting for resource management
- Automatic cleanup on session close

**2.3 Stub System**
```go
type Stub interface {
    Call(ctx context.Context, method string, args ...interface{}) (*Promise, error)
    Get(ctx context.Context, property string) (*Promise, error)
    Dispose() error
}
```

**2.4 Promise System**
```go
type Promise struct {
    session *Session
    importID ImportID
    path []string
}

func (p *Promise) Then(fn func(interface{}) interface{}) *Promise
func (p *Promise) Await(ctx context.Context) (interface{}, error)
```

### Phase 3: Type System and Code Generation

**3.1 Interface Definition**
- Define RPC interfaces using Go interfaces
- Struct tags for RPC metadata
- Method signature validation

**3.2 Stub Generation**
- Code generator for type-safe stubs
- Reflection-based runtime stub creation
- Support for both approaches

**3.3 Type Safety**
```go
//go:generate capnweb-gen -type=UserAPI
type UserAPI interface {
    Authenticate(token string) (*User, error)
    GetProfile(userID int) (*Profile, error)
}

// Generated code creates:
type UserAPIStub struct { /* ... */ }
func (s *UserAPIStub) Authenticate(ctx context.Context, token string) (*UserPromise, error)
```

### Phase 4: Advanced Features

**4.1 Promise Pipelining**
- Chain dependent calls without network round trips
- Automatic batching of pipelined operations
- Dependency graph resolution

**4.2 Bidirectional RPC**
- Server can call client methods
- Callback function support
- Event subscription patterns

**4.3 Resource Management**
- Context-based cancellation
- Automatic disposal with finalizers as backup
- Connection lifecycle management

### Phase 5: Transport Implementations

**5.1 HTTP Batch Transport**
```go
type HTTPBatchTransport struct {
    url    string
    client *http.Client
    batch  []message
}
```

**5.2 WebSocket Transport**
```go
type WebSocketTransport struct {
    conn   *websocket.Conn
    sendCh chan []byte
    recvCh chan []byte
}
```

**5.3 Custom Transports**
- gRPC transport
- TCP transport
- In-memory transport for testing

### Phase 6: Advanced Protocol Features

**6.1 Map Operations**
- Remote transformation of arrays/slices
- Functional programming support
- Batch operations on collections

**6.2 Streaming Support**
- Streaming results for large datasets
- Backpressure handling
- Progressive result delivery

**6.3 Security Features**
- Capability-based access control
- Authentication integration
- Rate limiting and quotas

### Phase 7: Developer Experience

**7.1 Code Generation Tools**
```bash
capnweb-gen -input=api.go -output=api_stub.go
```

**7.2 Testing Framework**
- Mock transport for unit tests
- Stub verification
- RPC call recording/playback

**7.3 Debugging Tools**
- RPC call tracing
- Performance metrics
- Connection state inspection

**7.4 Documentation and Examples**
- API documentation
- Tutorial examples
- Best practices guide

## Technical Decisions

### Stub Implementation Strategy

**Option A: Code Generation**
- Generate type-safe stubs at build time
- Better performance, compile-time safety
- Requires build step, less dynamic

**Option B: Reflection**
- Runtime stub creation using reflection
- More dynamic, no build step required
- Potential performance overhead

**Recommended**: Hybrid approach - code generation for production, reflection for development/testing

### Concurrency Model

```go
// Each session runs in its own goroutine
func (s *Session) messageLoop(ctx context.Context) {
    for {
        select {
        case msg := <-s.incomingMessages:
            s.handleMessage(msg)
        case <-ctx.Done():
            return
        }
    }
}
```

### Error Handling

```go
type RPCError struct {
    Type    string `json:"type"`
    Message string `json:"message"`
    Stack   string `json:"stack,omitempty"`
    Code    int    `json:"code,omitempty"`
}

func (e *RPCError) Error() string {
    return fmt.Sprintf("RPC error (%s): %s", e.Type, e.Message)
}
```

### Memory Management

```go
type Disposer interface {
    Dispose() error
}

// Use finalizers as safety net
func NewStub(session *Session, importID ImportID) *Stub {
    s := &Stub{session: session, importID: importID}
    runtime.SetFinalizer(s, (*Stub).dispose)
    return s
}
```

## API Design Examples

### Client Usage
```go
// HTTP Batch
session, err := capnweb.NewHTTPBatchSession("https://api.example.com/rpc")
if err != nil {
    log.Fatal(err)
}
defer session.Close()

api := NewUserAPIStub(session)

// Pipelined calls
user := api.Authenticate(ctx, "token123")
profile := api.GetProfile(ctx, user.ID()) // Uses result of first call
notifications := api.GetNotifications(ctx, user.ID())

// Await all results (sent in single HTTP request)
u, err := user.Await(ctx)
p, err := profile.Await(ctx)
n, err := notifications.Await(ctx)
```

### Server Usage
```go
type UserService struct{}

func (s *UserService) Authenticate(ctx context.Context, token string) (*User, error) {
    // Implementation
}

func (s *UserService) GetProfile(ctx context.Context, userID int) (*Profile, error) {
    // Implementation
}

// HTTP server
server := capnweb.NewHTTPServer()
server.Handle("/rpc", &UserService{})
log.Fatal(http.ListenAndServe(":8080", server))
```

## Testing Strategy

### Unit Tests
- Test each component in isolation
- Mock transports for network simulation
- Property-based testing for serialization

### Integration Tests
- End-to-end RPC scenarios
- Multiple transport types
- Error condition handling

### Performance Tests
- Latency measurements
- Throughput benchmarks
- Memory usage profiling
- Comparison with JavaScript implementation

## Migration Considerations

### JavaScript Compatibility
- Maintain wire protocol compatibility
- Support interoperability between Go and JS implementations
- Same JSON message format

### API Differences
- Go's static typing vs JavaScript's dynamic nature
- Context-based cancellation vs JavaScript promises
- Explicit error handling vs JavaScript exceptions

### Performance Characteristics
- Go's compiled nature should provide better performance
- Garbage collection considerations
- Goroutine overhead vs JavaScript event loop

## Dependencies

### Required Go Packages
- `encoding/json` - JSON serialization
- `reflect` - Runtime type information
- `context` - Request lifecycle management
- `net/http` - HTTP transport
- `nhooyr.io/websocket` - WebSocket support

### Optional Packages
- `google.golang.org/protobuf` - Alternative serialization
- `go.uber.org/zap` - Structured logging
- `github.com/stretchr/testify` - Testing utilities

## Future Enhancements

### Performance Optimizations
- Custom binary protocol option
- Connection pooling
- Message compression

### Protocol Extensions
- Streaming RPC support
- Distributed tracing integration
- Metrics collection

### Ecosystem Integration
- gRPC gateway compatibility
- OpenAPI/Swagger generation
- Service mesh integration

This plan provides a comprehensive roadmap for implementing Cap'n Web in Go while maintaining the core concepts and capabilities of the original JavaScript implementation.