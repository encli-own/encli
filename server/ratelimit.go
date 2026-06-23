package main

import (
	"sync"
	"time"
)

// RateLimiter реализует token bucket rate limiter per IP.
type RateLimiter struct {
	mu       sync.RWMutex
	buckets  map[string]*bucket
	rate     float64 // tokens per second
	burst    int     // maximum bucket size
	
	// Cleanup
	cleanupInterval time.Duration
	stopCh          chan struct{}
}

type bucket struct {
	tokens    float64
	lastCheck time.Time
	mu        sync.Mutex
}

// NewRateLimiter создает новый rate limiter.
func NewRateLimiter(requestsPerSecond, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets:         make(map[string]*bucket),
		rate:            float64(requestsPerSecond),
		burst:           burst,
		cleanupInterval: 5 * time.Minute,
		stopCh:          make(chan struct{}),
	}
	
	// Запускаем cleanup goroutine
	go rl.cleanupLoop()
	
	return rl
}

// Allow проверяет, разрешено ли действие для данного ключа (IP).
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.RLock()
	b, exists := rl.buckets[key]
	rl.mu.RUnlock()
	
	if !exists {
		rl.mu.Lock()
		b = &bucket{
			tokens:    float64(rl.burst) - 1, // Потребляем 1 токен
			lastCheck: time.Now(),
		}
		rl.buckets[key] = b
		rl.mu.Unlock()
		return true
	}
	
	b.mu.Lock()
	defer b.mu.Unlock()
	
	// Пополняем токены
	now := time.Now()
	elapsed := now.Sub(b.lastCheck).Seconds()
	b.tokens = min(float64(rl.burst), b.tokens+elapsed*rl.rate)
	b.lastCheck = now
	
	// Проверяем, есть ли токен
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	
	return false
}

// cleanupLoop периодически удаляет неиспользуемые buckets.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanupInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-rl.stopCh:
			return
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for key, b := range rl.buckets {
				b.mu.Lock()
				if now.Sub(b.lastCheck) > rl.cleanupInterval {
					delete(rl.buckets, key)
				}
				b.mu.Unlock()
			}
			rl.mu.Unlock()
		}
	}
}

// Stop останавливает cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stopCh)
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
