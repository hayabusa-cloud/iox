// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// Package iox provides non‑blocking and multi-shot I/O helpers that extend
// Go's standard io semantics while remaining fully compatible with its
// interfaces and fast paths (WriterTo/ReaderFrom).
//
// # Semantic errors are control outcomes
//
// iox treats the returned error as the control component of an observed
// operation result. A call result is progress plus control, not a naked failure
// value:
//   - nil: terminal success at this abstraction boundary.
//   - ErrMore: non-failure progress/control; a successor observation is expected.
//   - ErrWouldBlock: non-failure no-progress control; wait, yield, or register
//     interest before retrying.
//   - any other error: failure.
//
// Always process n or written before interpreting err. Do not treat ErrMore or
// ErrWouldBlock as failures.
//
// Extended result semantics
//   - ErrWouldBlock: the operation cannot make progress now without waiting.
//     Return immediately; retry later.
//   - ErrMore: the operation remains active and more observations are expected.
//     The count may be zero. Process any returned data and keep polling.
//
// Classification helpers such as IsMore, IsWouldBlock, IsSemantic,
// IsNonFailure, IsFailure, and Classify accept wrapped semantic sentinels
// through errors.Is. Callers should prefer these helpers over direct errors.Is
// checks when classifying iox semantic-control outcomes. Internal hot-path
// engines dispatch policy decisions by exact sentinel identity;
// package-controlled producers on those paths must return ErrMore and
// ErrWouldBlock unwrapped.
// IsProgress follows Classify: an error matching both semantic sentinels
// classifies as OutcomeWouldBlock and is not progress control. IsMore and
// IsWouldBlock report sentinel membership, so both may be true for that error.
//
// A [Step] holds a terminal result from [Finish] or a pending wait from [Await].
// A driver registers the wait with [Wait.Arm]. Once a signal is accepted,
// a worker can claim the ready work with [Ready.TryResume] to run its continuation.
//
// These semantics propagate through Copy/CopyN and Tee helpers. CopyN,
// CopyNBuffer, and their policy variants are bounded "copy exactly n bytes"
// operations: once written == n, they return nil at that abstraction boundary.
// They are not subscription or multi-shot route lifecycle APIs.
//
// When a WriterTo/ReaderFrom fast path is selected, that fast-path
// implementation owns its own source advancement and partial-write recovery; it
// must preserve ErrWouldBlock/ErrMore instead of converting them to nil or EOF.
// Use the iox.Copy family instead of io.Copy when you need to preserve these
// semantics.
//
// Note: Copy treats a (0, nil) read as "stop copying now" and returns (written, nil)
// to avoid hidden spinning inside a helper in event-loop code.
package iox
