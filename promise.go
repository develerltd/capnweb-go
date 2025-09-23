package capnweb

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Promise represents the result of an RPC call that may not have completed yet
// It supports chaining (pipelining) and can be awaited for the final result
type Promise struct {
	// Session reference for making further calls
	session *Session

	// Export ID for this promise (if it represents a remote operation result)
	exportID ExportID

	// Stub for chained operations
	stub Stub

	// Property path for pipelined operations
	path PropertyPath

	// State management
	state      promiseState
	result     interface{}
	err        error
	stateMutex sync.RWMutex

	// Waiting and notification
	waiters    []chan struct{}
	waitersMux sync.Mutex

	// Metadata
	created time.Time

	// Configuration
	timeout time.Duration
}

// promiseState represents the current state of a promise
type promiseState int32

const (
	promiseStatePending promiseState = iota
	promiseStateResolved
	promiseStateRejected
)

// NewPromise creates a new promise
func NewPromise(session *Session, exportID ExportID) *Promise {
	return &Promise{
		session:  session,
		exportID: exportID,
		state:    promiseStatePending,
		created:  time.Now(),
		timeout:  30 * time.Second, // Default timeout
	}
}

// NewPromiseWithStub creates a new promise with an associated stub for chaining
func NewPromiseWithStub(session *Session, stub Stub, path PropertyPath) *Promise {
	return &Promise{
		session: session,
		stub:    stub,
		path:    path,
		state:   promiseStatePending,
		created: time.Now(),
		timeout: 30 * time.Second,
	}
}

// Then creates a new promise that represents the result of calling a method
// on the result of this promise (promise pipelining)
func (p *Promise) Then(method string, args ...interface{}) *Promise {
	// Create new promise for the chained operation
	newPromise := &Promise{
		session: p.session,
		path:    append(p.path, method),
		state:   promiseStatePending,
		created: time.Now(),
		timeout: p.timeout,
	}

	// If we have a stub, use it for the call
	if p.stub != nil {
		// Clone the stub with extended path
		if stubImpl, ok := p.stub.(*stubImpl); ok {
			newPromise.stub = stubImpl.Clone(method)
		}
	}

	// TODO: In a full implementation, this would create a pipelined RPC call
	// For now, we'll mark it as a chained operation

	return newPromise
}

// Get creates a new promise that represents accessing a property
// on the result of this promise (promise pipelining)
func (p *Promise) Get(property string) *Promise {
	// Create new promise for the property access
	newPromise := &Promise{
		session: p.session,
		path:    append(p.path, property),
		state:   promiseStatePending,
		created: time.Now(),
		timeout: p.timeout,
	}

	// If we have a stub, use it for the property access
	if p.stub != nil {
		if stubImpl, ok := p.stub.(*stubImpl); ok {
			newPromise.stub = stubImpl.Clone(property)
		}
	}

	return newPromise
}

// Await waits for the promise to resolve and returns the result
func (p *Promise) Await(ctx context.Context) (interface{}, error) {
	// Check if already resolved
	p.stateMutex.RLock()
	currentState := p.state
	result := p.result
	err := p.err
	p.stateMutex.RUnlock()

	switch currentState {
	case promiseStateResolved:
		return result, nil
	case promiseStateRejected:
		return nil, err
	case promiseStatePending:
		// Need to wait
	}

	// Create a channel to wait for completion
	done := make(chan struct{})
	p.waitersMux.Lock()
	p.waiters = append(p.waiters, done)
	p.waitersMux.Unlock()

	// Set up timeout context if none provided
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), p.timeout)
		defer cancel()
	}

	// If this promise has an export ID, send a pull request
	if p.exportID != 0 {
		pullMsg := p.session.protocol.NewPullMessage(ImportID(p.exportID))
		if err := p.session.Send(ctx, pullMsg); err != nil {
			return nil, fmt.Errorf("failed to send pull request: %w", err)
		}
	}

	// Wait for completion or context cancellation
	select {
	case <-done:
		p.stateMutex.RLock()
		defer p.stateMutex.RUnlock()
		if p.state == promiseStateResolved {
			return p.result, nil
		}
		return nil, p.err

	case <-ctx.Done():
		return nil, fmt.Errorf("promise await cancelled: %w", ctx.Err())
	}
}

// AwaitWithTimeout waits for the promise with a specific timeout
func (p *Promise) AwaitWithTimeout(timeout time.Duration) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return p.Await(ctx)
}

// IsResolved returns true if the promise has been resolved successfully
func (p *Promise) IsResolved() bool {
	p.stateMutex.RLock()
	defer p.stateMutex.RUnlock()
	return p.state == promiseStateResolved
}

// IsPending returns true if the promise is still pending
func (p *Promise) IsPending() bool {
	p.stateMutex.RLock()
	defer p.stateMutex.RUnlock()
	return p.state == promiseStatePending
}

// IsRejected returns true if the promise was rejected
func (p *Promise) IsRejected() bool {
	p.stateMutex.RLock()
	defer p.stateMutex.RUnlock()
	return p.state == promiseStateRejected
}

// resolve resolves the promise with a value
func (p *Promise) resolve(value interface{}) {
	p.stateMutex.Lock()
	if p.state != promiseStatePending {
		p.stateMutex.Unlock()
		return // Already resolved
	}
	p.state = promiseStateResolved
	p.result = value
	p.stateMutex.Unlock()

	// Notify all waiters
	p.notifyWaiters()

	// Update session statistics
	if p.session != nil && p.session.stats != nil {
		atomic.AddInt64(&p.session.stats.CallsCompleted, 1)
	}
}

// reject rejects the promise with an error
func (p *Promise) reject(err error) {
	p.stateMutex.Lock()
	if p.state != promiseStatePending {
		p.stateMutex.Unlock()
		return // Already resolved
	}
	p.state = promiseStateRejected
	p.err = err
	p.stateMutex.Unlock()

	// Notify all waiters
	p.notifyWaiters()

	// Update session statistics
	if p.session != nil && p.session.stats != nil {
		atomic.AddInt64(&p.session.stats.CallsFailed, 1)
	}
}

// notifyWaiters notifies all waiting goroutines
func (p *Promise) notifyWaiters() {
	p.waitersMux.Lock()
	waiters := p.waiters
	p.waiters = nil // Clear the waiters
	p.waitersMux.Unlock()

	for _, waiter := range waiters {
		close(waiter)
	}
}

// SetTimeout sets the default timeout for this promise
func (p *Promise) SetTimeout(timeout time.Duration) *Promise {
	p.timeout = timeout
	return p
}

// GetTimeout returns the current timeout setting
func (p *Promise) GetTimeout() time.Duration {
	return p.timeout
}

// GetExportID returns the export ID associated with this promise
func (p *Promise) GetExportID() ExportID {
	return p.exportID
}

// GetPath returns the property path for this promise
func (p *Promise) GetPath() PropertyPath {
	// Return a copy to avoid race conditions
	path := make(PropertyPath, len(p.path))
	copy(path, p.path)
	return path
}

// String returns a string representation for debugging
func (p *Promise) String() string {
	p.stateMutex.RLock()
	defer p.stateMutex.RUnlock()

	stateStr := "pending"
	switch p.state {
	case promiseStateResolved:
		stateStr = "resolved"
	case promiseStateRejected:
		stateStr = "rejected"
	}

	if p.exportID != 0 {
		return fmt.Sprintf("Promise{export:%d, path:%v, state:%s}",
			p.exportID, p.path, stateStr)
	}
	return fmt.Sprintf("Promise{path:%v, state:%s}", p.path, stateStr)
}

// PromiseResolver helps manage promise resolution from message handlers
type PromiseResolver struct {
	promises map[ExportID]*Promise
	mutex    sync.RWMutex
}

// NewPromiseResolver creates a new promise resolver
func NewPromiseResolver() *PromiseResolver {
	return &PromiseResolver{
		promises: make(map[ExportID]*Promise),
	}
}

// RegisterPromise registers a promise for later resolution
func (pr *PromiseResolver) RegisterPromise(exportID ExportID, promise *Promise) {
	pr.mutex.Lock()
	defer pr.mutex.Unlock()
	pr.promises[exportID] = promise
}

// ResolvePromise resolves a promise with the given export ID
func (pr *PromiseResolver) ResolvePromise(exportID ExportID, value interface{}) bool {
	pr.mutex.Lock()
	promise, exists := pr.promises[exportID]
	if exists {
		delete(pr.promises, exportID)
	}
	pr.mutex.Unlock()

	if exists {
		promise.resolve(value)
		return true
	}
	return false
}

// RejectPromise rejects a promise with the given export ID
func (pr *PromiseResolver) RejectPromise(exportID ExportID, err error) bool {
	pr.mutex.Lock()
	promise, exists := pr.promises[exportID]
	if exists {
		delete(pr.promises, exportID)
	}
	pr.mutex.Unlock()

	if exists {
		promise.reject(err)
		return true
	}
	return false
}

// GetPendingCount returns the number of pending promises
func (pr *PromiseResolver) GetPendingCount() int {
	pr.mutex.RLock()
	defer pr.mutex.RUnlock()
	return len(pr.promises)
}

// CleanupPromises resolves all pending promises with an error (used during shutdown)
func (pr *PromiseResolver) CleanupPromises(err error) {
	pr.mutex.Lock()
	promises := pr.promises
	pr.promises = make(map[ExportID]*Promise)
	pr.mutex.Unlock()

	for _, promise := range promises {
		promise.reject(err)
	}
}