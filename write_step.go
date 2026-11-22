// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

import (
	"errors"
	"math"
)

// ErrWriteAttemptExhausted means a write needs another wait after using the
// largest attempt number for its demand.
var ErrWriteAttemptExhausted = errors.New("iox: write attempt number exhausted")

var errInvalidWriteCount = errors.New("iox: invalid write count")

// WriteStep calls dst.Write once, including for an empty p. Use a nonblocking
// writer when driving the operation from an event loop. If dst accepts fewer
// than len(p) bytes and returns [ErrWouldBlock] (possibly wrapped), a signal
// from writable permits one write of the remaining suffix to dst. Supply a
// trigger when dst can block. Keep p valid and unmodified until the operation
// finishes or [Wait.Withdraw] succeeds.
//
// Each observation records the count and original error from one call.
// The terminal result reports the total accepted bytes. A full count ends
// the operation with the writer's error, even if it is [ErrWouldBlock].
// On an incomplete write, other errors, including [ErrMore], are returned as
// received. A short nil-error write returns [ErrShortWrite].
//
// The first wait uses ticket. Each subsequent wait increments ticket.Attempt.
// A probe at the largest attempt number can still complete the write. If it
// needs another wait, the operation finishes with [ErrWriteAttemptExhausted].
func WriteStep(dst Writer, p []byte, ticket Ticket, writable Trigger[struct{}]) Step[int64] {
	n, err := dst.Write(p)
	if n == len(p) {
		return observedWrite(int64(n), err, Observation{N: int64(n), Err: err})
	}
	c := writeCursor{dst: dst, remaining: p, ticket: ticket, writable: writable}
	return c.incomplete(nil, n, err)
}

type writeCursor struct {
	dst       Writer
	remaining []byte
	written   int64
	ticket    Ticket
	writable  Trigger[struct{}]
}

// Keep the cursor separate from the slot so a stale handle to the slot
// can outlive the write without retaining its buffer through the slot.
type writeRetry struct {
	cursor writeCursor
	slot   *WaitSlot[struct{}, int64]
	resume func(struct{}) Step[int64]
}

func (r *writeRetry) probe(struct{}) Step[int64] {
	c := r.cursor
	n, err := c.dst.Write(c.remaining)
	if n == len(c.remaining) {
		return observedWrite(c.written+int64(n), err, Observation{N: int64(n), Err: err})
	}
	return c.incomplete(r, n, err)
}

func observedWrite(total int64, err error, raw Observation) Step[int64] {
	return Step[int64]{value: total, err: err, obs: raw, observed: true}
}

func (c writeCursor) incomplete(r *writeRetry, n int, err error) Step[int64] {
	o := Observation{N: int64(n), Err: err}
	if n < 0 || n > len(c.remaining) {
		if err == nil {
			err = errInvalidWriteCount
		}
		return observedWrite(c.written, err, o)
	}
	c.written += int64(n)
	if IsWouldBlock(err) {
		c.remaining = c.remaining[n:]
		if r == nil {
			r = &writeRetry{slot: new(WaitSlot[struct{}, int64])}
			r.resume = r.probe
		} else {
			if c.ticket.Attempt == math.MaxUint64 {
				return observedWrite(c.written, ErrWriteAttemptExhausted, o)
			}
			c.ticket.Attempt++
		}
		r.cursor = c
		return r.slot.Await(c.ticket, c.writable, r.resume).WithObservation(o)
	}
	if err == nil {
		err = ErrShortWrite
	}
	return observedWrite(c.written, err, o)
}
