# Cap'n Web Go vs JavaScript Compatibility Analysis

## 🚨 **Critical Incompatibilities Found**

### **1. Message Format - MAJOR INCOMPATIBILITY**

**JavaScript Wire Format (Array-based):**
```json
["push", expression]
["pull", importId]
["resolve", exportId, value]
["reject", exportId, error]
["release", exportId, refCount]
["abort", error]
```

**Go Wire Format (Object-based):**
```json
{
  "type": "push",
  "data": {"expression": ..., "exportId": ...},
  "version": "1.0"
}
```

**Impact:** Complete wire protocol incompatibility. Messages cannot be exchanged between implementations.

### **2. Serialization Format - MAJOR INCOMPATIBILITY**

**JavaScript Special Types (Array-based):**
```json
["bigint", "12345678901234567890"]
["date", 1672531200000]
["bytes", "base64string"]
["error", {...}]
```

**Go Special Types (Object-based):**
```json
{"type": "bigint", "value": "12345678901234567890"}
{"type": "date", "value": "2023-01-01T00:00:00Z"}
{"type": "bytes", "value": [1,2,3,4]}
{"type": "error", "value": {...}, "meta": {...}}
```

**Impact:** Serialized values cannot be exchanged between implementations.

### **3. Date Serialization**

**JavaScript:** Stores `Date.getTime()` (milliseconds since epoch)
```json
["date", 1672531200000]
```

**Go:** Stores RFC3339Nano string format
```json
{"type": "date", "value": "2023-01-01T00:00:00.000000000Z"}
```

### **4. Error Serialization**

**JavaScript:** Complex error reconstruction with type mapping
```javascript
// Preserves error constructor types (Error, TypeError, etc.)
// Uses ERROR_TYPES mapping for reconstruction
```

**Go:** Generic error interface with structured metadata
```json
{"type": "error", "value": {"message": "..."}, "meta": {"stack": "..."}}
```

## 📊 **Compatibility Matrix**

| Feature | JavaScript Format | Go Format | Compatible |
|---------|-------------------|-----------|------------|
| Message Structure | Array `["type", ...args]` | Object `{"type": "...", "data": {...}}` | ❌ No |
| Push Message | `["push", expr]` | `{"type": "push", "data": {"expression": ...}}` | ❌ No |
| Pull Message | `["pull", id]` | `{"type": "pull", "data": {"importId": ...}}` | ❌ No |
| Resolve Message | `["resolve", id, value]` | `{"type": "resolve", "data": {"exportId": ..., "value": ...}}` | ❌ No |
| BigInt | `["bigint", "123"]` | `{"type": "bigint", "value": "123"}` | ❌ No |
| Date | `["date", 1672531200000]` | `{"type": "date", "value": "RFC3339"}` | ❌ No |
| Bytes | `["bytes", "base64"]` | `{"type": "bytes", "value": [1,2,3]}` | ❌ No |
| Primitive Types | Direct JSON | Direct JSON | ✅ Yes |
| Arrays/Objects | Direct JSON | Direct JSON | ✅ Yes |
| Export/Import IDs | Numbers | Numbers | ✅ Yes |

## 🛠 **Required Changes for Compatibility**

### **Option 1: Modify Go Implementation (Recommended)**

Change Go to match JavaScript's array-based wire format:

```go
// Instead of:
type Message struct {
    Type MessageType `json:"type"`
    Data interface{} `json:"data"`
    Version string `json:"version,omitempty"`
}

// Use:
type Message []interface{} // ["type", ...args]

// Message creation:
func NewPushMessage(expr interface{}) Message {
    return Message{"push", expr}
}

func NewPullMessage(importID ImportID) Message {
    return Message{"pull", importID}
}

func NewResolveMessage(exportID ExportID, value interface{}) Message {
    return Message{"resolve", exportID, value}
}
```

### **Option 2: Create Translation Layer**

Add compatibility layer that translates between formats:

```go
type CompatibilityProtocol struct {
    *Protocol
    JavaScriptMode bool
}

func (cp *CompatibilityProtocol) EncodeMessage(msg *Message) ([]byte, error) {
    if cp.JavaScriptMode {
        return cp.encodeAsJavaScriptArray(msg)
    }
    return cp.Protocol.EncodeMessage(msg)
}
```

### **Option 3: Protocol Negotiation**

Implement version/format negotiation:

```go
type ProtocolFormat int

const (
    FormatGo ProtocolFormat = iota
    FormatJavaScript
)

func (p *Protocol) SetFormat(format ProtocolFormat) {
    p.format = format
}
```

## 🎯 **Recommended Approach**

**Phase 1: Immediate Compatibility (Option 1)**
1. Change Go message format to match JavaScript exactly
2. Update serialization to use array format `["type", value]`
3. Modify date serialization to use milliseconds
4. Update all tests to use new format

**Phase 2: Enhanced Compatibility**
1. Add comprehensive cross-platform integration tests
2. Implement JavaScript error type reconstruction in Go
3. Add performance optimizations for array-based format

**Phase 3: Long-term Protocol Evolution**
1. Design next-generation protocol that's optimal for both languages
2. Implement backward compatibility layers
3. Add formal protocol specification

## 🧪 **Testing Strategy**

1. **Wire Format Tests**: Create JSON test vectors that both implementations must pass
2. **Integration Tests**: Go client ↔ JavaScript server, JavaScript client ↔ Go server
3. **Round-trip Tests**: Ensure all data types survive round-trip through both implementations
4. **Performance Tests**: Compare performance of array vs object formats

## 📝 **Implementation Checklist**

- [ ] Modify Go message format to array-based
- [ ] Update serialization format to match JavaScript
- [ ] Fix date serialization (milliseconds vs RFC3339)
- [ ] Implement JavaScript-compatible error handling
- [ ] Update all protocol tests
- [ ] Create cross-platform integration tests
- [ ] Update documentation and examples
- [ ] Performance benchmarking

## 🔍 **Code Changes Required**

### **protocol.go**
```go
// Change from object-based to array-based messages
type Message []interface{}

// Update all message constructors
func NewPushMessage(expr interface{}) Message {
    return Message{"push", expr}
}
```

### **serialize.go**
```go
// Change special type format from object to array
func (s *Serializer) serializeBigInt(value *big.Int) interface{} {
    return []interface{}{"bigint", value.String()}
}

func (s *Serializer) serializeDate(value time.Time) interface{} {
    return []interface{}{"date", value.UnixMilli()}
}
```

## 🎯 **Success Criteria**

1. **100% Wire Compatibility**: Go and JavaScript can exchange all message types
2. **Data Fidelity**: All supported data types round-trip correctly
3. **Performance**: Array format performs comparably to object format
4. **Test Coverage**: Comprehensive integration tests with JavaScript runtime
5. **Documentation**: Clear specification of wire protocol format

---

**Conclusion**: The current Go implementation is **NOT compatible** with the JavaScript version due to fundamental differences in message and serialization formats. However, these are fixable with focused effort to align the Go implementation with JavaScript's array-based wire protocol.