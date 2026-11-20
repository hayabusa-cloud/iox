// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"math"
	"math/big"
	"testing"
	"testing/synctest"
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
	synctest.Test(t, func(t *testing.T) {
		var b iox.Backoff
		checkBackoffWait(t, &b, iox.DefaultBackoffBase)
		if got := b.Block(); got != 2 {
			t.Errorf("after first Wait, Block() = %d, want 2", got)
		}
	})
}

// The caller runs in a synctest bubble so elapsed time measures the requested sleep.
func checkBackoffWait(t *testing.T, b *iox.Backoff, nominal time.Duration) {
	t.Helper()
	start := time.Now()
	b.Wait()
	elapsed := time.Since(start)
	minWait, maxWait := nominal-nominal/8, nominal+nominal/8
	if elapsed < minWait || elapsed > maxWait {
		t.Errorf("Wait() slept %v, want [%v, %v]", elapsed, minWait, maxWait)
	}
}

func TestBackoffNextDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var b iox.Backoff
		for i, block := range []int{1, 2, 2, 3, 3, 3, 4} {
			nominal := time.Duration(block) * iox.DefaultBackoffBase
			if got := b.Duration(); got != nominal {
				t.Fatalf("attempt %d: nominal %v, want %v", i+1, got, nominal)
			}
			if i%2 == 1 {
				checkBackoffWait(t, &b, nominal)
				continue
			}
			start := time.Now()
			delay := b.NextDelay()
			if elapsed := time.Since(start); elapsed != 0 {
				t.Fatalf("NextDelay advanced the clock by %v", elapsed)
			}
			if delay < nominal-nominal/8 || delay > nominal+nominal/8 {
				t.Fatalf("block %d: delay %v outside jitter bounds for %v", block, delay, nominal)
			}
		}
	})
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

	b.NextDelay() // End block 1.

	// Block 2: 100ms (would be 100ms, equals max)
	if got := b.Duration(); got != 100*time.Millisecond {
		t.Errorf("Block 2 Duration() = %v, want 100ms", got)
	}

	b.NextDelay()
	b.NextDelay() // End block 2.

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
	b.NextDelay()

	// Block 2: 2 iterations at 200µs
	if b.Block() != 2 || b.Duration() != 2*base {
		t.Errorf("Block 2 transition failed: got block %d, duration %v", b.Block(), b.Duration())
	}
	b.NextDelay()
	b.NextDelay()

	// Block 3: 3 iterations at 300µs
	if b.Block() != 3 || b.Duration() != 3*base {
		t.Errorf("Block 3 transition failed")
	}
}

func TestBackoff_MaxCap(t *testing.T) {
	var b iox.Backoff
	b.SetBase(10 * time.Millisecond)
	b.SetMax(15 * time.Millisecond)

	b.NextDelay() // End block 1.
	// Block 2 duration would be 20ms, should cap at 15ms
	if b.Duration() != 15*time.Millisecond {
		t.Errorf("Expected cap at 15ms, got %v", b.Duration())
	}
}

func TestBackoff_Reset(t *testing.T) {
	var b iox.Backoff
	b.NextDelay()
	b.NextDelay()
	if b.Block() == 1 {
		t.Errorf("Should have advanced")
	}
	b.Reset()
	if b.Block() != 1 || b.Duration() != iox.DefaultBackoffBase {
		t.Errorf("Reset failed")
	}
}

func TestBackoff_WaitMaxCap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var b iox.Backoff
		b.SetBase(10 * time.Millisecond)
		b.SetMax(15 * time.Millisecond)

		checkBackoffWait(t, &b, 10*time.Millisecond)
		checkBackoffWait(t, &b, 15*time.Millisecond)
		if got := b.Block(); got != 2 {
			t.Errorf("after capped Wait, Block() = %d, want 2", got)
		}
	})
}

func TestBackoffCappedProduct(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var b iox.Backoff
		const capDelay = 8 * time.Millisecond
		b.SetBase(1 << 62)
		b.SetMax(capDelay)
		// Blocks two and four cross the signed and unsigned 64-bit limits.
		for range 7 {
			if d := b.Duration(); d != capDelay {
				t.Fatalf("block %d: nominal %v, want %v", b.Block(), d, capDelay)
			}
			checkBackoffWait(t, &b, capDelay)
		}
	})
}

func TestBackoffJitterAcrossDurationRange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// At 1024 ns the sampled adjustment equals the jitter numerator.
		var unit iox.Backoff
		unit.SetBase(1024)
		unit.SetMax(1024)
		for _, d := range []time.Duration{1, 1023, 1025, math.MaxInt64 / 128, 1 << 56, 1<<62 + 31, math.MaxInt64} {
			nominal := big.NewInt(int64(d))
			for range 256 {
				// Sample block one to isolate jitter from the block multiplication.
				unit.Reset()
				b := unit
				b.SetBase(d)
				b.SetMax(d)
				rate := int64(unit.NextDelay() - 1024)
				var exact big.Int
				exact.Mul(nominal, big.NewInt(rate))
				exact.Quo(&exact, big.NewInt(1024))
				exact.Add(&exact, nominal)
				want := time.Duration(math.MaxInt64)
				if exact.IsInt64() {
					want = time.Duration(exact.Int64())
				}
				if got := b.NextDelay(); got != want {
					t.Fatalf("nominal %v, jitter %d/1024: got %v, want %v", d, rate, got, want)
				}
			}
		}
	})
}
