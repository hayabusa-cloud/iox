// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

import (
	"time"
)

const (
	// DefaultBackoffBase is the tuned base duration for backoff (500µs).
	// It matches the expected scale of local-network round-trip times.
	DefaultBackoffBase = 500 * time.Microsecond

	// DefaultBackoffMax is the default ceiling for sleep duration (100ms).
	DefaultBackoffMax = 100 * time.Millisecond
)

// Backoff implements a linear block-based back-off strategy with jitter.
// It is designed for external I/O readiness waiting (e.g., buffer release).
//
// Zero-value is ready to use: a freshly declared Backoff{} uses
// DefaultBackoffBase (500µs) and DefaultBackoffMax (100ms).
//
// This is the "Adapt" layer of the Three-Tier Progress model:
//  1. Strike: System call → Direct kernel hit.
//  2. Spin: Hardware yield (spin) → Local atomic synchronization.
//  3. Adapt: Software backoff (iox.Backoff) → External I/O readiness.
//
// The algorithm groups iterations into blocks. In block n, it performs n
// sleeps of duration (base × n). Jitter (±12.5%) is applied to prevent
// synchronized thundering herds.
type Backoff struct {
	n       int           // block counter (1-indexed)
	i       int           // iteration within current block
	base    time.Duration // base duration
	max     time.Duration // maximum duration
	fastSrc uint64        // PRNG state for jitter
}

// Wait performs a non-blocking-friendly sleep.
// The duration scales linearly: min(base * n, max) ± 12.5% jitter.
func (b *Backoff) Wait() {
	if b.n == 0 {
		b.n = 1
		if b.base <= 0 {
			b.base = DefaultBackoffBase
		}
		if b.max <= 0 {
			b.max = DefaultBackoffMax
		}
		if b.fastSrc == 0 {
			b.fastSrc = uint64(time.Now().UnixNano()) | 1
		}
	}

	// Linear duration: base * n
	d := time.Duration(b.n) * b.base
	if d > b.max {
		d = b.max
	}

	time.Sleep(b.applyJitter(d))

	b.i++
	if b.i >= b.n {
		b.i = 0
		b.n++
	}
}

func (b *Backoff) applyJitter(d time.Duration) time.Duration {
	b.fastSrc ^= b.fastSrc << 13
	b.fastSrc ^= b.fastSrc >> 7
	b.fastSrc ^= b.fastSrc << 17
	r := int64(b.fastSrc>>32) % 256
	factor := int64(d) * (r - 128) / 1024
	return d + time.Duration(factor)
}

// SetBase configures the initial duration and linear scaling factor.
func (b *Backoff) SetBase(d time.Duration) { b.base = d }

// SetMax configures the maximum allowed sleep duration.
func (b *Backoff) SetMax(d time.Duration) { b.max = d }

// Reset restores the backoff state to block 1.
func (b *Backoff) Reset() { b.n = 0; b.i = 0 }

// Block returns the current progression tier.
func (b *Backoff) Block() int {
	if b.n == 0 {
		return 1
	}
	return b.n
}

// Duration returns the current duration without jitter.
// For a zero-value Backoff, returns DefaultBackoffBase.
func (b *Backoff) Duration() time.Duration {
	n := b.n
	if n == 0 {
		n = 1
	}
	base := b.base
	if base <= 0 {
		base = DefaultBackoffBase
	}
	d := time.Duration(n) * base
	max := b.max
	if max <= 0 {
		max = DefaultBackoffMax
	}
	if d > max {
		return max
	}
	return d
}
