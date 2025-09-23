package capnweb

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RateLimiter defines the interface for rate limiting implementations
type RateLimiter interface {
	// Allow checks if a request is allowed under the current rate limit
	Allow(key string) bool

	// AllowN checks if N requests are allowed under the current rate limit
	AllowN(key string, n int) bool

	// Wait blocks until a request is allowed or context is canceled
	Wait(ctx context.Context, key string) error

	// Reserve reserves a slot and returns the time until it's available
	Reserve(key string) (time.Duration, error)

	// GetLimit returns the current rate limit configuration
	GetLimit(key string) (int, time.Duration)

	// SetLimit updates the rate limit for a key
	SetLimit(key string, requests int, window time.Duration) error

	// Reset resets the rate limiter for a key
	Reset(key string)

	// Stats returns rate limiting statistics
	Stats(key string) RateLimitStats
}

// RateLimitStats tracks rate limiting statistics
type RateLimitStats struct {
	Key              string        `json:"key"`
	RequestsAllowed  int64         `json:"requestsAllowed"`
	RequestsDenied   int64         `json:"requestsDenied"`
	CurrentTokens    int           `json:"currentTokens"`
	MaxTokens        int           `json:"maxTokens"`
	RefillRate       time.Duration `json:"refillRate"`
	LastRefill       time.Time     `json:"lastRefill"`
	WindowStart      time.Time     `json:"windowStart"`
	RequestsInWindow int           `json:"requestsInWindow"`
}

// TokenBucketLimiter implements a token bucket rate limiter
type TokenBucketLimiter struct {
	buckets   map[string]*tokenBucket
	bucketsMu sync.RWMutex
	config    TokenBucketConfig
}

// TokenBucketConfig configures token bucket behavior
type TokenBucketConfig struct {
	DefaultCapacity int           // Default bucket capacity
	DefaultRefill   time.Duration // Default refill interval
	CleanupInterval time.Duration // How often to clean up unused buckets
	MaxBuckets      int           // Maximum number of buckets to maintain
}

// tokenBucket represents a single token bucket
type tokenBucket struct {
	capacity     int
	tokens       int
	refillRate   time.Duration
	lastRefill   time.Time
	mu           sync.Mutex
	stats        RateLimitStats
}

// DefaultTokenBucketConfig returns reasonable defaults
func DefaultTokenBucketConfig() TokenBucketConfig {
	return TokenBucketConfig{
		DefaultCapacity: 100,
		DefaultRefill:   time.Second,
		CleanupInterval: 5 * time.Minute,
		MaxBuckets:      10000,
	}
}

// NewTokenBucketLimiter creates a new token bucket rate limiter
func NewTokenBucketLimiter(config TokenBucketConfig) *TokenBucketLimiter {
	limiter := &TokenBucketLimiter{
		buckets: make(map[string]*tokenBucket),
		config:  config,
	}

	// Start cleanup goroutine
	go limiter.cleanupLoop()

	return limiter
}

// Allow implements RateLimiter.Allow
func (tbl *TokenBucketLimiter) Allow(key string) bool {
	return tbl.AllowN(key, 1)
}

// AllowN implements RateLimiter.AllowN
func (tbl *TokenBucketLimiter) AllowN(key string, n int) bool {
	bucket := tbl.getBucket(key)
	return bucket.takeTokens(n)
}

// Wait implements RateLimiter.Wait
func (tbl *TokenBucketLimiter) Wait(ctx context.Context, key string) error {
	for {
		if tbl.Allow(key) {
			return nil
		}

		// Calculate wait time
		duration, err := tbl.Reserve(key)
		if err != nil {
			return err
		}

		// Wait for the calculated duration or context cancellation
		timer := time.NewTimer(duration)
		select {
		case <-timer.C:
			// Continue to next iteration
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}

// Reserve implements RateLimiter.Reserve
func (tbl *TokenBucketLimiter) Reserve(key string) (time.Duration, error) {
	bucket := tbl.getBucket(key)
	return bucket.reserveTime(), nil
}

// GetLimit implements RateLimiter.GetLimit
func (tbl *TokenBucketLimiter) GetLimit(key string) (int, time.Duration) {
	bucket := tbl.getBucket(key)
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	return bucket.capacity, bucket.refillRate
}

// SetLimit implements RateLimiter.SetLimit
func (tbl *TokenBucketLimiter) SetLimit(key string, requests int, window time.Duration) error {
	bucket := tbl.getBucket(key)
	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	bucket.capacity = requests
	bucket.refillRate = window
	if bucket.tokens > requests {
		bucket.tokens = requests
	}

	return nil
}

// Reset implements RateLimiter.Reset
func (tbl *TokenBucketLimiter) Reset(key string) {
	tbl.bucketsMu.Lock()
	defer tbl.bucketsMu.Unlock()
	delete(tbl.buckets, key)
}

// Stats implements RateLimiter.Stats
func (tbl *TokenBucketLimiter) Stats(key string) RateLimitStats {
	bucket := tbl.getBucket(key)
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	return bucket.stats
}

// getBucket retrieves or creates a bucket for the given key
func (tbl *TokenBucketLimiter) getBucket(key string) *tokenBucket {
	tbl.bucketsMu.RLock()
	bucket, exists := tbl.buckets[key]
	tbl.bucketsMu.RUnlock()

	if exists {
		return bucket
	}

	// Create new bucket
	tbl.bucketsMu.Lock()
	defer tbl.bucketsMu.Unlock()

	// Double-check after acquiring write lock
	if bucket, exists := tbl.buckets[key]; exists {
		return bucket
	}

	// Check if we've hit the maximum number of buckets
	if len(tbl.buckets) >= tbl.config.MaxBuckets {
		// Remove oldest bucket (simple cleanup)
		for k := range tbl.buckets {
			delete(tbl.buckets, k)
			break
		}
	}

	bucket = &tokenBucket{
		capacity:   tbl.config.DefaultCapacity,
		tokens:     tbl.config.DefaultCapacity,
		refillRate: tbl.config.DefaultRefill,
		lastRefill: time.Now(),
		stats: RateLimitStats{
			Key:              key,
			CurrentTokens:    tbl.config.DefaultCapacity,
			MaxTokens:        tbl.config.DefaultCapacity,
			RefillRate:       tbl.config.DefaultRefill,
			LastRefill:       time.Now(),
		},
	}

	tbl.buckets[key] = bucket
	return bucket
}

// takeTokens attempts to take n tokens from the bucket
func (tb *tokenBucket) takeTokens(n int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	if tb.tokens >= n {
		tb.tokens -= n
		tb.stats.RequestsAllowed++
		tb.stats.CurrentTokens = tb.tokens
		return true
	}

	tb.stats.RequestsDenied++
	return false
}

// refill adds tokens to the bucket based on elapsed time
func (tb *tokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)

	if elapsed >= tb.refillRate {
		intervals := int(elapsed / tb.refillRate)
		tokensToAdd := intervals

		tb.tokens += tokensToAdd
		if tb.tokens > tb.capacity {
			tb.tokens = tb.capacity
		}

		tb.lastRefill = now
		tb.stats.LastRefill = now
		tb.stats.CurrentTokens = tb.tokens
	}
}

// reserveTime calculates how long to wait for tokens to be available
func (tb *tokenBucket) reserveTime() time.Duration {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	if tb.tokens > 0 {
		return 0
	}

	// Calculate time until next refill
	timeSinceLastRefill := time.Since(tb.lastRefill)
	timeUntilNextRefill := tb.refillRate - timeSinceLastRefill

	if timeUntilNextRefill < 0 {
		return 0
	}

	return timeUntilNextRefill
}

// cleanupLoop periodically removes unused buckets
func (tbl *TokenBucketLimiter) cleanupLoop() {
	ticker := time.NewTicker(tbl.config.CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		tbl.cleanup()
	}
}

// cleanup removes buckets that haven't been used recently
func (tbl *TokenBucketLimiter) cleanup() {
	tbl.bucketsMu.Lock()
	defer tbl.bucketsMu.Unlock()

	cutoff := time.Now().Add(-tbl.config.CleanupInterval)

	for key, bucket := range tbl.buckets {
		bucket.mu.Lock()
		if bucket.lastRefill.Before(cutoff) {
			delete(tbl.buckets, key)
		}
		bucket.mu.Unlock()
	}
}

// QuotaManager manages resource quotas and usage tracking
type QuotaManager struct {
	quotas    map[string]*Quota
	quotasMu  sync.RWMutex
	config    QuotaConfig
}

// QuotaConfig configures quota management
type QuotaConfig struct {
	DefaultQuota    int64         // Default quota amount
	ResetInterval   time.Duration // How often quotas reset
	CleanupInterval time.Duration // How often to clean up unused quotas
	MaxQuotas       int           // Maximum number of quotas to track
}

// Quota represents a resource quota
type Quota struct {
	Key         string    `json:"key"`
	Limit       int64     `json:"limit"`
	Used        int64     `json:"used"`
	WindowStart time.Time `json:"windowStart"`
	WindowEnd   time.Time `json:"windowEnd"`
	ResetPeriod time.Duration `json:"resetPeriod"`
	mu          sync.Mutex
	stats       QuotaStats
}

// QuotaStats tracks quota usage statistics
type QuotaStats struct {
	TotalRequests     int64     `json:"totalRequests"`
	AllowedRequests   int64     `json:"allowedRequests"`
	DeniedRequests    int64     `json:"deniedRequests"`
	QuotaExceeded     int64     `json:"quotaExceeded"`
	LastReset         time.Time `json:"lastReset"`
	AverageUsage      float64   `json:"averageUsage"`
}

// DefaultQuotaConfig returns reasonable quota defaults
func DefaultQuotaConfig() QuotaConfig {
	return QuotaConfig{
		DefaultQuota:    10000,
		ResetInterval:   24 * time.Hour,
		CleanupInterval: time.Hour,
		MaxQuotas:       10000,
	}
}

// NewQuotaManager creates a new quota manager
func NewQuotaManager(config QuotaConfig) *QuotaManager {
	manager := &QuotaManager{
		quotas: make(map[string]*Quota),
		config: config,
	}

	// Start cleanup and reset goroutines
	go manager.cleanupLoop()
	go manager.resetLoop()

	return manager
}

// CheckQuota checks if a request can be allowed under the quota
func (qm *QuotaManager) CheckQuota(key string, amount int64) bool {
	quota := qm.getQuota(key)
	return quota.checkUsage(amount)
}

// UseQuota consumes quota and returns whether it was successful
func (qm *QuotaManager) UseQuota(key string, amount int64) bool {
	quota := qm.getQuota(key)
	return quota.use(amount)
}

// GetQuotaInfo returns current quota information
func (qm *QuotaManager) GetQuotaInfo(key string) QuotaInfo {
	quota := qm.getQuota(key)
	quota.mu.Lock()
	defer quota.mu.Unlock()

	return QuotaInfo{
		Key:          key,
		Limit:        quota.Limit,
		Used:         quota.Used,
		Remaining:    quota.Limit - quota.Used,
		WindowStart:  quota.WindowStart,
		WindowEnd:    quota.WindowEnd,
		ResetIn:      time.Until(quota.WindowEnd),
	}
}

// SetQuota sets the quota limit for a key
func (qm *QuotaManager) SetQuota(key string, limit int64, resetPeriod time.Duration) {
	quota := qm.getQuota(key)
	quota.mu.Lock()
	defer quota.mu.Unlock()

	quota.Limit = limit
	quota.ResetPeriod = resetPeriod
	quota.resetWindow()
}

// ResetQuota resets the usage for a key
func (qm *QuotaManager) ResetQuota(key string) {
	quota := qm.getQuota(key)
	quota.reset()
}

// GetStats returns quota statistics
func (qm *QuotaManager) GetStats(key string) QuotaStats {
	quota := qm.getQuota(key)
	quota.mu.Lock()
	defer quota.mu.Unlock()
	return quota.stats
}

// QuotaInfo provides quota information
type QuotaInfo struct {
	Key         string        `json:"key"`
	Limit       int64         `json:"limit"`
	Used        int64         `json:"used"`
	Remaining   int64         `json:"remaining"`
	WindowStart time.Time     `json:"windowStart"`
	WindowEnd   time.Time     `json:"windowEnd"`
	ResetIn     time.Duration `json:"resetIn"`
}

// getQuota retrieves or creates a quota for the given key
func (qm *QuotaManager) getQuota(key string) *Quota {
	qm.quotasMu.RLock()
	quota, exists := qm.quotas[key]
	qm.quotasMu.RUnlock()

	if exists {
		return quota
	}

	// Create new quota
	qm.quotasMu.Lock()
	defer qm.quotasMu.Unlock()

	// Double-check after acquiring write lock
	if quota, exists := qm.quotas[key]; exists {
		return quota
	}

	// Check if we've hit the maximum number of quotas
	if len(qm.quotas) >= qm.config.MaxQuotas {
		// Remove oldest quota (simple cleanup)
		for k := range qm.quotas {
			delete(qm.quotas, k)
			break
		}
	}

	now := time.Now()
	quota = &Quota{
		Key:         key,
		Limit:       qm.config.DefaultQuota,
		Used:        0,
		WindowStart: now,
		WindowEnd:   now.Add(qm.config.ResetInterval),
		ResetPeriod: qm.config.ResetInterval,
		stats: QuotaStats{
			LastReset: now,
		},
	}

	qm.quotas[key] = quota
	return quota
}

// checkUsage checks if the quota allows the requested amount
func (q *Quota) checkUsage(amount int64) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.checkReset()
	return q.Used + amount <= q.Limit
}

// use consumes quota if available
func (q *Quota) use(amount int64) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.checkReset()
	q.stats.TotalRequests++

	if q.Used + amount <= q.Limit {
		q.Used += amount
		q.stats.AllowedRequests++
		q.updateAverageUsage()
		return true
	}

	q.stats.DeniedRequests++
	q.stats.QuotaExceeded++
	return false
}

// reset resets the quota usage
func (q *Quota) reset() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.resetWindow()
}

// checkReset checks if the quota window should be reset
func (q *Quota) checkReset() {
	if time.Now().After(q.WindowEnd) {
		q.resetWindow()
	}
}

// resetWindow resets the quota window
func (q *Quota) resetWindow() {
	now := time.Now()
	q.Used = 0
	q.WindowStart = now
	q.WindowEnd = now.Add(q.ResetPeriod)
	q.stats.LastReset = now
}

// updateAverageUsage updates the average usage percentage
func (q *Quota) updateAverageUsage() {
	if q.Limit > 0 {
		currentUsage := float64(q.Used) / float64(q.Limit) * 100
		if q.stats.AverageUsage == 0 {
			q.stats.AverageUsage = currentUsage
		} else {
			// Simple moving average
			q.stats.AverageUsage = (q.stats.AverageUsage + currentUsage) / 2
		}
	}
}

// cleanupLoop periodically removes unused quotas
func (qm *QuotaManager) cleanupLoop() {
	ticker := time.NewTicker(qm.config.CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		qm.cleanup()
	}
}

// cleanup removes quotas that haven't been used recently
func (qm *QuotaManager) cleanup() {
	qm.quotasMu.Lock()
	defer qm.quotasMu.Unlock()

	cutoff := time.Now().Add(-qm.config.CleanupInterval * 2)

	for key, quota := range qm.quotas {
		quota.mu.Lock()
		if quota.stats.LastReset.Before(cutoff) && quota.Used == 0 {
			delete(qm.quotas, key)
		}
		quota.mu.Unlock()
	}
}

// resetLoop periodically resets quotas
func (qm *QuotaManager) resetLoop() {
	ticker := time.NewTicker(time.Minute) // Check every minute
	defer ticker.Stop()

	for range ticker.C {
		qm.resetExpiredQuotas()
	}
}

// resetExpiredQuotas resets quotas whose windows have expired
func (qm *QuotaManager) resetExpiredQuotas() {
	qm.quotasMu.RLock()
	quotas := make([]*Quota, 0, len(qm.quotas))
	for _, quota := range qm.quotas {
		quotas = append(quotas, quota)
	}
	qm.quotasMu.RUnlock()

	for _, quota := range quotas {
		quota.mu.Lock()
		quota.checkReset()
		quota.mu.Unlock()
	}
}

// RateLimitQuotaManager combines rate limiting and quota management
type RateLimitQuotaManager struct {
	rateLimiter  RateLimiter
	quotaManager *QuotaManager
}

// NewRateLimitQuotaManager creates a combined rate limit and quota manager
func NewRateLimitQuotaManager(rateLimiterConfig TokenBucketConfig, quotaConfig QuotaConfig) *RateLimitQuotaManager {
	return &RateLimitQuotaManager{
		rateLimiter:  NewTokenBucketLimiter(rateLimiterConfig),
		quotaManager: NewQuotaManager(quotaConfig),
	}
}

// CheckRequest checks both rate limit and quota for a request
func (rlqm *RateLimitQuotaManager) CheckRequest(key string, quotaAmount int64) error {
	// Check rate limit first (it's cheaper)
	if !rlqm.rateLimiter.Allow(key) {
		return fmt.Errorf("rate limit exceeded for %s", key)
	}

	// Check quota
	if !rlqm.quotaManager.CheckQuota(key, quotaAmount) {
		return fmt.Errorf("quota exceeded for %s", key)
	}

	return nil
}

// UseRequest consumes both rate limit and quota for a request
func (rlqm *RateLimitQuotaManager) UseRequest(key string, quotaAmount int64) error {
	// Check rate limit first
	if !rlqm.rateLimiter.Allow(key) {
		return fmt.Errorf("rate limit exceeded for %s", key)
	}

	// Use quota
	if !rlqm.quotaManager.UseQuota(key, quotaAmount) {
		return fmt.Errorf("quota exceeded for %s", key)
	}

	return nil
}

// WaitForRequest waits for rate limit and checks quota
func (rlqm *RateLimitQuotaManager) WaitForRequest(ctx context.Context, key string, quotaAmount int64) error {
	// Wait for rate limit
	if err := rlqm.rateLimiter.Wait(ctx, key); err != nil {
		return fmt.Errorf("rate limit wait failed: %w", err)
	}

	// Check quota
	if !rlqm.quotaManager.UseQuota(key, quotaAmount) {
		return fmt.Errorf("quota exceeded for %s", key)
	}

	return nil
}

// GetRateLimitStats returns rate limiting statistics
func (rlqm *RateLimitQuotaManager) GetRateLimitStats(key string) RateLimitStats {
	return rlqm.rateLimiter.Stats(key)
}

// GetQuotaStats returns quota statistics
func (rlqm *RateLimitQuotaManager) GetQuotaStats(key string) QuotaStats {
	return rlqm.quotaManager.GetStats(key)
}

// GetQuotaInfo returns quota information
func (rlqm *RateLimitQuotaManager) GetQuotaInfo(key string) QuotaInfo {
	return rlqm.quotaManager.GetQuotaInfo(key)
}

// SetRateLimit sets the rate limit for a key
func (rlqm *RateLimitQuotaManager) SetRateLimit(key string, requests int, window time.Duration) error {
	return rlqm.rateLimiter.SetLimit(key, requests, window)
}

// SetQuota sets the quota for a key
func (rlqm *RateLimitQuotaManager) SetQuota(key string, limit int64, resetPeriod time.Duration) {
	rlqm.quotaManager.SetQuota(key, limit, resetPeriod)
}

// Reset resets both rate limit and quota for a key
func (rlqm *RateLimitQuotaManager) Reset(key string) {
	rlqm.rateLimiter.Reset(key)
	rlqm.quotaManager.ResetQuota(key)
}