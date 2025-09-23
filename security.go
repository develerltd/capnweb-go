package capnweb

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Capability represents a permission to access specific resources and operations
type Capability struct {
	ID          string            `json:"id"`
	Resource    string            `json:"resource"`    // Resource identifier (e.g., "user.123", "file.abc")
	Operations  []string          `json:"operations"`  // Allowed operations (e.g., "read", "write", "execute")
	Attributes  map[string]string `json:"attributes"`  // Additional attributes/constraints
	IssuedAt    time.Time         `json:"issuedAt"`
	ExpiresAt   *time.Time        `json:"expiresAt,omitempty"`
	Issuer      string            `json:"issuer"`
	Subject     string            `json:"subject"`     // Who this capability is issued to
	Delegatable bool              `json:"delegatable"` // Can this capability be delegated?
	Used        bool              `json:"used"`        // Has this capability been used?
	UseCount    int               `json:"useCount"`    // Number of times used
	MaxUses     int               `json:"maxUses"`     // Maximum allowed uses (0 = unlimited)
}

// IsValid checks if the capability is currently valid
func (c *Capability) IsValid() bool {
	if c.MaxUses > 0 && c.UseCount >= c.MaxUses {
		return false
	}

	if c.ExpiresAt != nil && time.Now().After(*c.ExpiresAt) {
		return false
	}

	return true
}

// CanPerform checks if this capability allows a specific operation on a resource
func (c *Capability) CanPerform(resource, operation string) bool {
	if !c.IsValid() {
		return false
	}

	// Check resource match (supports wildcards)
	if !c.matchesResource(resource) {
		return false
	}

	// Check operation
	for _, op := range c.Operations {
		if op == operation || op == "*" {
			return true
		}
	}

	return false
}

// matchesResource checks if the capability applies to the given resource
func (c *Capability) matchesResource(resource string) bool {
	if c.Resource == "*" {
		return true
	}

	if c.Resource == resource {
		return true
	}

	// Support prefix matching with wildcards
	if strings.HasSuffix(c.Resource, "*") {
		prefix := strings.TrimSuffix(c.Resource, "*")
		return strings.HasPrefix(resource, prefix)
	}

	return false
}

// Use marks the capability as used and increments the use count
func (c *Capability) Use() error {
	if !c.IsValid() {
		return fmt.Errorf("capability is not valid")
	}

	c.UseCount++
	if c.MaxUses > 0 && c.UseCount >= c.MaxUses {
		c.Used = true
	}

	return nil
}

// Clone creates a copy of the capability for delegation
func (c *Capability) Clone() *Capability {
	clone := *c

	// Copy slices and maps
	clone.Operations = make([]string, len(c.Operations))
	copy(clone.Operations, c.Operations)

	clone.Attributes = make(map[string]string)
	for k, v := range c.Attributes {
		clone.Attributes[k] = v
	}

	return &clone
}

// CapabilityStore manages capabilities and their validation
type CapabilityStore interface {
	// Store saves a capability
	Store(capability *Capability) error

	// Retrieve gets a capability by ID
	Retrieve(capabilityID string) (*Capability, error)

	// Validate checks if a capability is valid and can perform an operation
	Validate(capabilityID, resource, operation string) (*Capability, error)

	// Revoke invalidates a capability
	Revoke(capabilityID string) error

	// List returns all capabilities for a subject
	List(subject string) ([]*Capability, error)

	// Cleanup removes expired capabilities
	Cleanup() error
}

// MemoryCapabilityStore is an in-memory implementation of CapabilityStore
type MemoryCapabilityStore struct {
	capabilities map[string]*Capability
	mutex        sync.RWMutex
}

// NewMemoryCapabilityStore creates a new in-memory capability store
func NewMemoryCapabilityStore() *MemoryCapabilityStore {
	return &MemoryCapabilityStore{
		capabilities: make(map[string]*Capability),
	}
}

// Store implements CapabilityStore.Store
func (s *MemoryCapabilityStore) Store(capability *Capability) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.capabilities[capability.ID] = capability
	return nil
}

// Retrieve implements CapabilityStore.Retrieve
func (s *MemoryCapabilityStore) Retrieve(capabilityID string) (*Capability, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	capability, exists := s.capabilities[capabilityID]
	if !exists {
		return nil, fmt.Errorf("capability not found: %s", capabilityID)
	}

	return capability.Clone(), nil
}

// Validate implements CapabilityStore.Validate
func (s *MemoryCapabilityStore) Validate(capabilityID, resource, operation string) (*Capability, error) {
	capability, err := s.Retrieve(capabilityID)
	if err != nil {
		return nil, err
	}

	if !capability.CanPerform(resource, operation) {
		return nil, fmt.Errorf("capability %s does not allow %s on %s", capabilityID, operation, resource)
	}

	// Mark as used
	s.mutex.Lock()
	stored := s.capabilities[capabilityID]
	if stored != nil {
		stored.Use()
	}
	s.mutex.Unlock()

	return capability, nil
}

// Revoke implements CapabilityStore.Revoke
func (s *MemoryCapabilityStore) Revoke(capabilityID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.capabilities, capabilityID)
	return nil
}

// List implements CapabilityStore.List
func (s *MemoryCapabilityStore) List(subject string) ([]*Capability, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var result []*Capability
	for _, capability := range s.capabilities {
		if capability.Subject == subject {
			result = append(result, capability.Clone())
		}
	}

	return result, nil
}

// Cleanup implements CapabilityStore.Cleanup
func (s *MemoryCapabilityStore) Cleanup() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now()
	for id, capability := range s.capabilities {
		if !capability.IsValid() || (capability.ExpiresAt != nil && now.After(*capability.ExpiresAt)) {
			delete(s.capabilities, id)
		}
	}

	return nil
}

// SecurityManager handles authentication, authorization, and capability management
type SecurityManager struct {
	capabilityStore CapabilityStore
	authProviders   map[string]AuthProvider
	sessions        map[string]*SecurityContext
	sessionsMux     sync.RWMutex
	config          SecurityConfig
}

// SecurityConfig configures security behavior
type SecurityConfig struct {
	// RequireAuthentication requires all RPC calls to be authenticated
	RequireAuthentication bool

	// RequireAuthorization requires capabilities for all operations
	RequireAuthorization bool

	// DefaultCapabilityTTL is the default time-to-live for capabilities
	DefaultCapabilityTTL time.Duration

	// MaxCapabilitiesPerSubject limits capabilities per subject
	MaxCapabilitiesPerSubject int

	// EnableCapabilityDelegation allows capability delegation
	EnableCapabilityDelegation bool

	// SessionTimeout is how long security contexts remain valid
	SessionTimeout time.Duration

	// CleanupInterval is how often expired items are cleaned up
	CleanupInterval time.Duration
}

// DefaultSecurityConfig returns reasonable security defaults
func DefaultSecurityConfig() SecurityConfig {
	return SecurityConfig{
		RequireAuthentication:     true,
		RequireAuthorization:      true,
		DefaultCapabilityTTL:      24 * time.Hour,
		MaxCapabilitiesPerSubject: 100,
		EnableCapabilityDelegation: true,
		SessionTimeout:            30 * time.Minute,
		CleanupInterval:           5 * time.Minute,
	}
}

// SecurityContext represents the security context for a session
type SecurityContext struct {
	SessionID    string            `json:"sessionId"`
	Subject      string            `json:"subject"`
	Authenticated bool             `json:"authenticated"`
	Capabilities []string          `json:"capabilities"` // Capability IDs
	Attributes   map[string]string `json:"attributes"`
	CreatedAt    time.Time         `json:"createdAt"`
	LastUsed     time.Time         `json:"lastUsed"`
}

// IsValid checks if the security context is still valid
func (sc *SecurityContext) IsValid(timeout time.Duration) bool {
	return time.Since(sc.LastUsed) < timeout
}

// AuthProvider handles authentication for different auth schemes
type AuthProvider interface {
	// Authenticate verifies credentials and returns a subject identifier
	Authenticate(ctx context.Context, credentials map[string]interface{}) (string, error)

	// GetAuthScheme returns the authentication scheme name
	GetAuthScheme() string
}

// TokenAuthProvider implements authentication using bearer tokens
type TokenAuthProvider struct {
	validTokens map[string]string // token -> subject
	mutex       sync.RWMutex
}

// NewTokenAuthProvider creates a new token-based auth provider
func NewTokenAuthProvider() *TokenAuthProvider {
	return &TokenAuthProvider{
		validTokens: make(map[string]string),
	}
}

// AddToken adds a valid token for a subject
func (p *TokenAuthProvider) AddToken(token, subject string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.validTokens[token] = subject
}

// RemoveToken removes a token
func (p *TokenAuthProvider) RemoveToken(token string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	delete(p.validTokens, token)
}

// Authenticate implements AuthProvider.Authenticate
func (p *TokenAuthProvider) Authenticate(ctx context.Context, credentials map[string]interface{}) (string, error) {
	token, ok := credentials["token"].(string)
	if !ok {
		return "", fmt.Errorf("token not provided")
	}

	p.mutex.RLock()
	defer p.mutex.RUnlock()

	subject, exists := p.validTokens[token]
	if !exists {
		return "", fmt.Errorf("invalid token")
	}

	return subject, nil
}

// GetAuthScheme implements AuthProvider.GetAuthScheme
func (p *TokenAuthProvider) GetAuthScheme() string {
	return "bearer"
}

// NewSecurityManager creates a new security manager
func NewSecurityManager(config SecurityConfig) *SecurityManager {
	sm := &SecurityManager{
		capabilityStore: NewMemoryCapabilityStore(),
		authProviders:   make(map[string]AuthProvider),
		sessions:        make(map[string]*SecurityContext),
		config:          config,
	}

	// Start cleanup goroutine
	go sm.cleanupLoop()

	return sm
}

// RegisterAuthProvider registers an authentication provider
func (sm *SecurityManager) RegisterAuthProvider(provider AuthProvider) {
	sm.authProviders[provider.GetAuthScheme()] = provider
}

// Authenticate authenticates a user and creates a security context
func (sm *SecurityManager) Authenticate(ctx context.Context, scheme string, credentials map[string]interface{}) (*SecurityContext, error) {
	provider, exists := sm.authProviders[scheme]
	if !exists {
		return nil, fmt.Errorf("unsupported auth scheme: %s", scheme)
	}

	subject, err := provider.Authenticate(ctx, credentials)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Create security context
	sessionID := sm.generateSessionID()
	securityContext := &SecurityContext{
		SessionID:     sessionID,
		Subject:       subject,
		Authenticated: true,
		Capabilities:  []string{},
		Attributes:    make(map[string]string),
		CreatedAt:     time.Now(),
		LastUsed:      time.Now(),
	}

	// Store session
	sm.sessionsMux.Lock()
	sm.sessions[sessionID] = securityContext
	sm.sessionsMux.Unlock()

	return securityContext, nil
}

// GetSecurityContext retrieves a security context by session ID
func (sm *SecurityManager) GetSecurityContext(sessionID string) (*SecurityContext, error) {
	sm.sessionsMux.RLock()
	defer sm.sessionsMux.RUnlock()

	context, exists := sm.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	if !context.IsValid(sm.config.SessionTimeout) {
		return nil, fmt.Errorf("session expired: %s", sessionID)
	}

	// Update last used time
	context.LastUsed = time.Now()

	return context, nil
}

// CreateCapability creates a new capability for a subject
func (sm *SecurityManager) CreateCapability(subject, resource string, operations []string, attributes map[string]string) (*Capability, error) {
	// Check if subject has too many capabilities
	existing, err := sm.capabilityStore.List(subject)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing capabilities: %w", err)
	}

	if len(existing) >= sm.config.MaxCapabilitiesPerSubject {
		return nil, fmt.Errorf("subject %s has too many capabilities (%d)", subject, len(existing))
	}

	// Create capability
	capability := &Capability{
		ID:          sm.generateCapabilityID(),
		Resource:    resource,
		Operations:  operations,
		Attributes:  attributes,
		IssuedAt:    time.Now(),
		Issuer:      "security-manager",
		Subject:     subject,
		Delegatable: sm.config.EnableCapabilityDelegation,
		Used:        false,
		UseCount:    0,
		MaxUses:     0, // Unlimited by default
	}

	// Set expiration
	if sm.config.DefaultCapabilityTTL > 0 {
		expiresAt := time.Now().Add(sm.config.DefaultCapabilityTTL)
		capability.ExpiresAt = &expiresAt
	}

	// Store capability
	err = sm.capabilityStore.Store(capability)
	if err != nil {
		return nil, fmt.Errorf("failed to store capability: %w", err)
	}

	return capability, nil
}

// DelegateCapability creates a delegated capability from an existing one
func (sm *SecurityManager) DelegateCapability(originalCapabilityID, newSubject string, restrictedOperations []string) (*Capability, error) {
	if !sm.config.EnableCapabilityDelegation {
		return nil, fmt.Errorf("capability delegation is disabled")
	}

	// Retrieve original capability
	original, err := sm.capabilityStore.Retrieve(originalCapabilityID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve original capability: %w", err)
	}

	if !original.Delegatable {
		return nil, fmt.Errorf("capability %s is not delegatable", originalCapabilityID)
	}

	// Create delegated capability
	delegated := original.Clone()
	delegated.ID = sm.generateCapabilityID()
	delegated.Subject = newSubject
	delegated.Issuer = original.Subject // The original subject becomes the issuer
	delegated.IssuedAt = time.Now()

	// Restrict operations if specified
	if len(restrictedOperations) > 0 {
		delegated.Operations = sm.intersectOperations(original.Operations, restrictedOperations)
	}

	// Delegated capabilities are not delegatable by default
	delegated.Delegatable = false

	// Store delegated capability
	err = sm.capabilityStore.Store(delegated)
	if err != nil {
		return nil, fmt.Errorf("failed to store delegated capability: %w", err)
	}

	return delegated, nil
}

// CheckAuthorization checks if a session has permission to perform an operation
func (sm *SecurityManager) CheckAuthorization(sessionID, resource, operation string) error {
	if !sm.config.RequireAuthorization {
		return nil
	}

	// Get security context
	securityContext, err := sm.GetSecurityContext(sessionID)
	if err != nil {
		return fmt.Errorf("authorization failed: %w", err)
	}

	if sm.config.RequireAuthentication && !securityContext.Authenticated {
		return fmt.Errorf("authentication required")
	}

	// Check capabilities
	for _, capabilityID := range securityContext.Capabilities {
		_, err := sm.capabilityStore.Validate(capabilityID, resource, operation)
		if err == nil {
			return nil // Found a valid capability
		}
	}

	return fmt.Errorf("access denied: no valid capability for %s on %s", operation, resource)
}

// GrantCapability adds a capability to a security context
func (sm *SecurityManager) GrantCapability(sessionID, capabilityID string) error {
	sm.sessionsMux.Lock()
	defer sm.sessionsMux.Unlock()

	context, exists := sm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Verify capability exists and is valid
	_, err := sm.capabilityStore.Retrieve(capabilityID)
	if err != nil {
		return fmt.Errorf("invalid capability: %w", err)
	}

	// Add to session
	context.Capabilities = append(context.Capabilities, capabilityID)
	context.LastUsed = time.Now()

	return nil
}

// RevokeCapability removes a capability from a security context and the store
func (sm *SecurityManager) RevokeCapability(capabilityID string) error {
	// Remove from store
	err := sm.capabilityStore.Revoke(capabilityID)
	if err != nil {
		return fmt.Errorf("failed to revoke capability: %w", err)
	}

	// Remove from all sessions
	sm.sessionsMux.Lock()
	defer sm.sessionsMux.Unlock()

	for _, context := range sm.sessions {
		for i, id := range context.Capabilities {
			if id == capabilityID {
				// Remove from slice
				context.Capabilities = append(context.Capabilities[:i], context.Capabilities[i+1:]...)
				break
			}
		}
	}

	return nil
}

// Logout invalidates a security context
func (sm *SecurityManager) Logout(sessionID string) error {
	sm.sessionsMux.Lock()
	defer sm.sessionsMux.Unlock()

	delete(sm.sessions, sessionID)
	return nil
}

// generateSessionID generates a unique session ID
func (sm *SecurityManager) generateSessionID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// generateCapabilityID generates a unique capability ID
func (sm *SecurityManager) generateCapabilityID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	hash := sha256.Sum256(bytes)
	return hex.EncodeToString(hash[:16])
}

// intersectOperations returns the intersection of two operation slices
func (sm *SecurityManager) intersectOperations(a, b []string) []string {
	var result []string
	for _, opA := range a {
		for _, opB := range b {
			if opA == opB {
				result = append(result, opA)
				break
			}
		}
	}
	return result
}

// cleanupLoop periodically cleans up expired sessions and capabilities
func (sm *SecurityManager) cleanupLoop() {
	ticker := time.NewTicker(sm.config.CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		sm.cleanup()
	}
}

// cleanup removes expired sessions and capabilities
func (sm *SecurityManager) cleanup() {
	// Cleanup expired sessions
	sm.sessionsMux.Lock()
	for sessionID, context := range sm.sessions {
		if !context.IsValid(sm.config.SessionTimeout) {
			delete(sm.sessions, sessionID)
		}
	}
	sm.sessionsMux.Unlock()

	// Cleanup expired capabilities
	sm.capabilityStore.Cleanup()
}

// GetStats returns security manager statistics
func (sm *SecurityManager) GetStats() SecurityStats {
	sm.sessionsMux.RLock()
	activeSessions := len(sm.sessions)
	sm.sessionsMux.RUnlock()

	return SecurityStats{
		ActiveSessions: activeSessions,
		// Other stats would be added here
	}
}

// SecurityStats tracks security-related statistics
type SecurityStats struct {
	ActiveSessions     int
	TotalCapabilities  int
	ExpiredCapabilities int
	AuthAttempts       int64
	AuthSuccesses      int64
	AuthFailures       int64
	AuthorizationChecks int64
	AuthorizationDenials int64
}