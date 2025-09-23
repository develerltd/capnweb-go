package capnweb

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// TransportManager handles transport selection, failover, and optimization
type TransportManager struct {
	// Transport providers
	providers map[string]TransportProvider
	mu        sync.RWMutex

	// Active transports
	activeTransports map[string]Transport
	activeMu         sync.RWMutex

	// Failover configuration
	failoverConfig FailoverConfig

	// Health monitoring
	healthChecker *HealthChecker

	// Load balancing
	loadBalancer LoadBalancer

	// Statistics
	stats TransportManagerStats
}

// TransportProvider abstracts transport creation and management
type TransportProvider interface {
	CreateTransport(endpoint string) (Transport, error)
	GetTransportType() string
	GetPriority() int
	SupportsEndpoint(endpoint string) bool
	GetHealthCheckConfig() HealthCheckConfig
}

// FailoverConfig configures failover behavior
type FailoverConfig struct {
	EnableFailover     bool
	FailoverTimeout    time.Duration
	MaxFailoverAttempts int
	BackoffMultiplier   float64
	HealthCheckInterval time.Duration
}

// DefaultFailoverConfig returns reasonable defaults
func DefaultFailoverConfig() FailoverConfig {
	return FailoverConfig{
		EnableFailover:      true,
		FailoverTimeout:     5 * time.Second,
		MaxFailoverAttempts: 3,
		BackoffMultiplier:   2.0,
		HealthCheckInterval: 30 * time.Second,
	}
}

// HealthCheckConfig configures health checking for transports
type HealthCheckConfig struct {
	Enabled         bool
	CheckInterval   time.Duration
	Timeout         time.Duration
	HealthyThreshold   int
	UnhealthyThreshold int
	CheckMethod     string // "ping", "echo", "custom"
}

// LoadBalancer determines which transport to use for requests
type LoadBalancer interface {
	SelectTransport(transports []Transport, endpoint string) (Transport, error)
	UpdateTransportHealth(transport Transport, healthy bool)
	GetType() string
}

// TransportManagerStats tracks transport manager statistics
type TransportManagerStats struct {
	ActiveTransports   int
	TotalRequests      int64
	FailoverAttempts   int64
	HealthChecksFailed int64
	LoadBalancerStats  map[string]interface{}
}

// NewTransportManager creates a new transport manager
func NewTransportManager(config ...FailoverConfig) *TransportManager {
	failoverCfg := DefaultFailoverConfig()
	if len(config) > 0 {
		failoverCfg = config[0]
	}

	tm := &TransportManager{
		providers:        make(map[string]TransportProvider),
		activeTransports: make(map[string]Transport),
		failoverConfig:   failoverCfg,
		loadBalancer:     NewRoundRobinLoadBalancer(),
	}

	// Start health checker if enabled
	if failoverCfg.EnableFailover {
		tm.healthChecker = NewHealthChecker(failoverCfg.HealthCheckInterval)
		tm.healthChecker.Start()
	}

	return tm
}

// RegisterProvider registers a transport provider
func (tm *TransportManager) RegisterProvider(provider TransportProvider) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	transportType := provider.GetTransportType()
	if _, exists := tm.providers[transportType]; exists {
		return fmt.Errorf("transport provider %s already registered", transportType)
	}

	tm.providers[transportType] = provider
	return nil
}

// GetTransport gets or creates a transport for the given endpoint
func (tm *TransportManager) GetTransport(endpoint string) (Transport, error) {
	// Check if we already have an active transport
	tm.activeMu.RLock()
	if transport, exists := tm.activeTransports[endpoint]; exists {
		tm.activeMu.RUnlock()
		return transport, nil
	}
	tm.activeMu.RUnlock()

	// Find suitable providers
	providers := tm.findSuitableProviders(endpoint)
	if len(providers) == 0 {
		return nil, fmt.Errorf("no suitable transport provider for endpoint: %s", endpoint)
	}

	// Try providers in priority order
	var lastErr error
	for _, provider := range providers {
		transport, err := provider.CreateTransport(endpoint)
		if err != nil {
			lastErr = err
			continue
		}

		// Store active transport
		tm.activeMu.Lock()
		tm.activeTransports[endpoint] = transport
		tm.activeMu.Unlock()

		// Start health monitoring if enabled
		if tm.healthChecker != nil {
			healthConfig := provider.GetHealthCheckConfig()
			tm.healthChecker.MonitorTransport(transport, healthConfig)
		}

		tm.stats.ActiveTransports++
		return transport, nil
	}

	return nil, fmt.Errorf("failed to create transport: %w", lastErr)
}

// GetTransportWithFailover gets a transport with failover support
func (tm *TransportManager) GetTransportWithFailover(endpoint string) (Transport, error) {
	if !tm.failoverConfig.EnableFailover {
		return tm.GetTransport(endpoint)
	}

	var lastErr error
	backoff := time.Second

	for attempt := 0; attempt < tm.failoverConfig.MaxFailoverAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff = time.Duration(float64(backoff) * tm.failoverConfig.BackoffMultiplier)
			tm.stats.FailoverAttempts++
		}

		// Try with timeout
		ctx, cancel := context.WithTimeout(context.Background(), tm.failoverConfig.FailoverTimeout)
		transport, err := tm.getTransportWithContext(ctx, endpoint)
		cancel()

		if err == nil {
			return transport, nil
		}

		lastErr = err
	}

	return nil, fmt.Errorf("failover exhausted after %d attempts: %w",
		tm.failoverConfig.MaxFailoverAttempts, lastErr)
}

// getTransportWithContext gets a transport with context
func (tm *TransportManager) getTransportWithContext(ctx context.Context, endpoint string) (Transport, error) {
	// This would implement context-aware transport creation
	// For now, delegate to regular GetTransport
	return tm.GetTransport(endpoint)
}

// findSuitableProviders finds providers that can handle the endpoint
func (tm *TransportManager) findSuitableProviders(endpoint string) []TransportProvider {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var suitable []TransportProvider

	for _, provider := range tm.providers {
		if provider.SupportsEndpoint(endpoint) {
			suitable = append(suitable, provider)
		}
	}

	// Sort by priority (higher priority first)
	for i := 0; i < len(suitable)-1; i++ {
		for j := i + 1; j < len(suitable); j++ {
			if suitable[i].GetPriority() < suitable[j].GetPriority() {
				suitable[i], suitable[j] = suitable[j], suitable[i]
			}
		}
	}

	return suitable
}

// RemoveTransport removes a transport from management
func (tm *TransportManager) RemoveTransport(endpoint string) error {
	tm.activeMu.Lock()
	defer tm.activeMu.Unlock()

	transport, exists := tm.activeTransports[endpoint]
	if !exists {
		return fmt.Errorf("transport not found for endpoint: %s", endpoint)
	}

	// Close transport
	err := transport.Close()

	// Remove from active transports
	delete(tm.activeTransports, endpoint)

	// Stop health monitoring
	if tm.healthChecker != nil {
		tm.healthChecker.StopMonitoring(transport)
	}

	tm.stats.ActiveTransports--
	return err
}

// SetLoadBalancer sets the load balancing strategy
func (tm *TransportManager) SetLoadBalancer(lb LoadBalancer) {
	tm.loadBalancer = lb
}

// GetStats returns transport manager statistics
func (tm *TransportManager) GetStats() TransportManagerStats {
	tm.activeMu.RLock()
	stats := tm.stats
	stats.ActiveTransports = len(tm.activeTransports)
	tm.activeMu.RUnlock()

	if tm.loadBalancer != nil {
		stats.LoadBalancerStats = map[string]interface{}{
			"type": tm.loadBalancer.GetType(),
		}
	}

	return stats
}

// Close closes all active transports
func (tm *TransportManager) Close() error {
	tm.activeMu.Lock()
	defer tm.activeMu.Unlock()

	var lastErr error

	// Close all active transports
	for endpoint, transport := range tm.activeTransports {
		if err := transport.Close(); err != nil {
			lastErr = err
		}
		delete(tm.activeTransports, endpoint)
	}

	// Stop health checker
	if tm.healthChecker != nil {
		tm.healthChecker.Stop()
	}

	tm.stats.ActiveTransports = 0
	return lastErr
}

// HealthChecker monitors transport health
type HealthChecker struct {
	transports map[Transport]*TransportHealth
	mu         sync.RWMutex
	interval   time.Duration
	stopCh     chan struct{}
	running    bool
}

// TransportHealth tracks health status of a transport
type TransportHealth struct {
	Transport          Transport
	Config             HealthCheckConfig
	Healthy            bool
	ConsecutiveSuccesses int
	ConsecutiveFailures  int
	LastCheck          time.Time
	LastError          error
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(interval time.Duration) *HealthChecker {
	return &HealthChecker{
		transports: make(map[Transport]*TransportHealth),
		interval:   interval,
		stopCh:     make(chan struct{}),
	}
}

// Start starts the health checker
func (hc *HealthChecker) Start() {
	hc.mu.Lock()
	if hc.running {
		hc.mu.Unlock()
		return
	}
	hc.running = true
	hc.mu.Unlock()

	go hc.healthCheckLoop()
}

// Stop stops the health checker
func (hc *HealthChecker) Stop() {
	hc.mu.Lock()
	if !hc.running {
		hc.mu.Unlock()
		return
	}
	hc.running = false
	hc.mu.Unlock()

	close(hc.stopCh)
}

// MonitorTransport adds a transport to health monitoring
func (hc *HealthChecker) MonitorTransport(transport Transport, config HealthCheckConfig) {
	if !config.Enabled {
		return
	}

	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.transports[transport] = &TransportHealth{
		Transport: transport,
		Config:    config,
		Healthy:   true, // Assume healthy initially
		LastCheck: time.Now(),
	}
}

// StopMonitoring removes a transport from health monitoring
func (hc *HealthChecker) StopMonitoring(transport Transport) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	delete(hc.transports, transport)
}

// healthCheckLoop runs periodic health checks
func (hc *HealthChecker) healthCheckLoop() {
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hc.performHealthChecks()
		case <-hc.stopCh:
			return
		}
	}
}

// performHealthChecks checks health of all monitored transports
func (hc *HealthChecker) performHealthChecks() {
	hc.mu.RLock()
	transports := make([]*TransportHealth, 0, len(hc.transports))
	for _, th := range hc.transports {
		transports = append(transports, th)
	}
	hc.mu.RUnlock()

	for _, th := range transports {
		hc.checkTransportHealth(th)
	}
}

// checkTransportHealth performs health check for a single transport
func (hc *HealthChecker) checkTransportHealth(th *TransportHealth) {
	ctx, cancel := context.WithTimeout(context.Background(), th.Config.Timeout)
	defer cancel()

	healthy := hc.performHealthCheck(ctx, th)

	hc.mu.Lock()
	defer hc.mu.Unlock()

	th.LastCheck = time.Now()

	if healthy {
		th.ConsecutiveSuccesses++
		th.ConsecutiveFailures = 0

		if !th.Healthy && th.ConsecutiveSuccesses >= th.Config.HealthyThreshold {
			th.Healthy = true
		}
	} else {
		th.ConsecutiveFailures++
		th.ConsecutiveSuccesses = 0

		if th.Healthy && th.ConsecutiveFailures >= th.Config.UnhealthyThreshold {
			th.Healthy = false
		}
	}
}

// performHealthCheck performs the actual health check
func (hc *HealthChecker) performHealthCheck(ctx context.Context, th *TransportHealth) bool {
	switch th.Config.CheckMethod {
	case "ping":
		return hc.pingCheck(ctx, th.Transport)
	case "echo":
		return hc.echoCheck(ctx, th.Transport)
	default:
		// Default to ping check
		return hc.pingCheck(ctx, th.Transport)
	}
}

// pingCheck performs a simple ping-style health check
func (hc *HealthChecker) pingCheck(ctx context.Context, transport Transport) bool {
	// For this example, we'll assume the transport is healthy if it's not closed
	// In a real implementation, this would send a ping message
	return true
}

// echoCheck performs an echo-style health check
func (hc *HealthChecker) echoCheck(ctx context.Context, transport Transport) bool {
	// Send a test message and wait for echo response
	testMessage := []byte(`{"type":"healthCheck","timestamp":` + fmt.Sprintf("%d", time.Now().UnixNano()) + `}`)

	err := transport.Send(ctx, testMessage)
	if err != nil {
		return false
	}

	// Wait for response (simplified)
	_, err = transport.Receive(ctx)
	return err == nil
}

// Load balancer implementations

// RoundRobinLoadBalancer implements round-robin load balancing
type RoundRobinLoadBalancer struct {
	counter int64
	mu      sync.Mutex
}

// NewRoundRobinLoadBalancer creates a new round-robin load balancer
func NewRoundRobinLoadBalancer() *RoundRobinLoadBalancer {
	return &RoundRobinLoadBalancer{}
}

// SelectTransport selects a transport using round-robin
func (lb *RoundRobinLoadBalancer) SelectTransport(transports []Transport, endpoint string) (Transport, error) {
	if len(transports) == 0 {
		return nil, fmt.Errorf("no transports available")
	}

	lb.mu.Lock()
	index := int(lb.counter % int64(len(transports)))
	lb.counter++
	lb.mu.Unlock()

	return transports[index], nil
}

// UpdateTransportHealth updates transport health status
func (lb *RoundRobinLoadBalancer) UpdateTransportHealth(transport Transport, healthy bool) {
	// Round-robin doesn't use health information for selection
}

// GetType returns the load balancer type
func (lb *RoundRobinLoadBalancer) GetType() string {
	return "round-robin"
}

// WeightedLoadBalancer implements weighted load balancing
type WeightedLoadBalancer struct {
	weights map[Transport]int
	mu      sync.RWMutex
}

// NewWeightedLoadBalancer creates a new weighted load balancer
func NewWeightedLoadBalancer() *WeightedLoadBalancer {
	return &WeightedLoadBalancer{
		weights: make(map[Transport]int),
	}
}

// SetWeight sets the weight for a transport
func (lb *WeightedLoadBalancer) SetWeight(transport Transport, weight int) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.weights[transport] = weight
}

// SelectTransport selects a transport using weighted selection
func (lb *WeightedLoadBalancer) SelectTransport(transports []Transport, endpoint string) (Transport, error) {
	if len(transports) == 0 {
		return nil, fmt.Errorf("no transports available")
	}

	lb.mu.RLock()
	defer lb.mu.RUnlock()

	// Calculate total weight
	totalWeight := 0
	for _, transport := range transports {
		weight := lb.weights[transport]
		if weight <= 0 {
			weight = 1 // Default weight
		}
		totalWeight += weight
	}

	if totalWeight == 0 {
		return transports[0], nil
	}

	// Select based on weight (simplified implementation)
	target := time.Now().UnixNano() % int64(totalWeight)
	current := int64(0)

	for _, transport := range transports {
		weight := lb.weights[transport]
		if weight <= 0 {
			weight = 1
		}
		current += int64(weight)
		if current > target {
			return transport, nil
		}
	}

	return transports[0], nil
}

// UpdateTransportHealth updates transport health status
func (lb *WeightedLoadBalancer) UpdateTransportHealth(transport Transport, healthy bool) {
	// Could adjust weights based on health
}

// GetType returns the load balancer type
func (lb *WeightedLoadBalancer) GetType() string {
	return "weighted"
}

// Provider implementations

// HTTPTransportProvider provides HTTP batch transports
type HTTPTransportProvider struct {
	options  HTTPBatchOptions
	priority int
}

// NewHTTPTransportProvider creates a new HTTP transport provider
func NewHTTPTransportProvider(options HTTPBatchOptions, priority int) *HTTPTransportProvider {
	return &HTTPTransportProvider{
		options:  options,
		priority: priority,
	}
}

// CreateTransport creates an HTTP batch transport
func (p *HTTPTransportProvider) CreateTransport(endpoint string) (Transport, error) {
	return NewHTTPBatchTransport(endpoint, p.options)
}

// GetTransportType returns the transport type
func (p *HTTPTransportProvider) GetTransportType() string {
	return "http"
}

// GetPriority returns the provider priority
func (p *HTTPTransportProvider) GetPriority() int {
	return p.priority
}

// SupportsEndpoint checks if this provider supports the endpoint
func (p *HTTPTransportProvider) SupportsEndpoint(endpoint string) bool {
	return len(endpoint) > 7 && (endpoint[:7] == "http://" || endpoint[:8] == "https://")
}

// GetHealthCheckConfig returns health check configuration
func (p *HTTPTransportProvider) GetHealthCheckConfig() HealthCheckConfig {
	return HealthCheckConfig{
		Enabled:            true,
		CheckInterval:      30 * time.Second,
		Timeout:            5 * time.Second,
		HealthyThreshold:   2,
		UnhealthyThreshold: 3,
		CheckMethod:        "ping",
	}
}

// WebSocketTransportProvider provides WebSocket transports
type WebSocketTransportProvider struct {
	options  WebSocketOptions
	priority int
}

// NewWebSocketTransportProvider creates a new WebSocket transport provider
func NewWebSocketTransportProvider(options WebSocketOptions, priority int) *WebSocketTransportProvider {
	return &WebSocketTransportProvider{
		options:  options,
		priority: priority,
	}
}

// CreateTransport creates a WebSocket transport
func (p *WebSocketTransportProvider) CreateTransport(endpoint string) (Transport, error) {
	transport, err := NewWebSocketTransport(endpoint, p.options)
	if err != nil {
		return nil, err
	}

	// Auto-connect
	ctx, cancel := context.WithTimeout(context.Background(), p.options.HandshakeTimeout)
	defer cancel()

	err = transport.Connect(ctx)
	if err != nil {
		return nil, err
	}

	return transport, nil
}

// GetTransportType returns the transport type
func (p *WebSocketTransportProvider) GetTransportType() string {
	return "websocket"
}

// GetPriority returns the provider priority
func (p *WebSocketTransportProvider) GetPriority() int {
	return p.priority
}

// SupportsEndpoint checks if this provider supports the endpoint
func (p *WebSocketTransportProvider) SupportsEndpoint(endpoint string) bool {
	return len(endpoint) > 5 && (endpoint[:5] == "ws://" || endpoint[:6] == "wss://")
}

// GetHealthCheckConfig returns health check configuration
func (p *WebSocketTransportProvider) GetHealthCheckConfig() HealthCheckConfig {
	return HealthCheckConfig{
		Enabled:            true,
		CheckInterval:      15 * time.Second,
		Timeout:            3 * time.Second,
		HealthyThreshold:   1,
		UnhealthyThreshold: 2,
		CheckMethod:        "echo",
	}
}