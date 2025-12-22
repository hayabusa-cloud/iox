// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"testing"
	"time"

	"code.hybscloud.com/iox"
)

func TestBackoff_ZeroValue(t *testing.T) {
	// Zero-value Backoff should be ready to use with defaults
	var b iox.Backoff

	// Block should return 1 for zero-value
	if got := b.Block(); got != 1 {
		t.Errorf("Block() = %d, want 1", got)
	}

	// Duration should return DefaultBackoffBase for zero-value
	if got := b.Duration(); got != iox.DefaultBackoffBase {
		t.Errorf("Duration() = %v, want %v", got, iox.DefaultBackoffBase)
	}
}

func TestBackoff_ZeroValueWait(t *testing.T) {
	// Zero-value Backoff should work with Wait() without prior configuration
	var b iox.Backoff

	start := time.Now()
	b.Wait()
	elapsed := time.Since(start)

	// Should have waited approximately DefaultBackoffBase (500µs) ± jitter
	// Allow generous tolerance for test stability (OS scheduling adds latency)
	minWait := iox.DefaultBackoffBase * 7 / 8 // -12.5% jitter
	maxWait := iox.DefaultBackoffBase * 10    // generous upper bound for CI/slow systems

	if elapsed < minWait || elapsed > maxWait {
		t.Errorf("Wait() elapsed = %v, expected between %v and %v", elapsed, minWait, maxWait)
	}

	// After first Wait, should be in block 2
	if got := b.Block(); got != 2 {
		t.Errorf("After Wait(), Block() = %d, want 2", got)
	}
}

func TestBackoff_Duration(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*iox.Backoff)
		wantDur time.Duration
		wantBlk int
	}{
		{
			name:    "zero-value",
			setup:   func(b *iox.Backoff) {},
			wantDur: iox.DefaultBackoffBase,
			wantBlk: 1,
		},
		{
			name: "custom base",
			setup: func(b *iox.Backoff) {
				b.SetBase(1 * time.Millisecond)
			},
			wantDur: 1 * time.Millisecond,
			wantBlk: 1,
		},
		{
			name: "zero base uses default",
			setup: func(b *iox.Backoff) {
				b.SetBase(0)
			},
			wantDur: iox.DefaultBackoffBase,
			wantBlk: 1,
		},
		{
			name: "negative base uses default",
			setup: func(b *iox.Backoff) {
				b.SetBase(-1 * time.Second)
			},
			wantDur: iox.DefaultBackoffBase,
			wantBlk: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b iox.Backoff
			tt.setup(&b)

			if got := b.Duration(); got != tt.wantDur {
				t.Errorf("Duration() = %v, want %v", got, tt.wantDur)
			}
			if got := b.Block(); got != tt.wantBlk {
				t.Errorf("Block() = %d, want %d", got, tt.wantBlk)
			}
		})
	}
}

func TestBackoff_DurationMaxCap(t *testing.T) {
	var b iox.Backoff
	b.SetBase(50 * time.Millisecond)
	// Don't set max, should use DefaultBackoffMax (100ms)

	// Block 1: 50ms
	if got := b.Duration(); got != 50*time.Millisecond {
		t.Errorf("Block 1 Duration() = %v, want 50ms", got)
	}

	b.Wait() // End block 1

	// Block 2: 100ms (would be 100ms, equals max)
	if got := b.Duration(); got != 100*time.Millisecond {
		t.Errorf("Block 2 Duration() = %v, want 100ms", got)
	}

	b.Wait()
	b.Wait() // End block 2

	// Block 3: would be 150ms, capped at 100ms
	if got := b.Duration(); got != iox.DefaultBackoffMax {
		t.Errorf("Block 3 Duration() = %v, want %v (capped)", got, iox.DefaultBackoffMax)
	}
}

func TestBackoff_LinearCurve(t *testing.T) {
	var b iox.Backoff
	base := 100 * time.Microsecond
	b.SetBase(base)

	// Block 1: 1 iteration at 100µs
	if b.Duration() != base {
		t.Errorf("Block 1 duration mismatch")
	}
	b.Wait()

	// Block 2: 2 iterations at 200µs
	if b.Block() != 2 || b.Duration() != 2*base {
		t.Errorf("Block 2 transition failed: got block %d, duration %v", b.Block(), b.Duration())
	}
	b.Wait()
	b.Wait()

	// Block 3: 3 iterations at 300µs
	if b.Block() != 3 || b.Duration() != 3*base {
		t.Errorf("Block 3 transition failed")
	}
}

func TestBackoff_MaxCap(t *testing.T) {
	var b iox.Backoff
	b.SetBase(10 * time.Millisecond)
	b.SetMax(15 * time.Millisecond)

	b.Wait() // Ends Block 1
	// Block 2 duration would be 20ms, should cap at 15ms
	if b.Duration() != 15*time.Millisecond {
		t.Errorf("Expected cap at 15ms, got %v", b.Duration())
	}
}

func TestBackoff_Reset(t *testing.T) {
	var b iox.Backoff
	b.Wait()
	b.Wait()
	if b.Block() == 1 {
		t.Errorf("Should have advanced")
	}
	b.Reset()
	if b.Block() != 1 || b.Duration() != iox.DefaultBackoffBase {
		t.Errorf("Reset failed")
	}
}
