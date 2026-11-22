// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

import "runtime"

// Op identifies the call that reported a semantic signal. Direct reads and
// writes have distinct values. [OpCopyWriterTo] and [OpCopyReaderFrom]
// identify a delegated method whose signal can come from either side.
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
	// PolicyReturn tells the helper to return control to the caller.
	PolicyReturn PolicyAction = iota

	// PolicyRetry tells the helper to call Yield before continuing.
	PolicyRetry
)

// SemanticPolicy selects an action for an exact [ErrWouldBlock] or [ErrMore].
// [PolicyRetry] invokes Yield before the helper resumes. Yield can wait for
// readiness. Repeated retries without a readiness wait can spin.
//
// A bounded copy skips the policy once its byte count is complete.
// [TeeReaderPolicy] can yield and return data already read to its caller.
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

// YieldOnWriteWouldBlockPolicy retries [ErrWouldBlock] from writes and from
// delegated [WriterTo] and [ReaderFrom] calls. Those methods report one
// aggregate error, which can originate on either side of the transfer.
// Direct reads and [ErrMore] return control to the caller.
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
