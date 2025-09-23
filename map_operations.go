package capnweb

import (
	"context"
	"fmt"
	"reflect"
)

// MapOperation represents a remote transformation operation on arrays/slices
type MapOperation struct {
	session   *Session
	importID  ImportID
	path      []string
	chunkSize int
}

// MapOptions configures map operation behavior
type MapOptions struct {
	// ChunkSize is the number of elements to process in each batch
	ChunkSize int

	// Parallel enables parallel processing of chunks
	Parallel bool

	// MaxConcurrency limits the number of concurrent chunks when parallel is enabled
	MaxConcurrency int

	// ProgressCallback is called periodically with progress updates
	ProgressCallback func(processed, total int)
}

// DefaultMapOptions returns reasonable defaults for map operations
func DefaultMapOptions() MapOptions {
	return MapOptions{
		ChunkSize:      100,
		Parallel:       true,
		MaxConcurrency: 4,
	}
}

// MapProgress represents the progress of a map operation
type MapProgress struct {
	Processed int
	Total     int
	Percent   float64
}

// NewMapOperation creates a new map operation for the given array/slice
func NewMapOperation(session *Session, importID ImportID, path []string) *MapOperation {
	return &MapOperation{
		session:   session,
		importID:  importID,
		path:      path,
		chunkSize: 100, // Default chunk size
	}
}

// SetChunkSize sets the number of elements to process in each batch
func (m *MapOperation) SetChunkSize(size int) *MapOperation {
	m.chunkSize = size
	return m
}

// Map applies a transformation function to each element of the remote array/slice
func (m *MapOperation) Map(ctx context.Context, transformFunc interface{}, options ...MapOptions) (*Promise, error) {
	opts := DefaultMapOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	// Validate transform function
	funcType := reflect.TypeOf(transformFunc)
	if funcType == nil || funcType.Kind() != reflect.Func {
		return nil, fmt.Errorf("transform function must be a function")
	}

	if funcType.NumIn() != 1 || funcType.NumOut() != 1 {
		return nil, fmt.Errorf("transform function must have exactly one input and one output parameter")
	}

	// Create map operation message
	mapMsg := map[string]interface{}{
		"type":         "map",
		"importID":     m.importID,
		"path":         m.path,
		"function":     m.serializeFunction(transformFunc),
		"chunkSize":    opts.ChunkSize,
		"parallel":     opts.Parallel,
		"maxConcurrency": opts.MaxConcurrency,
	}

	return m.session.SendRequest(ctx, mapMsg)
}

// Filter applies a predicate function to filter elements of the remote array/slice
func (m *MapOperation) Filter(ctx context.Context, predicateFunc interface{}, options ...MapOptions) (*Promise, error) {
	opts := DefaultMapOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	// Validate predicate function
	funcType := reflect.TypeOf(predicateFunc)
	if funcType == nil || funcType.Kind() != reflect.Func {
		return nil, fmt.Errorf("predicate function must be a function")
	}

	if funcType.NumIn() != 1 || funcType.NumOut() != 1 {
		return nil, fmt.Errorf("predicate function must have exactly one input and one output parameter")
	}

	if funcType.Out(0).Kind() != reflect.Bool {
		return nil, fmt.Errorf("predicate function must return a boolean")
	}

	// Create filter operation message
	filterMsg := map[string]interface{}{
		"type":         "filter",
		"importID":     m.importID,
		"path":         m.path,
		"predicate":    m.serializeFunction(predicateFunc),
		"chunkSize":    opts.ChunkSize,
		"parallel":     opts.Parallel,
		"maxConcurrency": opts.MaxConcurrency,
	}

	return m.session.SendRequest(ctx, filterMsg)
}

// Reduce applies a reduction function to combine all elements into a single value
func (m *MapOperation) Reduce(ctx context.Context, reduceFunc interface{}, initialValue interface{}, options ...MapOptions) (*Promise, error) {
	opts := DefaultMapOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	// Validate reduce function
	funcType := reflect.TypeOf(reduceFunc)
	if funcType == nil || funcType.Kind() != reflect.Func {
		return nil, fmt.Errorf("reduce function must be a function")
	}

	if funcType.NumIn() != 2 || funcType.NumOut() != 1 {
		return nil, fmt.Errorf("reduce function must have exactly two input parameters and one output parameter")
	}

	// Create reduce operation message
	reduceMsg := map[string]interface{}{
		"type":         "reduce",
		"importID":     m.importID,
		"path":         m.path,
		"function":     m.serializeFunction(reduceFunc),
		"initialValue": initialValue,
		"chunkSize":    opts.ChunkSize,
		"parallel":     opts.Parallel,
	}

	return m.session.SendRequest(ctx, reduceMsg)
}

// ForEach applies a function to each element without returning results
func (m *MapOperation) ForEach(ctx context.Context, actionFunc interface{}, options ...MapOptions) (*Promise, error) {
	opts := DefaultMapOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	// Validate action function
	funcType := reflect.TypeOf(actionFunc)
	if funcType == nil || funcType.Kind() != reflect.Func {
		return nil, fmt.Errorf("action function must be a function")
	}

	if funcType.NumIn() != 1 {
		return nil, fmt.Errorf("action function must have exactly one input parameter")
	}

	// Create forEach operation message
	forEachMsg := map[string]interface{}{
		"type":         "forEach",
		"importID":     m.importID,
		"path":         m.path,
		"function":     m.serializeFunction(actionFunc),
		"chunkSize":    opts.ChunkSize,
		"parallel":     opts.Parallel,
		"maxConcurrency": opts.MaxConcurrency,
	}

	return m.session.SendRequest(ctx, forEachMsg)
}

// Find searches for the first element that matches a predicate
func (m *MapOperation) Find(ctx context.Context, predicateFunc interface{}) (*Promise, error) {
	// Validate predicate function
	funcType := reflect.TypeOf(predicateFunc)
	if funcType == nil || funcType.Kind() != reflect.Func {
		return nil, fmt.Errorf("predicate function must be a function")
	}

	if funcType.NumIn() != 1 || funcType.NumOut() != 1 {
		return nil, fmt.Errorf("predicate function must have exactly one input and one output parameter")
	}

	if funcType.Out(0).Kind() != reflect.Bool {
		return nil, fmt.Errorf("predicate function must return a boolean")
	}

	// Create find operation message
	findMsg := map[string]interface{}{
		"type":      "find",
		"importID":  m.importID,
		"path":      m.path,
		"predicate": m.serializeFunction(predicateFunc),
	}

	return m.session.SendRequest(ctx, findMsg)
}

// Some checks if any element matches a predicate
func (m *MapOperation) Some(ctx context.Context, predicateFunc interface{}) (*Promise, error) {
	// Validate predicate function
	funcType := reflect.TypeOf(predicateFunc)
	if funcType == nil || funcType.Kind() != reflect.Func {
		return nil, fmt.Errorf("predicate function must be a function")
	}

	if funcType.NumIn() != 1 || funcType.NumOut() != 1 {
		return nil, fmt.Errorf("predicate function must have exactly one input and one output parameter")
	}

	if funcType.Out(0).Kind() != reflect.Bool {
		return nil, fmt.Errorf("predicate function must return a boolean")
	}

	// Create some operation message
	someMsg := map[string]interface{}{
		"type":      "some",
		"importID":  m.importID,
		"path":      m.path,
		"predicate": m.serializeFunction(predicateFunc),
	}

	return m.session.SendRequest(ctx, someMsg)
}

// Every checks if all elements match a predicate
func (m *MapOperation) Every(ctx context.Context, predicateFunc interface{}) (*Promise, error) {
	// Validate predicate function
	funcType := reflect.TypeOf(predicateFunc)
	if funcType == nil || funcType.Kind() != reflect.Func {
		return nil, fmt.Errorf("predicate function must be a function")
	}

	if funcType.NumIn() != 1 || funcType.NumOut() != 1 {
		return nil, fmt.Errorf("predicate function must have exactly one input and one output parameter")
	}

	if funcType.Out(0).Kind() != reflect.Bool {
		return nil, fmt.Errorf("predicate function must return a boolean")
	}

	// Create every operation message
	everyMsg := map[string]interface{}{
		"type":      "every",
		"importID":  m.importID,
		"path":      m.path,
		"predicate": m.serializeFunction(predicateFunc),
	}

	return m.session.SendRequest(ctx, everyMsg)
}

// Count returns the number of elements in the array/slice
func (m *MapOperation) Count(ctx context.Context) (*Promise, error) {
	// Create count operation message
	countMsg := map[string]interface{}{
		"type":     "count",
		"importID": m.importID,
		"path":     m.path,
	}

	return m.session.SendRequest(ctx, countMsg)
}

// Slice returns a subset of the array/slice
func (m *MapOperation) Slice(ctx context.Context, start, end int) (*Promise, error) {
	// Create slice operation message
	sliceMsg := map[string]interface{}{
		"type":     "slice",
		"importID": m.importID,
		"path":     m.path,
		"start":    start,
		"end":      end,
	}

	return m.session.SendRequest(ctx, sliceMsg)
}

// Sort sorts the array/slice using a comparison function
func (m *MapOperation) Sort(ctx context.Context, compareFunc interface{}) (*Promise, error) {
	// Validate compare function
	funcType := reflect.TypeOf(compareFunc)
	if funcType == nil || funcType.Kind() != reflect.Func {
		return nil, fmt.Errorf("compare function must be a function")
	}

	if funcType.NumIn() != 2 || funcType.NumOut() != 1 {
		return nil, fmt.Errorf("compare function must have exactly two input parameters and one output parameter")
	}

	if funcType.Out(0).Kind() != reflect.Int {
		return nil, fmt.Errorf("compare function must return an integer")
	}

	// Create sort operation message
	sortMsg := map[string]interface{}{
		"type":        "sort",
		"importID":    m.importID,
		"path":        m.path,
		"compareFunc": m.serializeFunction(compareFunc),
	}

	return m.session.SendRequest(ctx, sortMsg)
}

// serializeFunction converts a Go function to a format that can be sent over RPC
func (m *MapOperation) serializeFunction(fn interface{}) map[string]interface{} {
	// In a production implementation, this would:
	// 1. Extract the function's bytecode or source
	// 2. Serialize it to a transferable format
	// 3. Include type information for parameters and return values
	//
	// For this demo, we'll create a simplified representation
	funcType := reflect.TypeOf(fn)

	// Get parameter types
	paramTypes := make([]string, funcType.NumIn())
	for i := 0; i < funcType.NumIn(); i++ {
		paramTypes[i] = funcType.In(i).String()
	}

	// Get return types
	returnTypes := make([]string, funcType.NumOut())
	for i := 0; i < funcType.NumOut(); i++ {
		returnTypes[i] = funcType.Out(i).String()
	}

	return map[string]interface{}{
		"type":        "function",
		"paramTypes":  paramTypes,
		"returnTypes": returnTypes,
		"source":      fmt.Sprintf("func(%v) %v", paramTypes, returnTypes),
		// In production, would include actual executable code
		"executable": false,
		"note":       "Function serialization requires code generation or reflection-based execution",
	}
}

// BatchMapOperation represents a batch of map operations that can be executed together
type BatchMapOperation struct {
	session    *Session
	operations []map[string]interface{}
}

// NewBatchMapOperation creates a new batch map operation
func NewBatchMapOperation(session *Session) *BatchMapOperation {
	return &BatchMapOperation{
		session:    session,
		operations: make([]map[string]interface{}, 0),
	}
}

// AddMapOperation adds a map operation to the batch
func (b *BatchMapOperation) AddMapOperation(mapOp *MapOperation, transformFunc interface{}) *BatchMapOperation {
	operation := map[string]interface{}{
		"type":      "map",
		"importID":  mapOp.importID,
		"path":      mapOp.path,
		"function":  mapOp.serializeFunction(transformFunc),
		"chunkSize": mapOp.chunkSize,
	}
	b.operations = append(b.operations, operation)
	return b
}

// AddFilterOperation adds a filter operation to the batch
func (b *BatchMapOperation) AddFilterOperation(mapOp *MapOperation, predicateFunc interface{}) *BatchMapOperation {
	operation := map[string]interface{}{
		"type":      "filter",
		"importID":  mapOp.importID,
		"path":      mapOp.path,
		"predicate": mapOp.serializeFunction(predicateFunc),
		"chunkSize": mapOp.chunkSize,
	}
	b.operations = append(b.operations, operation)
	return b
}

// Execute executes all operations in the batch
func (b *BatchMapOperation) Execute(ctx context.Context) (*Promise, error) {
	// Create batch operation message
	batchMsg := map[string]interface{}{
		"type":       "batchMap",
		"operations": b.operations,
	}

	return b.session.SendRequest(ctx, batchMsg)
}

// Clear removes all operations from the batch
func (b *BatchMapOperation) Clear() {
	b.operations = b.operations[:0]
}

// Count returns the number of operations in the batch
func (b *BatchMapOperation) Count() int {
	return len(b.operations)
}

// MapOperationResult represents the result of a map operation
type MapOperationResult struct {
	Type        string      `json:"type"`
	Success     bool        `json:"success"`
	Result      interface{} `json:"result,omitempty"`
	Error       string      `json:"error,omitempty"`
	Processed   int         `json:"processed,omitempty"`
	Total       int         `json:"total,omitempty"`
	Duration    int64       `json:"duration,omitempty"` // Duration in milliseconds
	ChunksUsed  int         `json:"chunksUsed,omitempty"`
	Parallel    bool        `json:"parallel,omitempty"`
}

// MapOperationStats tracks statistics for map operations
type MapOperationStats struct {
	TotalOperations   int64
	SuccessfulOps     int64
	FailedOps         int64
	ElementsProcessed int64
	TotalDuration     int64
	AverageChunkSize  float64
	ParallelOps       int64
}

// Helper functions for common transformations

// MapToString creates a map operation that converts elements to strings
func MapToString(mapOp *MapOperation) func(ctx context.Context) (*Promise, error) {
	return func(ctx context.Context) (*Promise, error) {
		toString := func(x interface{}) string {
			return fmt.Sprintf("%v", x)
		}
		return mapOp.Map(ctx, toString)
	}
}

// MapToNumber creates a map operation that converts string elements to numbers
func MapToNumber(mapOp *MapOperation) func(ctx context.Context) (*Promise, error) {
	return func(ctx context.Context) (*Promise, error) {
		toNumber := func(x string) float64 {
			// In production, would use strconv.ParseFloat with error handling
			return 0.0 // Simplified for demo
		}
		return mapOp.Map(ctx, toNumber)
	}
}

// FilterNonEmpty creates a filter operation that removes empty/nil elements
func FilterNonEmpty(mapOp *MapOperation) func(ctx context.Context) (*Promise, error) {
	return func(ctx context.Context) (*Promise, error) {
		isNonEmpty := func(x interface{}) bool {
			return x != nil && x != ""
		}
		return mapOp.Filter(ctx, isNonEmpty)
	}
}

// Sum creates a reduce operation that sums all numeric elements
func Sum(mapOp *MapOperation) func(ctx context.Context) (*Promise, error) {
	return func(ctx context.Context) (*Promise, error) {
		add := func(acc, current float64) float64 {
			return acc + current
		}
		return mapOp.Reduce(ctx, add, 0.0)
	}
}