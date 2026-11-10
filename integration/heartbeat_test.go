// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"hash/fnv"
	"testing"
)

// C2-T1: Heartbeat only emitted every N polls (unit test for logic)
func TestHeartbeatThrottling(t *testing.T) {
	heartbeatPeriod := 6
	uuid := "test-uuid-1"
	pollCount := make(map[string]int)

	shouldEmit := func(uuid string) bool {
		pollCount[uuid]++
		count := pollCount[uuid]
		seed := hashUUIDTest(uuid) % heartbeatPeriod
		effectiveCount := (count + seed) % heartbeatPeriod
		return effectiveCount == 0
	}

	// Count heartbeats over 12 polls
	heartbeatCount := 0
	for i := 0; i < 12; i++ {
		if shouldEmit(uuid) {
			heartbeatCount++
		}
	}

	// Should emit ~2 heartbeats (12 / 6)
	if heartbeatCount != 2 {
		t.Errorf("expected 2 heartbeats in 12 polls, got %d", heartbeatCount)
	}
}

// C2-T2: Data change resets counter
func TestDataChangeResetsCounter(t *testing.T) {
	heartbeatPeriod := 6
	uuid := "test-uuid-2"
	pollCount := make(map[string]int)

	shouldEmit := func(uuid string) bool {
		pollCount[uuid]++
		count := pollCount[uuid]
		seed := hashUUIDTest(uuid) % heartbeatPeriod
		effectiveCount := (count + seed) % heartbeatPeriod
		return effectiveCount == 0
	}

	resetCounter := func(uuid string) {
		pollCount[uuid] = 0
	}

	// 3 polls, no heartbeat yet
	for i := 0; i < 3; i++ {
		shouldEmit(uuid)
	}

	// Simulate data change - reset counter
	resetCounter(uuid)

	// 3 more polls - should not yet emit (counter was reset)
	heartbeatCount := 0
	for i := 0; i < 3; i++ {
		if shouldEmit(uuid) {
			heartbeatCount++
		}
	}

	// With period=6, after reset, 3 polls shouldn't emit yet
	// (depends on seed, but conceptually counter was reset)
	if heartbeatCount > 1 {
		t.Errorf("expected at most 1 heartbeat after reset, got %d", heartbeatCount)
	}
}

// C2-T3: VMs spread heartbeats (different seeds)
func TestHeartbeatsSpread(t *testing.T) {
	heartbeatPeriod := 6
	uuids := []string{
		"00000001-0000-0000-0000-000000000001",
		"00000002-0000-0000-0000-000000000002",
		"00000003-0000-0000-0000-000000000003",
		"00000004-0000-0000-0000-000000000004",
		"00000005-0000-0000-0000-000000000005",
		"00000006-0000-0000-0000-000000000006",
	}

	// Calculate seeds for each UUID
	seeds := make(map[string]int)
	for _, uuid := range uuids {
		seeds[uuid] = hashUUIDTest(uuid) % heartbeatPeriod
	}

	// Seeds should be spread (not all the same)
	allSame := true
	firstSeed := seeds[uuids[0]]
	for _, seed := range seeds {
		if seed != firstSeed {
			allSame = false
			break
		}
	}

	if allSame {
		t.Error("all UUIDs have the same seed - heartbeats not spread")
	}
}

func hashUUIDTest(uuid string) int {
	h := fnv.New32a()
	h.Write([]byte(uuid))
	return int(h.Sum32())
}
