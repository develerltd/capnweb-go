package capnweb

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ResourceManager handles advanced resource management and cleanup
type ResourceManager struct {
	session *Session

	// Resource tracking
	resources     map[ResourceID]*ManagedResource
	resourcesMux  sync.RWMutex
	nextResourceID int64

	// Lifecycle management
	contexts     map[context.Context]*ContextInfo
	contextsMux  sync.RWMutex

	// Cleanup scheduling
	cleanupQueue    chan *ManagedResource
	cleanupInterval time.Duration
	cleanupWorkers  int

	// Reference tracking
	references     map[interface{}]*ReferenceInfo
	referencesMux  sync.RWMutex

	// Configuration
	options ResourceOptions

	// State
	running int32
	done    chan struct{}
}

// ResourceOptions configures resource management behavior
type ResourceOptions struct {
	// EnableFinalizers uses Go finalizers as backup cleanup
	EnableFinalizers bool

	// CleanupInterval sets how often to run cleanup
	CleanupInterval time.Duration

	// CleanupWorkers sets number of cleanup workers
	CleanupWorkers int

	// MaxIdleTime sets when to cleanup idle resources
	MaxIdleTime time.Duration

	// EnableLeakDetection tracks potential memory leaks
	EnableLeakDetection bool

	// GCInterval sets garbage collection hints interval
	GCInterval time.Duration

	// MaxResources limits total number of tracked resources
	MaxResources int
}

// DefaultResourceOptions returns reasonable defaults
func DefaultResourceOptions() ResourceOptions {
	return ResourceOptions{
		EnableFinalizers:    true,
		CleanupInterval:     30 * time.Second,
		CleanupWorkers:      2,
		MaxIdleTime:         5 * time.Minute,
		EnableLeakDetection: true,
		GCInterval:          1 * time.Minute,
		MaxResources:        10000,
	}
}

// ResourceID uniquely identifies a managed resource
type ResourceID int64

// ManagedResource represents a resource under management
type ManagedResource struct {
	ID          ResourceID
	Resource    interface{}
	Type        ResourceType
	Context     context.Context
	CleanupFunc func() error

	// Lifecycle tracking
	Created      time.Time
	LastAccessed time.Time
	AccessCount  int64
	Disposed     bool

	// Reference counting
	RefCount int32

	// Metadata
	Tags map[string]string
	Size int64 // Estimated memory size

	// Synchronization
	mu sync.RWMutex
}

// ResourceType categorizes different types of resources
type ResourceType int

const (
	ResourceTypeStub ResourceType = iota
	ResourceTypePromise
	ResourceTypeCallback
	ResourceTypeSubscription
	ResourceTypeTransport
	ResourceTypeSession
	ResourceTypeGeneric
)

// String returns string representation of resource type
func (rt ResourceType) String() string {
	switch rt {
	case ResourceTypeStub:
		return "stub"
	case ResourceTypePromise:
		return "promise"
	case ResourceTypeCallback:
		return "callback"
	case ResourceTypeSubscription:
		return "subscription"
	case ResourceTypeTransport:
		return "transport"
	case ResourceTypeSession:
		return "session"
	case ResourceTypeGeneric:
		return "generic"
	default:
		return "unknown"
	}
}

// ContextInfo tracks context-related information
type ContextInfo struct {
	Context     context.Context
	Created     time.Time
	Resources   []ResourceID
	CancelFunc  context.CancelFunc
	Disposed    bool
}

// ReferenceInfo tracks object references for leak detection
type ReferenceInfo struct {
	Object      interface{}
	Type        string
	Created     time.Time
	StackTrace  string
	RefCount    int32
}

// NewResourceManager creates a new resource manager
func NewResourceManager(session *Session, options ...ResourceOptions) *ResourceManager {
	opts := DefaultResourceOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	rm := &ResourceManager{
		session:         session,
		resources:       make(map[ResourceID]*ManagedResource),
		contexts:        make(map[context.Context]*ContextInfo),
		references:      make(map[interface{}]*ReferenceInfo),
		cleanupQueue:    make(chan *ManagedResource, 1000),
		cleanupInterval: opts.CleanupInterval,
		cleanupWorkers:  opts.CleanupWorkers,
		options:         opts,
		done:            make(chan struct{}),
	}

	// Start resource management
	rm.start()

	return rm
}

// TrackResource adds a resource to management
func (rm *ResourceManager) TrackResource(ctx context.Context, resource interface{}, resourceType ResourceType, cleanupFunc func() error) (ResourceID, error) {
	if rm.isStopped() {
		return 0, fmt.Errorf("resource manager is stopped")
	}

	// Check resource limits
	rm.resourcesMux.RLock()
	resourceCount := len(rm.resources)
	rm.resourcesMux.RUnlock()

	if resourceCount >= rm.options.MaxResources {
		return 0, fmt.Errorf("resource limit exceeded (%d)", rm.options.MaxResources)
	}

	// Generate resource ID
	resourceID := ResourceID(atomic.AddInt64(&rm.nextResourceID, 1))

	// Create managed resource
	managedResource := &ManagedResource{
		ID:           resourceID,
		Resource:     resource,
		Type:         resourceType,
		Context:      ctx,
		CleanupFunc:  cleanupFunc,
		Created:      time.Now(),
		LastAccessed: time.Now(),
		RefCount:     1,
		Tags:         make(map[string]string),
		Size:         rm.estimateSize(resource),
	}

	// Add finalizer if enabled
	if rm.options.EnableFinalizers {
		runtime.SetFinalizer(managedResource, (*ManagedResource).finalize)
	}

	// Track the resource
	rm.resourcesMux.Lock()
	rm.resources[resourceID] = managedResource
	rm.resourcesMux.Unlock()

	// Track context association
	rm.trackContext(ctx, resourceID)

	// Track reference if leak detection is enabled
	if rm.options.EnableLeakDetection {
		rm.trackReference(resource)
	}

	return resourceID, nil
}

// UntrackResource removes a resource from management
func (rm *ResourceManager) UntrackResource(resourceID ResourceID) error {
	rm.resourcesMux.Lock()
	resource, exists := rm.resources[resourceID]
	if exists {
		delete(rm.resources, resourceID)
	}
	rm.resourcesMux.Unlock()

	if !exists {
		return fmt.Errorf("resource %d not found", resourceID)
	}

	// Dispose the resource
	return rm.disposeResource(resource)
}

// AccessResource marks a resource as accessed (for idle cleanup)
func (rm *ResourceManager) AccessResource(resourceID ResourceID) error {
	rm.resourcesMux.RLock()
	resource, exists := rm.resources[resourceID]
	rm.resourcesMux.RUnlock()

	if !exists {
		return fmt.Errorf("resource %d not found", resourceID)
	}

	resource.mu.Lock()
	resource.LastAccessed = time.Now()
	resource.AccessCount++
	resource.mu.Unlock()

	return nil
}

// AddRef increments the reference count for a resource
func (rm *ResourceManager) AddRef(resourceID ResourceID) error {
	rm.resourcesMux.RLock()
	resource, exists := rm.resources[resourceID]
	rm.resourcesMux.RUnlock()

	if !exists {
		return fmt.Errorf("resource %d not found", resourceID)
	}

	atomic.AddInt32(&resource.RefCount, 1)
	return nil
}

// Release decrements the reference count and disposes if it reaches zero
func (rm *ResourceManager) Release(resourceID ResourceID) error {
	rm.resourcesMux.RLock()
	resource, exists := rm.resources[resourceID]
	rm.resourcesMux.RUnlock()

	if !exists {
		return fmt.Errorf("resource %d not found", resourceID)
	}

	newRefCount := atomic.AddInt32(&resource.RefCount, -1)
	if newRefCount <= 0 {
		// Schedule for cleanup
		select {
		case rm.cleanupQueue <- resource:
		default:
			// Queue full, cleanup immediately
			go rm.disposeResource(resource)
		}
	}

	return nil
}

// CreateContextualResource creates a resource tied to a specific context
func (rm *ResourceManager) CreateContextualResource(ctx context.Context, factory func(context.Context) (interface{}, func() error, error)) (ResourceID, error) {
	// Create the resource
	resource, cleanupFunc, err := factory(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to create resource: %w", err)
	}

	// Track it
	resourceID, err := rm.TrackResource(ctx, resource, ResourceTypeGeneric, cleanupFunc)
	if err != nil {
		// Cleanup on failure
		if cleanupFunc != nil {
			cleanupFunc()
		}
		return 0, err
	}

	// Set up context cancellation handler
	go rm.watchContext(ctx, resourceID)

	return resourceID, nil
}

// GetResource retrieves a managed resource
func (rm *ResourceManager) GetResource(resourceID ResourceID) (interface{}, error) {
	rm.resourcesMux.RLock()
	resource, exists := rm.resources[resourceID]
	rm.resourcesMux.RUnlock()

	if !exists {
		return nil, fmt.Errorf("resource %d not found", resourceID)
	}

	if resource.Disposed {
		return nil, fmt.Errorf("resource %d is disposed", resourceID)
	}

	// Update access time
	rm.AccessResource(resourceID)

	return resource.Resource, nil
}

// SetResourceTag sets metadata on a resource
func (rm *ResourceManager) SetResourceTag(resourceID ResourceID, key, value string) error {
	rm.resourcesMux.RLock()
	resource, exists := rm.resources[resourceID]
	rm.resourcesMux.RUnlock()

	if !exists {
		return fmt.Errorf("resource %d not found", resourceID)
	}

	resource.mu.Lock()
	resource.Tags[key] = value
	resource.mu.Unlock()

	return nil
}

// GetResourceStats returns statistics about managed resources
func (rm *ResourceManager) GetResourceStats() *ResourceStats {
	rm.resourcesMux.RLock()
	defer rm.resourcesMux.RUnlock()

	stats := &ResourceStats{
		TotalResources: len(rm.resources),
		ResourcesByType: make(map[string]int),
		TotalMemorySize: 0,
	}

	for _, resource := range rm.resources {
		typeStr := resource.Type.String()
		stats.ResourcesByType[typeStr]++
		stats.TotalMemorySize += resource.Size

		if !resource.Disposed {
			stats.ActiveResources++
		}
	}

	// Reference tracking stats
	rm.referencesMux.RLock()
	stats.TrackedReferences = len(rm.references)
	rm.referencesMux.RUnlock()

	// Context stats
	rm.contextsMux.RLock()
	stats.ActiveContexts = len(rm.contexts)
	rm.contextsMux.RUnlock()

	return stats
}

// ResourceStats contains resource management statistics
type ResourceStats struct {
	TotalResources     int
	ActiveResources    int
	ResourcesByType    map[string]int
	TotalMemorySize    int64
	TrackedReferences  int
	ActiveContexts     int
}

// Private methods

func (rm *ResourceManager) start() {
	atomic.StoreInt32(&rm.running, 1)

	// Start cleanup workers
	for i := 0; i < rm.cleanupWorkers; i++ {
		go rm.cleanupWorker()
	}

	// Start periodic cleanup
	go rm.periodicCleanup()

	// Start GC hints
	if rm.options.GCInterval > 0 {
		go rm.gcHints()
	}
}

func (rm *ResourceManager) stop() {
	if !atomic.CompareAndSwapInt32(&rm.running, 1, 0) {
		return // Already stopped
	}

	close(rm.done)

	// Cleanup all resources
	rm.resourcesMux.Lock()
	resources := make([]*ManagedResource, 0, len(rm.resources))
	for _, resource := range rm.resources {
		resources = append(resources, resource)
	}
	rm.resources = make(map[ResourceID]*ManagedResource)
	rm.resourcesMux.Unlock()

	// Dispose all resources
	for _, resource := range resources {
		rm.disposeResource(resource)
	}
}

func (rm *ResourceManager) isStopped() bool {
	return atomic.LoadInt32(&rm.running) == 0
}

func (rm *ResourceManager) cleanupWorker() {
	for {
		select {
		case resource := <-rm.cleanupQueue:
			rm.disposeResource(resource)
		case <-rm.done:
			return
		}
	}
}

func (rm *ResourceManager) periodicCleanup() {
	ticker := time.NewTicker(rm.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rm.cleanupIdleResources()
		case <-rm.done:
			return
		}
	}
}

func (rm *ResourceManager) cleanupIdleResources() {
	now := time.Now()
	idleThreshold := now.Add(-rm.options.MaxIdleTime)

	rm.resourcesMux.RLock()
	idleResources := make([]*ManagedResource, 0)
	for _, resource := range rm.resources {
		if resource.LastAccessed.Before(idleThreshold) && atomic.LoadInt32(&resource.RefCount) <= 0 {
			idleResources = append(idleResources, resource)
		}
	}
	rm.resourcesMux.RUnlock()

	// Cleanup idle resources
	for _, resource := range idleResources {
		select {
		case rm.cleanupQueue <- resource:
		default:
			// Queue full, skip this round
			break
		}
	}
}

func (rm *ResourceManager) disposeResource(resource *ManagedResource) error {
	resource.mu.Lock()
	if resource.Disposed {
		resource.mu.Unlock()
		return nil
	}
	resource.Disposed = true
	resource.mu.Unlock()

	// Remove finalizer
	if rm.options.EnableFinalizers {
		runtime.SetFinalizer(resource, nil)
	}

	// Execute cleanup function
	var err error
	if resource.CleanupFunc != nil {
		err = resource.CleanupFunc()
	}

	// Remove from tracking
	rm.resourcesMux.Lock()
	delete(rm.resources, resource.ID)
	rm.resourcesMux.Unlock()

	// Remove reference tracking
	if rm.options.EnableLeakDetection {
		rm.untrackReference(resource.Resource)
	}

	return err
}

func (rm *ResourceManager) trackContext(ctx context.Context, resourceID ResourceID) {
	rm.contextsMux.Lock()
	defer rm.contextsMux.Unlock()

	contextInfo, exists := rm.contexts[ctx]
	if !exists {
		contextInfo = &ContextInfo{
			Context:   ctx,
			Created:   time.Now(),
			Resources: make([]ResourceID, 0),
		}
		rm.contexts[ctx] = contextInfo
	}

	contextInfo.Resources = append(contextInfo.Resources, resourceID)
}

func (rm *ResourceManager) watchContext(ctx context.Context, resourceID ResourceID) {
	<-ctx.Done()

	// Context cancelled, cleanup resource
	rm.UntrackResource(resourceID)
}

func (rm *ResourceManager) trackReference(obj interface{}) {
	if !rm.options.EnableLeakDetection {
		return
	}

	rm.referencesMux.Lock()
	defer rm.referencesMux.Unlock()

	stackTrace := ""
	if rm.options.EnableLeakDetection {
		buf := make([]byte, 1024*4)
		n := runtime.Stack(buf, false)
		stackTrace = string(buf[:n])
	}

	refInfo := &ReferenceInfo{
		Object:     obj,
		Type:       fmt.Sprintf("%T", obj),
		Created:    time.Now(),
		StackTrace: stackTrace,
		RefCount:   1,
	}

	rm.references[obj] = refInfo
}

func (rm *ResourceManager) untrackReference(obj interface{}) {
	if !rm.options.EnableLeakDetection {
		return
	}

	rm.referencesMux.Lock()
	defer rm.referencesMux.Unlock()

	delete(rm.references, obj)
}

func (rm *ResourceManager) estimateSize(obj interface{}) int64 {
	// Simplified size estimation
	// In a production implementation, this could use reflection
	// or integration with runtime memory profiling
	switch obj.(type) {
	case string:
		return int64(len(obj.(string)))
	case []byte:
		return int64(len(obj.([]byte)))
	default:
		return 64 // Default estimate
	}
}

func (rm *ResourceManager) gcHints() {
	ticker := time.NewTicker(rm.options.GCInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Suggest garbage collection
			runtime.GC()
		case <-rm.done:
			return
		}
	}
}

// finalize is called by the Go finalizer
func (mr *ManagedResource) finalize() {
	if !mr.Disposed && mr.CleanupFunc != nil {
		// Last resort cleanup
		mr.CleanupFunc()
	}
}

// Stop stops the resource manager and cleans up all resources
func (rm *ResourceManager) Stop() {
	rm.stop()
}

// GetLeakReport returns a report of potential memory leaks
func (rm *ResourceManager) GetLeakReport() *LeakReport {
	if !rm.options.EnableLeakDetection {
		return nil
	}

	rm.referencesMux.RLock()
	defer rm.referencesMux.RUnlock()

	report := &LeakReport{
		Timestamp: time.Now(),
		References: make([]*ReferenceInfo, 0, len(rm.references)),
	}

	for _, refInfo := range rm.references {
		report.References = append(report.References, refInfo)
	}

	return report
}

// LeakReport contains information about potential memory leaks
type LeakReport struct {
	Timestamp  time.Time
	References []*ReferenceInfo
}