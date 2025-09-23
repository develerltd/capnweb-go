package capnweb

import (
	"context"
	"testing"
	"time"
)

func TestStubCreation(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	importID := ImportID(42)
	path := PropertyPath{"user", "profile"}

	stub := newStub(session, &importID, nil, path)

	if stub.GetImportID() == nil || *stub.GetImportID() != importID {
		t.Errorf("Expected import ID %d, got %v", importID, stub.GetImportID())
	}

	if stub.GetExportID() != nil {
		t.Error("Export ID should be nil for import stub")
	}

	stubPath := stub.GetPath()
	if len(stubPath) != 2 || stubPath[0] != "user" || stubPath[1] != "profile" {
		t.Errorf("Expected path [user, profile], got %v", stubPath)
	}

	if stub.IsDisposed() {
		t.Error("New stub should not be disposed")
	}
}

func TestStubCall(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	importID := ImportID(42)
	stub := newStub(session, &importID, nil, PropertyPath{})

	ctx := context.Background()
	args := []interface{}{"arg1", 123, true}

	promise, err := stub.Call(ctx, "testMethod", args...)
	if err != nil {
		t.Fatalf("Failed to make call: %v", err)
	}

	if promise == nil {
		t.Fatal("Promise should not be nil")
	}

	if promise.session != session {
		t.Error("Promise session should match stub session")
	}
}

func TestStubGet(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	importID := ImportID(42)
	stub := newStub(session, &importID, nil, PropertyPath{"user"})

	ctx := context.Background()
	promise, err := stub.Get(ctx, "name")
	if err != nil {
		t.Fatalf("Failed to get property: %v", err)
	}

	if promise == nil {
		t.Fatal("Promise should not be nil")
	}

	// The promise should represent accessing user.name
	expectedPath := PropertyPath{"user", "name"}
	promisePath := promise.GetPath()
	if len(promisePath) != len(expectedPath) {
		t.Errorf("Expected path length %d, got %d", len(expectedPath), len(promisePath))
	}
	for i, segment := range expectedPath {
		if i >= len(promisePath) || promisePath[i] != segment {
			t.Errorf("Expected path %v, got %v", expectedPath, promisePath)
			break
		}
	}
}

func TestStubDispose(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	importID := ImportID(42)
	stub := newStub(session, &importID, nil, PropertyPath{})

	// Add the import to session's table
	session.mu.Lock()
	session.imports[importID] = &importEntry{
		ImportID: importID,
		RefCount: 1,
		session:  session,
		Created:  time.Now(),
	}
	session.mu.Unlock()

	if stub.IsDisposed() {
		t.Error("Stub should not be disposed initially")
	}

	err = stub.Dispose()
	if err != nil {
		t.Errorf("Failed to dispose stub: %v", err)
	}

	if !stub.IsDisposed() {
		t.Error("Stub should be disposed after Dispose()")
	}

	// Double dispose should not error
	err = stub.Dispose()
	if err != nil {
		t.Errorf("Double dispose should not error: %v", err)
	}
}

func TestStubClone(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	importID := ImportID(42)
	originalPath := PropertyPath{"user", "profile"}
	stub := newStub(session, &importID, nil, originalPath)

	// Add to session's imports table
	session.mu.Lock()
	session.imports[importID] = &importEntry{
		ImportID: importID,
		RefCount: 1,
		session:  session,
		Created:  time.Now(),
	}
	session.mu.Unlock()

	clone := stub.Clone("settings", "theme")
	if clone == nil {
		t.Fatal("Clone should not be nil")
	}

	expectedPath := PropertyPath{"user", "profile", "settings", "theme"}
	clonePath := clone.GetPath()
	if len(clonePath) != len(expectedPath) {
		t.Errorf("Expected clone path length %d, got %d", len(expectedPath), len(clonePath))
	}
	for i, segment := range expectedPath {
		if i >= len(clonePath) || clonePath[i] != segment {
			t.Errorf("Expected clone path %v, got %v", expectedPath, clonePath)
			break
		}
	}

	// Original stub should be unchanged
	originalPathCheck := stub.GetPath()
	if len(originalPathCheck) != len(originalPath) {
		t.Error("Original stub path should be unchanged")
	}
}

func TestStubRefCounting(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	importID := ImportID(42)
	stub := newStub(session, &importID, nil, PropertyPath{})

	initialRefCount := stub.GetRefCount()
	if initialRefCount != 1 {
		t.Errorf("Expected initial ref count 1, got %d", initialRefCount)
	}

	stub.AddRef()
	if stub.GetRefCount() != 2 {
		t.Errorf("Expected ref count 2 after AddRef(), got %d", stub.GetRefCount())
	}

	err = stub.Release()
	if err != nil {
		t.Errorf("Failed to release: %v", err)
	}
	if stub.GetRefCount() != 1 {
		t.Errorf("Expected ref count 1 after Release(), got %d", stub.GetRefCount())
	}

	if stub.IsDisposed() {
		t.Error("Stub should not be disposed with ref count > 0")
	}

	// Final release should dispose
	err = stub.Release()
	if err != nil {
		t.Errorf("Failed to final release: %v", err)
	}

	if !stub.IsDisposed() {
		t.Error("Stub should be disposed when ref count reaches 0")
	}
}

func TestStubFactory(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	factory := NewStubFactory(session)

	// Test import stub creation
	importID := ImportID(42)
	importStub := factory.CreateImportStub(importID)
	if importStub.GetImportID() == nil || *importStub.GetImportID() != importID {
		t.Errorf("Expected import ID %d, got %v", importID, importStub.GetImportID())
	}

	// Test export stub creation
	exportID := ExportID(24)
	exportStub := factory.CreateExportStub(exportID)
	if exportStub.GetExportID() == nil || *exportStub.GetExportID() != exportID {
		t.Errorf("Expected export ID %d, got %v", exportID, exportStub.GetExportID())
	}

	// Test local stub creation
	localStub := factory.CreateLocalStub()
	if localStub.GetImportID() != nil {
		t.Error("Local stub should not have import ID")
	}
	if localStub.GetExportID() != nil {
		t.Error("Local stub should not have export ID")
	}
}

func TestStubCallOnLocalExport(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	exportID := ExportID(42)
	stub := newStub(session, nil, &exportID, PropertyPath{})

	ctx := context.Background()
	_, err = stub.Call(ctx, "testMethod")
	if err == nil {
		t.Error("Should not be able to call method on local export stub")
	}
}

func TestStubGetOnLocalExport(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	exportID := ExportID(42)
	stub := newStub(session, nil, &exportID, PropertyPath{})

	ctx := context.Background()
	_, err = stub.Get(ctx, "property")
	if err == nil {
		t.Error("Should not be able to get property on local export stub")
	}
}

func TestStubString(t *testing.T) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// Test import stub string representation
	importID := ImportID(42)
	importStub := newStub(session, &importID, nil, PropertyPath{"user", "profile"})
	importStr := importStub.String()
	if importStr == "" {
		t.Error("Stub string representation should not be empty")
	}
	t.Logf("Import stub: %s", importStr)

	// Test export stub string representation
	exportID := ExportID(24)
	exportStub := newStub(session, nil, &exportID, PropertyPath{})
	exportStr := exportStub.String()
	if exportStr == "" {
		t.Error("Stub string representation should not be empty")
	}
	t.Logf("Export stub: %s", exportStr)

	// Test local stub string representation
	localStub := newStub(session, nil, nil, PropertyPath{"local"})
	localStr := localStub.String()
	if localStr == "" {
		t.Error("Stub string representation should not be empty")
	}
	t.Logf("Local stub: %s", localStr)
}

// Benchmark tests
func BenchmarkStubCreation(b *testing.B) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		b.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	importID := ImportID(42)
	path := PropertyPath{"user", "profile"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stub := newStub(session, &importID, nil, path)
		_ = stub
	}
}

func BenchmarkStubCall(b *testing.B) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		b.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	importID := ImportID(42)
	stub := newStub(session, &importID, nil, PropertyPath{})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := stub.Call(ctx, "testMethod", "arg1", 123)
		if err != nil {
			b.Fatalf("Failed to make call: %v", err)
		}
	}
}

func BenchmarkStubClone(b *testing.B) {
	transport := &MemoryTransport{}
	session, err := NewSession(transport)
	if err != nil {
		b.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	importID := ImportID(42)
	stub := newStub(session, &importID, nil, PropertyPath{"user"})

	// Add to session's imports table
	session.mu.Lock()
	session.imports[importID] = &importEntry{
		ImportID: importID,
		RefCount: 1,
		session:  session,
		Created:  time.Now(),
	}
	session.mu.Unlock()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		clone := stub.Clone("profile")
		_ = clone
	}
}