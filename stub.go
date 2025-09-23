package capnweb

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Stub represents a remote object reference that can be called via RPC
type Stub interface {
	// Call invokes a method on the remote object
	Call(ctx context.Context, method string, args ...interface{}) (*Promise, error)

	// Get retrieves a property from the remote object
	Get(ctx context.Context, property string) (*Promise, error)

	// Set sets a property on the remote object
	Set(ctx context.Context, property string, value interface{}) error

	// Dispose releases the remote reference
	Dispose() error

	// GetImportID returns the import ID if this is a remote stub
	GetImportID() *ImportID

	// GetExportID returns the export ID if this is a local stub
	GetExportID() *ExportID

	// GetPath returns the property path for pipelined operations
	GetPath() PropertyPath

	// IsDisposed returns true if the stub has been disposed
	IsDisposed() bool
}

// stubImpl implements the Stub interface
type stubImpl struct {
	// Session reference
	session *Session

	// Remote reference (if any)
	importID *ImportID
	exportID *ExportID

	// Property path for pipelined operations
	path PropertyPath

	// State management
	disposed int32 // atomic flag
	refCount int32 // atomic reference count

	// Synchronization
	mu sync.RWMutex

	// Metadata
	created time.Time
}

// newStub creates a new stub instance
func newStub(session *Session, importID *ImportID, exportID *ExportID, path PropertyPath) *stubImpl {
	stub := &stubImpl{
		session:  session,
		importID: importID,
		exportID: exportID,
		path:     path,
		refCount: 1,
		created:  time.Now(),
	}

	// Set finalizer as a safety net for cleanup
	// Note: In production, explicit Dispose() should always be called
	// runtime.SetFinalizer(stub, (*stubImpl).dispose)

	return stub
}

// Call invokes a method on the remote object
func (s *stubImpl) Call(ctx context.Context, method string, args ...interface{}) (*Promise, error) {
	if atomic.LoadInt32(&s.disposed) != 0 {
		return nil, fmt.Errorf("stub has been disposed")
	}

	if s.session == nil {
		return nil, fmt.Errorf("stub has no session")
	}

	if s.importID == nil {
		return nil, fmt.Errorf("cannot call method on local export stub")
	}

	// Increment call statistics
	if s.session.stats != nil {
		atomic.AddInt64(&s.session.stats.CallsStarted, 1)
	}

	// Create method call path
	methodPath := append(s.path, method)

	// Create pipeline expression for the method call
	expr := NewPipelineExpression(*s.importID, methodPath, args)

	// Generate new export ID for the result
	resultExportID, err := s.session.exportValue(nil) // Placeholder for promise result
	if err != nil {
		if s.session.stats != nil {
			atomic.AddInt64(&s.session.stats.CallsFailed, 1)
		}
		return nil, fmt.Errorf("failed to create result export: %w", err)
	}

	// Send push message
	pushMsg := s.session.protocol.NewPushMessage(expr, &resultExportID)
	if err := s.session.Send(ctx, pushMsg); err != nil {
		if s.session.stats != nil {
			atomic.AddInt64(&s.session.stats.CallsFailed, 1)
		}
		return nil, fmt.Errorf("failed to send call: %w", err)
	}

	// Create promise for the result
	promise := &Promise{
		session:  s.session,
		exportID: resultExportID,
		path:     PropertyPath{},
		created:  time.Now(),
	}

	return promise, nil
}

// Get retrieves a property from the remote object
func (s *stubImpl) Get(ctx context.Context, property string) (*Promise, error) {
	if atomic.LoadInt32(&s.disposed) != 0 {
		return nil, fmt.Errorf("stub has been disposed")
	}

	if s.session == nil {
		return nil, fmt.Errorf("stub has no session")
	}

	if s.importID == nil {
		return nil, fmt.Errorf("cannot get property on local export stub")
	}

	// Create property access path
	propertyPath := append(s.path, property)

	// For property access, we create a new stub with extended path
	// This enables promise pipelining
	newStub := newStub(s.session, s.importID, nil, propertyPath)

	// Create a promise that represents this property access
	promise := &Promise{
		session: s.session,
		stub:    newStub,
		path:    propertyPath,
		created: time.Now(),
	}

	return promise, nil
}

// Set sets a property on the remote object
func (s *stubImpl) Set(ctx context.Context, property string, value interface{}) error {
	if atomic.LoadInt32(&s.disposed) != 0 {
		return fmt.Errorf("stub has been disposed")
	}

	if s.session == nil {
		return fmt.Errorf("stub has no session")
	}

	if s.importID == nil {
		return fmt.Errorf("cannot set property on local export stub")
	}

	// Create property set path - this would need special handling
	// For now, we'll treat it as a method call to a setter
	setterMethod := fmt.Sprintf("set_%s", property)
	_, err := s.Call(ctx, setterMethod, value)
	return err
}

// Dispose releases the remote reference
func (s *stubImpl) Dispose() error {
	return s.dispose()
}

func (s *stubImpl) dispose() error {
	// Check if already disposed
	if !atomic.CompareAndSwapInt32(&s.disposed, 0, 1) {
		return nil // Already disposed
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session != nil && s.importID != nil {
		// Send release message for remote reference
		releaseMsg := s.session.protocol.NewReleaseMessage(*s.importID, 1)
		if err := s.session.Send(context.Background(), releaseMsg); err != nil {
			// Log error but don't fail disposal
			// In production, you might want to retry or queue for later
		}

		// Remove from session's import table
		s.session.mu.Lock()
		if entry, exists := s.session.imports[*s.importID]; exists {
			if atomic.AddInt32(&entry.RefCount, -1) <= 0 {
				delete(s.session.imports, *s.importID)
				if s.session.stats != nil {
					atomic.AddInt64(&s.session.stats.ImportsActive, -1)
				}
			}
		}
		s.session.mu.Unlock()
	}

	// Clear finalizer
	// runtime.SetFinalizer(s, nil)

	return nil
}

// GetImportID returns the import ID if this is a remote stub
func (s *stubImpl) GetImportID() *ImportID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.importID
}

// GetExportID returns the export ID if this is a local stub
func (s *stubImpl) GetExportID() *ExportID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.exportID
}

// GetPath returns the property path for pipelined operations
func (s *stubImpl) GetPath() PropertyPath {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Return a copy to avoid race conditions
	pathCopy := make(PropertyPath, len(s.path))
	copy(pathCopy, s.path)
	return pathCopy
}

// IsDisposed returns true if the stub has been disposed
func (s *stubImpl) IsDisposed() bool {
	return atomic.LoadInt32(&s.disposed) != 0
}

// AddRef increments the reference count
func (s *stubImpl) AddRef() {
	atomic.AddInt32(&s.refCount, 1)
}

// Release decrements the reference count and disposes if it reaches zero
func (s *stubImpl) Release() error {
	if atomic.AddInt32(&s.refCount, -1) <= 0 {
		return s.dispose()
	}
	return nil
}

// GetRefCount returns the current reference count
func (s *stubImpl) GetRefCount() int32 {
	return atomic.LoadInt32(&s.refCount)
}

// Clone creates a new stub with the same target but extended path
func (s *stubImpl) Clone(additionalPath ...string) *stubImpl {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if atomic.LoadInt32(&s.disposed) != 0 {
		return nil
	}

	newPath := make(PropertyPath, len(s.path)+len(additionalPath))
	copy(newPath, s.path)
	copy(newPath[len(s.path):], additionalPath)

	clone := newStub(s.session, s.importID, s.exportID, newPath)

	// If this is a remote reference, increment the session's ref count
	if s.importID != nil && s.session != nil {
		s.session.mu.Lock()
		if entry, exists := s.session.imports[*s.importID]; exists {
			atomic.AddInt32(&entry.RefCount, 1)
		}
		s.session.mu.Unlock()
	}

	return clone
}

// String returns a string representation of the stub for debugging
func (s *stubImpl) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	disposed := atomic.LoadInt32(&s.disposed) != 0
	refCount := atomic.LoadInt32(&s.refCount)

	if s.importID != nil {
		return fmt.Sprintf("Stub{import:%d, path:%v, refs:%d, disposed:%t}",
			*s.importID, s.path, refCount, disposed)
	}
	if s.exportID != nil {
		return fmt.Sprintf("Stub{export:%d, path:%v, refs:%d, disposed:%t}",
			*s.exportID, s.path, refCount, disposed)
	}
	return fmt.Sprintf("Stub{local, path:%v, refs:%d, disposed:%t}",
		s.path, refCount, disposed)
}

// StubFactory creates stubs for the session
type StubFactory struct {
	session *Session
}

// NewStubFactory creates a new stub factory
func NewStubFactory(session *Session) *StubFactory {
	return &StubFactory{session: session}
}

// CreateImportStub creates a stub for a remote import
func (f *StubFactory) CreateImportStub(importID ImportID) Stub {
	return newStub(f.session, &importID, nil, PropertyPath{})
}

// CreateExportStub creates a stub for a local export
func (f *StubFactory) CreateExportStub(exportID ExportID) Stub {
	return newStub(f.session, nil, &exportID, PropertyPath{})
}

// CreateLocalStub creates a stub for a local object (no RPC)
func (f *StubFactory) CreateLocalStub() Stub {
	return newStub(f.session, nil, nil, PropertyPath{})
}