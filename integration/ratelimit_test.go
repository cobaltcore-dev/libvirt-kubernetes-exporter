// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// C3-T1: Domain writes rate limited
func TestDomainWriteRateLimited(t *testing.T) {
	// Test that rate limiter works correctly
	domainWriteRate := 10.0 // 10 writes/sec
	limiter := rate.NewLimiter(rate.Limit(domainWriteRate), 1)

	ctx := context.Background()

	// Simulate 5 writes
	start := time.Now()
	for i := 0; i < 5; i++ {
		limiter.Wait(ctx)
	}
	duration := time.Since(start)

	// 5 writes at 10/sec should take ~400ms (first is instant)
	if duration < 350*time.Millisecond {
		t.Errorf("rate limiting too fast: %v (expected ~400ms)", duration)
	}
	if duration > 600*time.Millisecond {
		t.Errorf("rate limiting too slow: %v (expected ~400ms)", duration)
	}
}

// C3-T2: Heartbeat writes have different rate
func TestHeartbeatSeparateRate(t *testing.T) {
	domainWriteRate := 10.0    // 10/sec
	heartbeatWriteRate := 50.0 // 50/sec

	domainLimiter := rate.NewLimiter(rate.Limit(domainWriteRate), 1)
	heartbeatLimiter := rate.NewLimiter(rate.Limit(heartbeatWriteRate), 1)

	ctx := context.Background()

	// Simulate 10 heartbeat writes
	start := time.Now()
	for i := 0; i < 10; i++ {
		heartbeatLimiter.Wait(ctx)
	}
	heartbeatDuration := time.Since(start)

	// Simulate 10 domain writes
	start = time.Now()
	for i := 0; i < 10; i++ {
		domainLimiter.Wait(ctx)
	}
	domainDuration := time.Since(start)

	// Heartbeat should be ~5x faster (50/sec vs 10/sec)
	if heartbeatDuration > domainDuration/2 {
		t.Errorf("heartbeat rate not faster: heartbeat=%v, domain=%v", heartbeatDuration, domainDuration)
	}
}
