package capnweb

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// PipelineManager handles promise pipelining and automatic batching
type PipelineManager struct {
	session *Session

	// Pipeline tracking
	pipelines    map[PipelineID]*Pipeline
	pipelinesMux sync.RWMutex

	// Batch management
	batchQueue    []*PipelineOperation
	batchMux      sync.Mutex
	batchTimer    *time.Timer
	batchInterval time.Duration
	maxBatchSize  int

	// Dependency tracking
	dependencies map[ExportID][]*PipelineOperation
	depsMux      sync.RWMutex

	// Configuration
	options PipelineOptions
}

// PipelineID uniquely identifies a pipeline
type PipelineID string

// PipelineOptions configures pipelining behavior
type PipelineOptions struct {
	// BatchInterval is how long to wait before sending a batch
	BatchInterval time.Duration

	// MaxBatchSize is the maximum operations per batch
	MaxBatchSize int

	// EnableDependencyOptimization optimizes dependent calls
	EnableDependencyOptimization bool

	// MaxPipelineDepth limits how deep pipelines can go
	MaxPipelineDepth int

	// TimeoutPerOperation sets timeout for each operation
	TimeoutPerOperation time.Duration
}

// DefaultPipelineOptions returns reasonable defaults
func DefaultPipelineOptions() PipelineOptions {
	return PipelineOptions{
		BatchInterval:                100 * time.Millisecond,
		MaxBatchSize:                 50,
		EnableDependencyOptimization: true,
		MaxPipelineDepth:            10,
		TimeoutPerOperation:         30 * time.Second,
	}
}

// Pipeline represents a chain of dependent RPC operations
type Pipeline struct {
	ID          PipelineID
	Operations  []*PipelineOperation
	Status      PipelineStatus
	Created     time.Time
	StartedAt   time.Time
	CompletedAt time.Time
	Error       error

	// Synchronization
	mu sync.RWMutex

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// PipelineStatus represents the state of a pipeline
type PipelineStatus int

const (
	PipelineStatusPending PipelineStatus = iota
	PipelineStatusRunning
	PipelineStatusCompleted
	PipelineStatusFailed
	PipelineStatusCancelled
)

// String returns string representation of pipeline status
func (ps PipelineStatus) String() string {
	switch ps {
	case PipelineStatusPending:
		return "pending"
	case PipelineStatusRunning:
		return "running"
	case PipelineStatusCompleted:
		return "completed"
	case PipelineStatusFailed:
		return "failed"
	case PipelineStatusCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// PipelineOperation represents a single operation in a pipeline
type PipelineOperation struct {
	ID           OperationID
	Type         OperationType
	TargetID     ImportID
	Method       string
	Arguments    []interface{}
	Path         PropertyPath
	ResultID     ExportID
	Dependencies []ExportID
	Promise      *Promise

	// Execution tracking
	Status      OperationStatus
	StartedAt   time.Time
	CompletedAt time.Time
	Error       error
	Result      interface{}

	// Pipeline context
	Pipeline *Pipeline
}

// OperationID uniquely identifies an operation
type OperationID string

// OperationType defines the type of operation
type OperationType int

const (
	OperationTypeCall OperationType = iota
	OperationTypeGet
	OperationTypeSet
)

// OperationStatus represents the state of an operation
type OperationStatus int

const (
	OperationStatusPending OperationStatus = iota
	OperationStatusRunning
	OperationStatusCompleted
	OperationStatusFailed
)

// NewPipelineManager creates a new pipeline manager
func NewPipelineManager(session *Session, options ...PipelineOptions) *PipelineManager {
	opts := DefaultPipelineOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	pm := &PipelineManager{
		session:      session,
		pipelines:    make(map[PipelineID]*Pipeline),
		dependencies: make(map[ExportID][]*PipelineOperation),
		options:      opts,
		batchInterval: opts.BatchInterval,
		maxBatchSize: opts.MaxBatchSize,
	}

	// Start batch processing
	pm.startBatchProcessor()

	return pm
}

// CreatePipeline creates a new pipeline for chained operations
func (pm *PipelineManager) CreatePipeline(ctx context.Context) *Pipeline {
	pipelineID := PipelineID(fmt.Sprintf("pipeline_%d", time.Now().UnixNano()))

	pipelineCtx, cancel := context.WithCancel(ctx)

	pipeline := &Pipeline{
		ID:        pipelineID,
		Status:    PipelineStatusPending,
		Created:   time.Now(),
		ctx:       pipelineCtx,
		cancel:    cancel,
	}

	pm.pipelinesMux.Lock()
	pm.pipelines[pipelineID] = pipeline
	pm.pipelinesMux.Unlock()

	return pipeline
}

// AddOperation adds an operation to a pipeline
func (pm *PipelineManager) AddOperation(pipeline *Pipeline, op *PipelineOperation) error {
	if pipeline.Status != PipelineStatusPending {
		return fmt.Errorf("cannot add operation to %s pipeline", pipeline.Status)
	}

	if len(pipeline.Operations) >= pm.options.MaxPipelineDepth {
		return fmt.Errorf("pipeline depth limit exceeded (%d)", pm.options.MaxPipelineDepth)
	}

	op.Pipeline = pipeline
	op.Status = OperationStatusPending

	pipeline.mu.Lock()
	pipeline.Operations = append(pipeline.Operations, op)
	pipeline.mu.Unlock()

	// Track dependencies
	pm.trackDependencies(op)

	return nil
}

// ExecutePipeline executes all operations in a pipeline with optimization
func (pm *PipelineManager) ExecutePipeline(pipeline *Pipeline) error {
	pipeline.mu.Lock()
	if pipeline.Status != PipelineStatusPending {
		pipeline.mu.Unlock()
		return fmt.Errorf("pipeline is not in pending state")
	}

	pipeline.Status = PipelineStatusRunning
	pipeline.StartedAt = time.Now()
	pipeline.mu.Unlock()

	// Optimize the pipeline
	optimizedOps, err := pm.optimizePipeline(pipeline)
	if err != nil {
		pm.failPipeline(pipeline, err)
		return err
	}

	// Execute operations with batching
	err = pm.executeBatchedOperations(pipeline, optimizedOps)
	if err != nil {
		pm.failPipeline(pipeline, err)
		return err
	}

	// Mark pipeline as completed
	pipeline.mu.Lock()
	pipeline.Status = PipelineStatusCompleted
	pipeline.CompletedAt = time.Now()
	pipeline.mu.Unlock()

	return nil
}

// optimizePipeline analyzes and optimizes the pipeline operations
func (pm *PipelineManager) optimizePipeline(pipeline *Pipeline) ([]*PipelineOperation, error) {
	if !pm.options.EnableDependencyOptimization {
		return pipeline.Operations, nil
	}

	// Build dependency graph
	depGraph := pm.buildDependencyGraph(pipeline.Operations)

	// Topological sort for execution order
	sortedOps, err := pm.topologicalSort(depGraph)
	if err != nil {
		return nil, fmt.Errorf("dependency cycle detected: %w", err)
	}

	// Group operations that can be batched
	batchedOps := pm.groupForBatching(sortedOps)

	return batchedOps, nil
}

// buildDependencyGraph creates a dependency graph from operations
func (pm *PipelineManager) buildDependencyGraph(operations []*PipelineOperation) map[*PipelineOperation][]*PipelineOperation {
	graph := make(map[*PipelineOperation][]*PipelineOperation)
	opsByResult := make(map[ExportID]*PipelineOperation)

	// Build mapping of result IDs to operations
	for _, op := range operations {
		if op.ResultID != 0 {
			opsByResult[op.ResultID] = op
		}
		graph[op] = []*PipelineOperation{}
	}

	// Build dependency edges
	for _, op := range operations {
		for _, depID := range op.Dependencies {
			if depOp, exists := opsByResult[depID]; exists {
				graph[depOp] = append(graph[depOp], op)
			}
		}
	}

	return graph
}

// topologicalSort sorts operations by dependency order
func (pm *PipelineManager) topologicalSort(graph map[*PipelineOperation][]*PipelineOperation) ([]*PipelineOperation, error) {
	var result []*PipelineOperation
	visited := make(map[*PipelineOperation]bool)
	visiting := make(map[*PipelineOperation]bool)

	var visit func(*PipelineOperation) error
	visit = func(op *PipelineOperation) error {
		if visiting[op] {
			return fmt.Errorf("circular dependency detected")
		}
		if visited[op] {
			return nil
		}

		visiting[op] = true

		for _, dep := range graph[op] {
			if err := visit(dep); err != nil {
				return err
			}
		}

		visiting[op] = false
		visited[op] = true
		result = append([]*PipelineOperation{op}, result...)

		return nil
	}

	for op := range graph {
		if !visited[op] {
			if err := visit(op); err != nil {
				return nil, err
			}
		}
	}

	return result, nil
}

// groupForBatching groups operations that can be executed in parallel
func (pm *PipelineManager) groupForBatching(operations []*PipelineOperation) []*PipelineOperation {
	// For now, return operations as-is
	// In a full implementation, this would group operations with no dependencies
	// that can be executed in parallel
	return operations
}

// executeBatchedOperations executes operations with automatic batching
func (pm *PipelineManager) executeBatchedOperations(pipeline *Pipeline, operations []*PipelineOperation) error {
	for _, op := range operations {
		// Check if pipeline was cancelled
		select {
		case <-pipeline.ctx.Done():
			return pipeline.ctx.Err()
		default:
		}

		// Execute the operation
		err := pm.executeOperation(op)
		if err != nil {
			return fmt.Errorf("operation %s failed: %w", op.ID, err)
		}
	}

	return nil
}

// executeOperation executes a single pipeline operation
func (pm *PipelineManager) executeOperation(op *PipelineOperation) error {
	op.Status = OperationStatusRunning
	op.StartedAt = time.Now()

	// Create operation context with timeout
	ctx, cancel := context.WithTimeout(op.Pipeline.ctx, pm.options.TimeoutPerOperation)
	defer cancel()

	// Get the target stub
	stub, err := pm.session.createImportStub(op.TargetID, false)
	if err != nil {
		return fmt.Errorf("failed to create stub: %w", err)
	}

	stubImpl := stub.(*stubImpl)

	var result interface{}

	switch op.Type {
	case OperationTypeCall:
		promise, err := stubImpl.Call(ctx, op.Method, op.Arguments...)
		if err != nil {
			op.Status = OperationStatusFailed
			op.Error = err
			return err
		}

		// For pipelined operations, we don't await immediately
		// Instead, we store the promise for later resolution
		op.Promise = promise
		result = promise

	case OperationTypeGet:
		promise, err := stubImpl.Get(ctx, op.Method)
		if err != nil {
			op.Status = OperationStatusFailed
			op.Error = err
			return err
		}

		op.Promise = promise
		result = promise

	default:
		return fmt.Errorf("unsupported operation type: %d", op.Type)
	}

	op.Status = OperationStatusCompleted
	op.CompletedAt = time.Now()
	op.Result = result

	return nil
}

// trackDependencies tracks operation dependencies for optimization
func (pm *PipelineManager) trackDependencies(op *PipelineOperation) {
	pm.depsMux.Lock()
	defer pm.depsMux.Unlock()

	for _, depID := range op.Dependencies {
		pm.dependencies[depID] = append(pm.dependencies[depID], op)
	}
}

// failPipeline marks a pipeline as failed
func (pm *PipelineManager) failPipeline(pipeline *Pipeline, err error) {
	pipeline.mu.Lock()
	pipeline.Status = PipelineStatusFailed
	pipeline.Error = err
	pipeline.CompletedAt = time.Now()
	pipeline.mu.Unlock()
}

// CancelPipeline cancels a running pipeline
func (pm *PipelineManager) CancelPipeline(pipelineID PipelineID) error {
	pm.pipelinesMux.RLock()
	pipeline, exists := pm.pipelines[pipelineID]
	pm.pipelinesMux.RUnlock()

	if !exists {
		return fmt.Errorf("pipeline %s not found", pipelineID)
	}

	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()

	if pipeline.Status == PipelineStatusCompleted || pipeline.Status == PipelineStatusFailed {
		return fmt.Errorf("cannot cancel %s pipeline", pipeline.Status)
	}

	pipeline.Status = PipelineStatusCancelled
	pipeline.CompletedAt = time.Now()
	pipeline.cancel()

	return nil
}

// GetPipeline retrieves a pipeline by ID
func (pm *PipelineManager) GetPipeline(pipelineID PipelineID) (*Pipeline, bool) {
	pm.pipelinesMux.RLock()
	defer pm.pipelinesMux.RUnlock()

	pipeline, exists := pm.pipelines[pipelineID]
	return pipeline, exists
}

// startBatchProcessor starts the background batch processor
func (pm *PipelineManager) startBatchProcessor() {
	go func() {
		for {
			pm.batchMux.Lock()
			if len(pm.batchQueue) > 0 {
				batch := make([]*PipelineOperation, len(pm.batchQueue))
				copy(batch, pm.batchQueue)
				pm.batchQueue = pm.batchQueue[:0]
				pm.batchMux.Unlock()

				pm.processBatch(batch)
			} else {
				pm.batchMux.Unlock()
			}

			time.Sleep(pm.batchInterval)
		}
	}()
}

// processBatch processes a batch of operations
func (pm *PipelineManager) processBatch(batch []*PipelineOperation) {
	// Group operations by target for more efficient processing
	targetGroups := make(map[ImportID][]*PipelineOperation)

	for _, op := range batch {
		targetGroups[op.TargetID] = append(targetGroups[op.TargetID], op)
	}

	// Process each target group
	for targetID, ops := range targetGroups {
		pm.processTargetGroup(targetID, ops)
	}
}

// processTargetGroup processes operations for a specific target
func (pm *PipelineManager) processTargetGroup(targetID ImportID, operations []*PipelineOperation) {
	// In a full implementation, this would create batched RPC calls
	// to the same target to reduce network round trips
	for _, op := range operations {
		pm.executeOperation(op)
	}
}

// PipelineBuilder helps build complex pipelines fluently
type PipelineBuilder struct {
	manager  *PipelineManager
	pipeline *Pipeline
}

// NewPipelineBuilder creates a new pipeline builder
func (pm *PipelineManager) NewPipelineBuilder(ctx context.Context) *PipelineBuilder {
	pipeline := pm.CreatePipeline(ctx)
	return &PipelineBuilder{
		manager:  pm,
		pipeline: pipeline,
	}
}

// Call adds a method call operation to the pipeline
func (pb *PipelineBuilder) Call(targetID ImportID, method string, args ...interface{}) *PipelineBuilder {
	opID := OperationID(fmt.Sprintf("op_%d", time.Now().UnixNano()))

	op := &PipelineOperation{
		ID:        opID,
		Type:      OperationTypeCall,
		TargetID:  targetID,
		Method:    method,
		Arguments: args,
		ResultID:  ExportID(time.Now().UnixNano()),
	}

	pb.manager.AddOperation(pb.pipeline, op)
	return pb
}

// Get adds a property access operation to the pipeline
func (pb *PipelineBuilder) Get(targetID ImportID, property string) *PipelineBuilder {
	opID := OperationID(fmt.Sprintf("op_%d", time.Now().UnixNano()))

	op := &PipelineOperation{
		ID:       opID,
		Type:     OperationTypeGet,
		TargetID: targetID,
		Method:   property,
		ResultID: ExportID(time.Now().UnixNano()),
	}

	pb.manager.AddOperation(pb.pipeline, op)
	return pb
}

// DependsOn adds a dependency to the last operation
func (pb *PipelineBuilder) DependsOn(exportIDs ...ExportID) *PipelineBuilder {
	if len(pb.pipeline.Operations) > 0 {
		lastOp := pb.pipeline.Operations[len(pb.pipeline.Operations)-1]
		lastOp.Dependencies = append(lastOp.Dependencies, exportIDs...)
	}
	return pb
}

// Execute builds and executes the pipeline
func (pb *PipelineBuilder) Execute() error {
	return pb.manager.ExecutePipeline(pb.pipeline)
}

// Build returns the pipeline without executing it
func (pb *PipelineBuilder) Build() *Pipeline {
	return pb.pipeline
}

// PipelineStats tracks pipeline execution statistics
type PipelineStats struct {
	TotalPipelines    int64
	CompletedPipelines int64
	FailedPipelines   int64
	CancelledPipelines int64
	AverageExecutionTime time.Duration
	TotalOperations   int64
	BatchedOperations int64
}

// GetStats returns pipeline execution statistics
func (pm *PipelineManager) GetStats() *PipelineStats {
	pm.pipelinesMux.RLock()
	defer pm.pipelinesMux.RUnlock()

	stats := &PipelineStats{}
	var totalDuration time.Duration
	var completedCount int64

	for _, pipeline := range pm.pipelines {
		stats.TotalPipelines++

		switch pipeline.Status {
		case PipelineStatusCompleted:
			stats.CompletedPipelines++
			completedCount++
			if !pipeline.StartedAt.IsZero() && !pipeline.CompletedAt.IsZero() {
				totalDuration += pipeline.CompletedAt.Sub(pipeline.StartedAt)
			}
		case PipelineStatusFailed:
			stats.FailedPipelines++
		case PipelineStatusCancelled:
			stats.CancelledPipelines++
		}

		stats.TotalOperations += int64(len(pipeline.Operations))
	}

	if completedCount > 0 {
		stats.AverageExecutionTime = totalDuration / time.Duration(completedCount)
	}

	return stats
}