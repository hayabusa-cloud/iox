// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

import (
	"math"
	"math/bits"
	"time"
)

const (
	// DefaultBackoffBase is the initial sleep duration before jitter.
	DefaultBackoffBase = 500 * time.Microsecond

	// DefaultBackoffMax is the default duration cap before jitter (100ms).
	DefaultBackoffMax = 100 * time.Millisecond
)

// Backoff spaces retry attempts when progress depends on external I/O.
// Its zero value uses [DefaultBackoffBase] and [DefaultBackoffMax].
// Use one Backoff per retry loop, or synchronize access to a shared Backoff.
//
// Block n supplies n intervals of min(n × base, max) with up to 12.5% jitter.
// [Backoff.Wait] and [Backoff.NextDelay] each consume one interval.
// Call [Backoff.Reset] after progress to start again at block 1.
type Backoff struct {
	n       int           // block counter (1-indexed)
	i       int           // iteration within current block
	base    time.Duration // base duration
	max     time.Duration // maximum duration
	fastSrc uint64        // PRNG state for jitter
}

// Wait sleeps the calling goroutine for one interval from [Backoff.NextDelay].
func (b *Backoff) Wait() {
	time.Sleep(b.NextDelay())
}

// NextDelay returns the next jittered interval and advances the schedule
// without sleeping. A caller can use the interval to arm a retry timer.
// The cap applies before jitter. The result saturates at the largest [time.Duration].
func (b *Backoff) NextDelay() time.Duration {
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

	delay := b.applyJitter(cappedDuration(b.n, b.base, b.max))

	b.i++
	if b.i >= b.n {
		b.i = 0
		b.n++
	}
	return delay
}

func cappedDuration(block int, base, limit time.Duration) time.Duration {
	hi, lo := bits.Mul64(uint64(block), uint64(base))
	if hi != 0 {
		// The product exceeds every representable duration cap.
		return limit
	}
	return time.Duration(min(lo, uint64(limit)))
}

func (b *Backoff) applyJitter(d time.Duration) time.Duration {
	b.fastSrc ^= b.fastSrc << 13
	b.fastSrc ^= b.fastSrc >> 7
	b.fastSrc ^= b.fastSrc << 17
	rate := int64(uint8(b.fastSrc>>32)) - 128
	ns := int64(d)
	// Split the nominal delay before multiplication. Only the remainder
	// needs rounding toward zero, including for negative jitter.
	offset := (ns>>10)*rate + (ns&1023)*rate/1024
	return d + time.Duration(min(offset, math.MaxInt64-ns))
}

// SetBase configures the initial duration and linear scaling factor.
// d must be positive. The current block is preserved.
func (b *Backoff) SetBase(d time.Duration) { b.base = d }

// SetMax configures the duration cap before jitter.
// d must be positive. The current block is preserved.
func (b *Backoff) SetMax(d time.Duration) { b.max = d }

// Reset restarts the block schedule and preserves the configured base and cap.
func (b *Backoff) Reset() { b.n = 0; b.i = 0 }

// Block returns the current block number, starting at 1.
func (b *Backoff) Block() int {
	if b.n == 0 {
		return 1
	}
	return b.n
}

// Duration reports the current block's interval before jitter without advancing
// the schedule. For a zero-value Backoff it returns [DefaultBackoffBase].
func (b *Backoff) Duration() time.Duration {
	n := b.n
	if n == 0 {
		n = 1
	}
	base := b.base
	if base <= 0 {
		base = DefaultBackoffBase
	}
	max := b.max
	if max <= 0 {
		max = DefaultBackoffMax
	}
	return cappedDuration(n, base, max)
}
