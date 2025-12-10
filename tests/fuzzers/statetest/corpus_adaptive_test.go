// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package statetest

import (
	"math"
	"sync"
	"testing"
	"time"
)

// TestQueueAdjustment verifies the queue adjustment factor calculations.
func TestQueueAdjustment(t *testing.T) {
	tests := []struct {
		name    string
		hpLen   int
		wantMin float64
		wantMax float64
	}{
		{
			name:    "empty queue",
			hpLen:   0,
			wantMin: -0.30,
			wantMax: -0.30,
		},
		{
			name:    "single item",
			hpLen:   1,
			wantMin: -0.21,
			wantMax: -0.19,
		},
		{
			name:    "10 items",
			hpLen:   10,
			wantMin: -0.11,
			wantMax: -0.09,
		},
		{
			name:    "100 items",
			hpLen:   100,
			wantMin: -0.01,
			wantMax: 0.01,
		},
		{
			name:    "1000 items",
			hpLen:   1000,
			wantMin: 0.09,
			wantMax: 0.11,
		},
		{
			name:    "10000 items",
			hpLen:   10000,
			wantMin: 0.14,
			wantMax: 0.16,
		},
		{
			name:    "very large queue",
			hpLen:   100000,
			wantMin: 0.14, // Should cap at +0.15
			wantMax: 0.16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := queueAdjustment(tt.hpLen)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("queueAdjustment(%d) = %v, want in [%v, %v]",
					tt.hpLen, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// TestDroughtAdjustment verifies the drought adjustment factor calculations.
func TestDroughtAdjustment(t *testing.T) {
	threshold := 30 * time.Second

	tests := []struct {
		name          string
		timeSinceFind time.Duration
		threshold     time.Duration
		wantMin       float64
		wantMax       float64
	}{
		{
			name:          "no drought - recent find",
			timeSinceFind: 10 * time.Second,
			threshold:     threshold,
			wantMin:       0.0,
			wantMax:       0.0,
		},
		{
			name:          "at threshold",
			timeSinceFind: 30 * time.Second,
			threshold:     threshold,
			wantMin:       -0.06,
			wantMax:       -0.04,
		},
		{
			name:          "2x threshold",
			timeSinceFind: 60 * time.Second,
			threshold:     threshold,
			wantMin:       -0.11,
			wantMax:       -0.09,
		},
		{
			name:          "4x threshold",
			timeSinceFind: 120 * time.Second,
			threshold:     threshold,
			wantMin:       -0.16,
			wantMax:       -0.14,
		},
		{
			name:          "10x threshold",
			timeSinceFind: 300 * time.Second,
			threshold:     threshold,
			wantMin:       -0.21,
			wantMax:       -0.19,
		},
		{
			name:          "extreme drought - caps at floor",
			timeSinceFind: 3000 * time.Second,
			threshold:     threshold,
			wantMin:       -0.21,
			wantMax:       -0.19,
		},
		{
			name:          "zero threshold returns 0",
			timeSinceFind: 100 * time.Second,
			threshold:     0,
			wantMin:       0.0,
			wantMax:       0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := droughtAdjustment(tt.timeSinceFind, tt.threshold)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("droughtAdjustment(%v, %v) = %v, want in [%v, %v]",
					tt.timeSinceFind, tt.threshold, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// TestSuccessAdjustment verifies the success adjustment factor calculations.
func TestSuccessAdjustment(t *testing.T) {
	tests := []struct {
		name      string
		hpFinds   int64
		hpPicks   int64
		seedFinds int64
		seedPicks int64
		wantMin   float64
		wantMax   float64
	}{
		{
			name:      "insufficient HP picks",
			hpFinds:   10,
			hpPicks:   30,
			seedFinds: 10,
			seedPicks: 100,
			wantMin:   0.0,
			wantMax:   0.0,
		},
		{
			name:      "insufficient seed picks",
			hpFinds:   10,
			hpPicks:   100,
			seedFinds: 10,
			seedPicks: 30,
			wantMin:   0.0,
			wantMax:   0.0,
		},
		{
			name:      "equal efficiency",
			hpFinds:   10,
			hpPicks:   100,
			seedFinds: 10,
			seedPicks: 100,
			wantMin:   -0.01,
			wantMax:   0.01,
		},
		{
			name:      "HP 2x more efficient",
			hpFinds:   20,
			hpPicks:   100,
			seedFinds: 10,
			seedPicks: 100,
			wantMin:   0.09,
			wantMax:   0.11,
		},
		{
			name:      "HP 4x more efficient - caps at +0.15",
			hpFinds:   40,
			hpPicks:   100,
			seedFinds: 10,
			seedPicks: 100,
			wantMin:   0.14,
			wantMax:   0.16,
		},
		{
			name:      "seed 2x more efficient",
			hpFinds:   10,
			hpPicks:   100,
			seedFinds: 20,
			seedPicks: 100,
			wantMin:   -0.11,
			wantMax:   -0.09,
		},
		{
			name:      "seed 4x more efficient - caps at -0.15",
			hpFinds:   10,
			hpPicks:   100,
			seedFinds: 40,
			seedPicks: 100,
			wantMin:   -0.16,
			wantMax:   -0.14,
		},
		{
			name:      "both have zero finds",
			hpFinds:   0,
			hpPicks:   100,
			seedFinds: 0,
			seedPicks: 100,
			wantMin:   0.0,
			wantMax:   0.0,
		},
		{
			name:      "only HP has finds",
			hpFinds:   10,
			hpPicks:   100,
			seedFinds: 0,
			seedPicks: 100,
			wantMin:   0.14,
			wantMax:   0.16,
		},
		{
			name:      "only seed has finds",
			hpFinds:   0,
			hpPicks:   100,
			seedFinds: 10,
			seedPicks: 100,
			wantMin:   -0.16,
			wantMax:   -0.14,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := successAdjustment(tt.hpFinds, tt.hpPicks, tt.seedFinds, tt.seedPicks)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("successAdjustment(%d, %d, %d, %d) = %v, want in [%v, %v]",
					tt.hpFinds, tt.hpPicks, tt.seedFinds, tt.seedPicks, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// TestClampFloat64 verifies the clamping function.
func TestClampFloat64(t *testing.T) {
	tests := []struct {
		name string
		val  float64
		min  float64
		max  float64
		want float64
	}{
		{"within range", 0.5, 0.0, 1.0, 0.5},
		{"at min", 0.0, 0.0, 1.0, 0.0},
		{"at max", 1.0, 0.0, 1.0, 1.0},
		{"below min", -0.5, 0.0, 1.0, 0.0},
		{"above max", 1.5, 0.0, 1.0, 1.0},
		{"negative range", -0.5, -1.0, -0.2, -0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampFloat64(tt.val, tt.min, tt.max)
			if got != tt.want {
				t.Errorf("clampFloat64(%v, %v, %v) = %v, want %v",
					tt.val, tt.min, tt.max, got, tt.want)
			}
		})
	}
}

// TestAdaptiveHPDisabledByDefault verifies adaptive HP is disabled by default.
func TestAdaptiveHPDisabledByDefault(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}, {4, 5, 6}}
	corpus := NewCoverageCorpus(seeds)

	stats := corpus.FullStats()
	if stats.AdaptiveEnabled {
		t.Error("adaptive HP should be disabled by default")
	}

	// EffectiveP should equal the static highPriorityP (0.8)
	if math.Abs(stats.EffectiveP-0.8) > 0.001 {
		t.Errorf("EffectiveP = %v, want 0.8", stats.EffectiveP)
	}
}

// TestAdaptiveHPEnabled verifies adaptive HP can be enabled.
func TestAdaptiveHPEnabled(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}, {4, 5, 6}}
	corpus := NewCoverageCorpus(seeds, WithAdaptiveHP(true))

	stats := corpus.FullStats()
	if !stats.AdaptiveEnabled {
		t.Error("adaptive HP should be enabled")
	}
}

// TestAdaptiveBoundsOption verifies bounds can be configured.
func TestAdaptiveBoundsOption(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}}
	corpus := NewCoverageCorpus(seeds,
		WithAdaptiveHP(true),
		WithAdaptiveBounds(0.3, 0.9),
	)

	// Access internal state for verification
	if corpus.adaptiveMinP != 0.3 {
		t.Errorf("adaptiveMinP = %v, want 0.3", corpus.adaptiveMinP)
	}
	if corpus.adaptiveMaxP != 0.9 {
		t.Errorf("adaptiveMaxP = %v, want 0.9", corpus.adaptiveMaxP)
	}
}

// TestAdaptiveBoundsValidation verifies invalid bounds are rejected.
func TestAdaptiveBoundsValidation(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}}

	tests := []struct {
		name     string
		minP     float64
		maxP     float64
		wantMinP float64
		wantMaxP float64
	}{
		{"valid bounds", 0.3, 0.9, 0.3, 0.9},
		{"min > max ignored", 0.9, 0.3, 0.2, 0.95}, // Defaults
		{"min < 0 ignored", -0.1, 0.9, 0.2, 0.95},
		{"max > 1 ignored", 0.3, 1.5, 0.2, 0.95},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			corpus := NewCoverageCorpus(seeds,
				WithAdaptiveHP(true),
				WithAdaptiveBounds(tt.minP, tt.maxP),
			)
			if corpus.adaptiveMinP != tt.wantMinP {
				t.Errorf("adaptiveMinP = %v, want %v", corpus.adaptiveMinP, tt.wantMinP)
			}
			if corpus.adaptiveMaxP != tt.wantMaxP {
				t.Errorf("adaptiveMaxP = %v, want %v", corpus.adaptiveMaxP, tt.wantMaxP)
			}
		})
	}
}

// TestAdaptiveDroughtThresholdOption verifies drought threshold can be configured.
func TestAdaptiveDroughtThresholdOption(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}}
	corpus := NewCoverageCorpus(seeds,
		WithAdaptiveHP(true),
		WithAdaptiveDroughtThreshold(60*time.Second),
	)

	if corpus.adaptiveDroughtThreshold != 60*time.Second {
		t.Errorf("adaptiveDroughtThreshold = %v, want 60s", corpus.adaptiveDroughtThreshold)
	}
}

// TestAdaptiveWarmupPicksOption verifies warmup picks can be configured.
func TestAdaptiveWarmupPicksOption(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}}
	corpus := NewCoverageCorpus(seeds,
		WithAdaptiveHP(true),
		WithAdaptiveWarmupPicks(1000),
	)

	if corpus.adaptiveWarmupPicks != 1000 {
		t.Errorf("adaptiveWarmupPicks = %v, want 1000", corpus.adaptiveWarmupPicks)
	}
}

// TestRecordFind verifies coverage find recording.
func TestRecordFind(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}}
	corpus := NewCoverageCorpus(seeds, WithAdaptiveHP(true))

	// Record some finds
	corpus.RecordFind(true)  // HP find
	corpus.RecordFind(true)  // HP find
	corpus.RecordFind(false) // Seed find

	stats := corpus.FullStats()
	if stats.HPFinds != 2 {
		t.Errorf("HPFinds = %v, want 2", stats.HPFinds)
	}
	if stats.SeedFinds != 1 {
		t.Errorf("SeedFinds = %v, want 1", stats.SeedFinds)
	}
}

// TestRecordFindConcurrent verifies thread-safety of RecordFind.
func TestRecordFindConcurrent(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}}
	corpus := NewCoverageCorpus(seeds, WithAdaptiveHP(true))

	var wg sync.WaitGroup
	iterations := 1000

	// Concurrent HP finds
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			corpus.RecordFind(true)
		}
	}()

	// Concurrent seed finds
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			corpus.RecordFind(false)
		}
	}()

	wg.Wait()

	stats := corpus.FullStats()
	if stats.HPFinds != int64(iterations) {
		t.Errorf("HPFinds = %v, want %v", stats.HPFinds, iterations)
	}
	if stats.SeedFinds != int64(iterations) {
		t.Errorf("SeedFinds = %v, want %v", stats.SeedFinds, iterations)
	}
}

// TestGetAdaptiveP verifies GetAdaptiveP returns correct values.
func TestGetAdaptiveP(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}}

	// With adaptive disabled
	corpus := NewCoverageCorpus(seeds)
	if p := corpus.GetAdaptiveP(); math.Abs(p-0.8) > 0.001 {
		t.Errorf("GetAdaptiveP() with adaptive disabled = %v, want 0.8", p)
	}

	// With adaptive enabled
	corpus = NewCoverageCorpus(seeds, WithAdaptiveHP(true))
	p := corpus.GetAdaptiveP()
	if p < 0.2 || p > 0.95 {
		t.Errorf("GetAdaptiveP() with adaptive enabled = %v, want in [0.2, 0.95]", p)
	}
}

// TestAdaptivePWithEmptyQueue verifies probability reduces with empty HP queue.
func TestAdaptivePWithEmptyQueue(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}}
	corpus := NewCoverageCorpus(seeds,
		WithAdaptiveHP(true),
		WithHighPriorityProbability(0.8),
		WithAdaptiveWarmupPicks(1), // Set to 1 so first pick has full confidence
	)

	// Do one pick to get past warmup
	corpus.Pop()

	// With empty HP queue, should get -0.30 queue adjustment
	// Base 0.8 + (-0.30) = 0.5
	p := corpus.GetAdaptiveP()
	// Queue adjustment is -0.30 for empty queue
	// Expected: 0.8 - 0.30 = 0.50
	if p < 0.49 || p > 0.51 {
		t.Errorf("GetAdaptiveP() with empty queue = %v, want ~0.50", p)
	}
}

// TestAdaptivePWithFullQueue verifies probability increases with full HP queue.
func TestAdaptivePWithFullQueue(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}}
	corpus := NewCoverageCorpus(seeds,
		WithAdaptiveHP(true),
		WithHighPriorityProbability(0.8),
		WithMaxQueueSize(10000),
		WithAdaptiveWarmupPicks(1), // Set to 1 so first pick has full confidence
	)

	// Add many items to the HP queue
	for i := 0; i < 10000; i++ {
		corpus.AddHighPriority(&PriorityInput{
			Data:          []byte{byte(i), byte(i >> 8), byte(i >> 16)},
			Priority:      100,
			CoverageDelta: 0.001,
			DiscoveredAt:  time.Now(),
		})
	}

	// Do one pick to get past warmup
	corpus.Pop()

	// With 10000 items, should get +0.15 queue adjustment
	// Base 0.8 + 0.15 = 0.95, clamped to max
	p := corpus.GetAdaptiveP()
	if p < 0.94 {
		t.Errorf("GetAdaptiveP() with full queue = %v, want >= 0.94", p)
	}
}

// TestAdaptivePWarmupConfidence verifies warmup phase-in.
func TestAdaptivePWarmupConfidence(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}}
	corpus := NewCoverageCorpus(seeds,
		WithAdaptiveHP(true),
		WithHighPriorityProbability(0.8),
		WithAdaptiveWarmupPicks(500),
	)

	// Before any picks, confidence should be 0, so effectiveP = baseP
	p := corpus.GetAdaptiveP()
	if math.Abs(p-0.8) > 0.01 {
		t.Errorf("GetAdaptiveP() before picks = %v, want ~0.8", p)
	}

	// Simulate picks
	for i := 0; i < 250; i++ {
		corpus.Pop()
	}

	stats := corpus.FullStats()
	// At 250 picks with 500 warmup, confidence should be ~0.5
	if stats.Confidence < 0.4 || stats.Confidence > 0.6 {
		t.Errorf("Confidence at 250 picks = %v, want ~0.5", stats.Confidence)
	}
}

// TestAdaptivePBoundsEnforced verifies probability stays within bounds.
func TestAdaptivePBoundsEnforced(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}}

	tests := []struct {
		name   string
		baseP  float64
		minP   float64
		maxP   float64
		checks func(*CoverageCorpus) float64
	}{
		{
			name:  "clamps to min",
			baseP: 0.1, // Very low base
			minP:  0.2,
			maxP:  0.95,
			checks: func(c *CoverageCorpus) float64 {
				return c.GetAdaptiveP()
			},
		},
		{
			name:  "clamps to max",
			baseP: 0.99, // Very high base
			minP:  0.2,
			maxP:  0.95,
			checks: func(c *CoverageCorpus) float64 {
				// Add items to get positive queue adjustment
				for i := 0; i < 1000; i++ {
					c.AddHighPriority(&PriorityInput{
						Data:          []byte{byte(i)},
						Priority:      100,
						CoverageDelta: 0.001,
						DiscoveredAt:  time.Now(),
					})
				}
				return c.GetAdaptiveP()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			corpus := NewCoverageCorpus(seeds,
				WithAdaptiveHP(true),
				WithHighPriorityProbability(tt.baseP),
				WithAdaptiveBounds(tt.minP, tt.maxP),
				WithAdaptiveWarmupPicks(0),
			)
			p := tt.checks(corpus)
			if p < tt.minP || p > tt.maxP {
				t.Errorf("GetAdaptiveP() = %v, want in [%v, %v]", p, tt.minP, tt.maxP)
			}
		})
	}
}

// TestClearResetsAdaptiveCounters verifies Clear resets adaptive state.
func TestClearResetsAdaptiveCounters(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}}
	corpus := NewCoverageCorpus(seeds, WithAdaptiveHP(true))

	// Record some finds and do some picks
	corpus.RecordFind(true)
	corpus.RecordFind(false)
	for i := 0; i < 10; i++ {
		corpus.Pop()
	}

	// Verify we have data
	stats := corpus.FullStats()
	if stats.HPFinds == 0 && stats.SeedFinds == 0 {
		t.Error("expected non-zero finds before Clear")
	}

	// Clear
	corpus.Clear()

	// Verify reset
	stats = corpus.FullStats()
	if stats.HPFinds != 0 {
		t.Errorf("HPFinds after Clear = %v, want 0", stats.HPFinds)
	}
	if stats.SeedFinds != 0 {
		t.Errorf("SeedFinds after Clear = %v, want 0", stats.SeedFinds)
	}
	if stats.HPPicks != 0 {
		t.Errorf("HPPicks after Clear = %v, want 0", stats.HPPicks)
	}
	if stats.SeedPicks != 0 {
		t.Errorf("SeedPicks after Clear = %v, want 0", stats.SeedPicks)
	}
}

// TestFullStatsAdaptiveFields verifies all adaptive fields are populated.
func TestFullStatsAdaptiveFields(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}}
	corpus := NewCoverageCorpus(seeds, WithAdaptiveHP(true))

	// Add some items and do picks
	corpus.AddHighPriority(&PriorityInput{
		Data:          []byte{10, 20, 30},
		Priority:      100,
		CoverageDelta: 0.01,
		DiscoveredAt:  time.Now(),
	})

	for i := 0; i < 100; i++ {
		corpus.Pop()
	}
	corpus.RecordFind(true)
	corpus.RecordFind(false)

	stats := corpus.FullStats()

	// Verify adaptive fields are populated
	if !stats.AdaptiveEnabled {
		t.Error("AdaptiveEnabled should be true")
	}
	if stats.EffectiveP < 0.2 || stats.EffectiveP > 0.95 {
		t.Errorf("EffectiveP = %v, should be in [0.2, 0.95]", stats.EffectiveP)
	}
	if stats.HPFinds != 1 {
		t.Errorf("HPFinds = %v, want 1", stats.HPFinds)
	}
	if stats.SeedFinds != 1 {
		t.Errorf("SeedFinds = %v, want 1", stats.SeedFinds)
	}
	// Efficiency should be calculated when picks > 0
	totalPicks := stats.HPPicks + stats.SeedPicks
	if totalPicks > 0 && stats.HPPicks > 0 {
		expectedHPEff := float64(stats.HPFinds) / float64(stats.HPPicks)
		if math.Abs(stats.HPEfficiency-expectedHPEff) > 0.0001 {
			t.Errorf("HPEfficiency = %v, want %v", stats.HPEfficiency, expectedHPEff)
		}
	}
	// QueueAdj should be set based on queue length
	if stats.HPQueueLen == 0 && stats.QueueAdj != -0.30 {
		t.Errorf("QueueAdj with empty queue = %v, want -0.30", stats.QueueAdj)
	}
	// Confidence should be calculated
	if stats.Confidence < 0 || stats.Confidence > 1 {
		t.Errorf("Confidence = %v, should be in [0, 1]", stats.Confidence)
	}
}

// TestAdaptivePEdgeCases verifies handling of edge cases.
func TestAdaptivePEdgeCases(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}}

	// Test with zero warmup picks (should have full confidence immediately)
	// When warmupPicks is 0, it means "no warmup phase" so confidence is always 1.0
	corpus := NewCoverageCorpus(seeds,
		WithAdaptiveHP(true),
		WithAdaptiveWarmupPicks(0),
	)

	// Do a pick to verify confidence stays at 1.0
	corpus.Pop()

	stats := corpus.FullStats()
	// With warmupPicks=0, confidence should be 1.0 (no warmup phase)
	if stats.Confidence != 1.0 {
		t.Errorf("Confidence with zero warmup after pick = %v, want 1.0", stats.Confidence)
	}

	// Test that NaN/Inf don't break the system
	// This is hard to trigger directly, so we just verify the result is valid
	p := corpus.GetAdaptiveP()
	if math.IsNaN(p) || math.IsInf(p, 0) {
		t.Errorf("GetAdaptiveP() returned invalid value: %v", p)
	}
}

// TestPopUsesAdaptiveP verifies Pop uses adaptive probability when enabled.
func TestPopUsesAdaptiveP(t *testing.T) {
	seeds := [][]byte{{1, 2, 3}, {4, 5, 6}}

	// Create corpus with adaptive enabled and force low HP probability via empty queue
	corpus := NewCoverageCorpus(seeds,
		WithAdaptiveHP(true),
		WithHighPriorityProbability(0.8),
		WithAdaptiveWarmupPicks(0),
	)

	// With empty HP queue, queue adjustment is -0.30
	// Effective P should be 0.8 - 0.30 = 0.50
	// Since HP queue is empty, all picks should go to seeds
	for i := 0; i < 100; i++ {
		result := corpus.Pop()
		if result == nil {
			t.Error("Pop returned nil with available seeds")
		}
	}

	stats := corpus.FullStats()
	// All picks should be seed picks since HP queue is empty
	if stats.HPPicks != 0 {
		// Note: even with low probability, if HP queue is empty,
		// the check `len(c.hpQueue) > 0` prevents HP picks
		t.Logf("HPPicks = %v (should be 0 since queue is empty)", stats.HPPicks)
	}
	if stats.SeedPicks != 100 {
		t.Errorf("SeedPicks = %v, want 100", stats.SeedPicks)
	}
}

// BenchmarkComputeAdaptiveP measures the overhead of adaptive probability computation.
func BenchmarkComputeAdaptiveP(b *testing.B) {
	seeds := [][]byte{{1, 2, 3}}
	corpus := NewCoverageCorpus(seeds,
		WithAdaptiveHP(true),
		WithMaxQueueSize(10000),
	)

	// Add some items to make it realistic
	for i := 0; i < 1000; i++ {
		corpus.AddHighPriority(&PriorityInput{
			Data:          []byte{byte(i)},
			Priority:      100,
			CoverageDelta: 0.001,
			DiscoveredAt:  time.Now(),
		})
	}

	// Simulate some picks
	corpus.hpPicks = 5000
	corpus.seedPicks = 5000
	corpus.RecordFind(true)
	corpus.RecordFind(false)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = corpus.GetAdaptiveP()
	}
}

// BenchmarkPopWithAdaptive measures Pop overhead with adaptive enabled.
func BenchmarkPopWithAdaptive(b *testing.B) {
	seeds := [][]byte{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}}

	b.Run("adaptive_disabled", func(b *testing.B) {
		corpus := NewCoverageCorpus(seeds)
		for i := 0; i < 100; i++ {
			corpus.AddHighPriority(&PriorityInput{
				Data:          []byte{byte(i)},
				Priority:      100,
				CoverageDelta: 0.001,
				DiscoveredAt:  time.Now(),
			})
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			corpus.Pop()
		}
	})

	b.Run("adaptive_enabled", func(b *testing.B) {
		corpus := NewCoverageCorpus(seeds, WithAdaptiveHP(true))
		for i := 0; i < 100; i++ {
			corpus.AddHighPriority(&PriorityInput{
				Data:          []byte{byte(i)},
				Priority:      100,
				CoverageDelta: 0.001,
				DiscoveredAt:  time.Now(),
			})
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			corpus.Pop()
		}
	})
}
