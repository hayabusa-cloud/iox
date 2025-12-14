// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

import "runtime"

// Op identifies where a semantic signal (ErrWouldBlock / ErrMore) came from.
//
// This is intentionally coarse-grained: it lets a Policy distinguish reader-side
// vs writer-side semantics (e.g., writer-side ErrMore as a frame boundary).
type Op uint8

const (
	OpCopyRead Op = iota
	OpCopyWrite

	OpCopyWriterTo
	OpCopyReaderFrom

	OpTeeReaderRead
	OpTeeReaderSideWrite

	OpTeeWriterPrimaryWrite
	OpTeeWriterTeeWrite
)

func (op Op) String() string {
	switch op {
	case OpCopyRead:
		return "CopyRead"
	case OpCopyWrite:
		return "CopyWrite"
	case OpCopyWriterTo:
		return "CopyWriterTo"
	case OpCopyReaderFrom:
		return "CopyReaderFrom"
	case OpTeeReaderRead:
		return "TeeReaderRead"
	case OpTeeReaderSideWrite:
		return "TeeReaderSideWrite"
	case OpTeeWriterPrimaryWrite:
		return "TeeWriterPrimaryWrite"
	case OpTeeWriterTeeWrite:
		return "TeeWriterTeeWrite"
	default:
		return "Op(unknown)"
	}
}

// PolicyAction tells an engine whether it should return to the caller
// or attempt the operation again.
type PolicyAction uint8

const (
	// PolicyReturn means: return immediately to the caller.
	// Use this for "delivery boundaries" (e.g., writer-side ErrMore).
	PolicyReturn PolicyAction = iota

	// PolicyRetry means: do not return; retry after waiting/yielding.
	// This is typically used to map ErrWouldBlock to blocking-ish behavior.
	PolicyRetry
)

// SemanticPolicy customizes how an engine reacts to iox semantic errors.
//
// This is a decision function that maps (operation, error) pairs to actions,
// plus an optional yield hook for when retry is selected.
//
// Contract expectations:
//   - OnWouldBlock / OnMore are only called for the matching semantic errors.
//   - If PolicyRetry is returned, the engine will call Yield(op) and then retry.
//   - If Yield(op) does not actually wait for readiness/completion, the engine
//     may spin.
//
// Note: keep this interface narrow; it should remain usable for both Copy and Tee.
type SemanticPolicy interface {
	Yield(op Op)
	OnWouldBlock(op Op) PolicyAction
	OnMore(op Op) PolicyAction
}

// PolicyFunc is a convenience implementation for callers that want to inject
// behavior without defining a struct type.
//
// Default behaviors when fields are nil:
//   - YieldFunc: calls runtime.Gosched() to yield the processor
//   - WouldBlockFunc: returns PolicyReturn (caller handles ErrWouldBlock)
//   - MoreFunc: returns PolicyReturn (caller handles ErrMore)
type PolicyFunc struct {
	YieldFunc      func(op Op)
	WouldBlockFunc func(op Op) PolicyAction
	MoreFunc       func(op Op) PolicyAction
}

func (p PolicyFunc) Yield(op Op) {
	if p.YieldFunc != nil {
		p.YieldFunc(op)
		return
	}
	runtime.Gosched()
}

func (p PolicyFunc) OnWouldBlock(op Op) PolicyAction {
	if p.WouldBlockFunc != nil {
		return p.WouldBlockFunc(op)
	}
	return PolicyReturn
}

func (p PolicyFunc) OnMore(op Op) PolicyAction {
	if p.MoreFunc != nil {
		return p.MoreFunc(op)
	}
	return PolicyReturn
}

// ReturnPolicy is the simplest policy: never waits and never retries.
// It preserves non-blocking semantics (callers handle ErrWouldBlock/ErrMore).
type ReturnPolicy struct{}

func (ReturnPolicy) Yield(Op) {}

func (ReturnPolicy) OnWouldBlock(Op) PolicyAction { return PolicyReturn }

func (ReturnPolicy) OnMore(Op) PolicyAction { return PolicyReturn }

// YieldPolicy is a ready-to-use policy with the common mapping:
//
//   - ErrWouldBlock: yield/wait and retry
//   - ErrMore: return immediately (treat as a delivery/boundary signal)
//
// This matches protocols where writer-side ErrMore denotes a completed frame and
// the caller wants to handle the boundary immediately, then call Copy again.
//
// Default Yield behavior: runtime.Gosched().
type YieldPolicy struct {
	// YieldFunc is invoked when the engine decides to retry after ErrWouldBlock.
	// It may spin, park, poll, run an event-loop tick, etc.
	YieldFunc func(op Op)
}

func (p YieldPolicy) Yield(op Op) {
	if p.YieldFunc != nil {
		p.YieldFunc(op)
		return
	}
	runtime.Gosched()
}

func (YieldPolicy) OnWouldBlock(Op) PolicyAction { return PolicyRetry }

func (YieldPolicy) OnMore(Op) PolicyAction { return PolicyReturn }

// YieldOnWriteWouldBlockPolicy retries only when the *writer side* would block.
// Reader-side ErrWouldBlock is returned to the caller.
//
// Useful when reads are fed by an event loop already, but writes need a local
// backpressure strategy (e.g., bounded output buffer).
type YieldOnWriteWouldBlockPolicy struct {
	YieldFunc func(op Op)
}

func (p YieldOnWriteWouldBlockPolicy) Yield(op Op) {
	if p.YieldFunc != nil {
		p.YieldFunc(op)
		return
	}
	runtime.Gosched()
}

func (YieldOnWriteWouldBlockPolicy) OnWouldBlock(op Op) PolicyAction {
	switch op {
	case OpCopyWrite, OpCopyWriterTo, OpCopyReaderFrom, OpTeeReaderSideWrite, OpTeeWriterPrimaryWrite, OpTeeWriterTeeWrite:
		return PolicyRetry
	default:
		return PolicyReturn
	}
}

func (YieldOnWriteWouldBlockPolicy) OnMore(Op) PolicyAction { return PolicyReturn }
