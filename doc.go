// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// Package iox extends Go's io interfaces with signals for non-blocking and
// multi-shot I/O. [Reader] and [Writer] are aliases of the standard interfaces.
//
// Process the returned byte count before handling the error. [ErrWouldBlock]
// means further progress requires readiness. [ErrMore] means the operation
// remains active and another observation is expected. Either signal can
// accompany zero or more bytes.
//
// [Classify] and the error predicates recognize wrapped signals. If an error
// matches both sentinels, Classify selects [OutcomeWouldBlock]. [IsProgress]
// reports the OK and More outcomes. [IsWouldBlock] and [IsMore] test each
// sentinel independently. Copy and tee helpers consult their policies only
// for exact sentinels.
//
// A [Step] holds a terminal result from [Finish] or a pending wait from [Await].
// A driver registers the wait with [Wait.Arm]. Once a signal is accepted,
// a worker can claim the ready work with [Ready.TryResume] to run its continuation.
//
// [WriteStep] retains the unwritten suffix after a partial [ErrWouldBlock].
// A wake permits one more write to the same destination. A full count ends
// the operation with the writer's error and the total accepted byte count.
//
// [Copy] uses [WriterTo] or [ReaderFrom] when available. A delegated method
// owns source advancement and recovery from partial writes. In the generic
// read/write loop, a (0, nil) read ends the copy and returns (written, nil).
// [CopyN] and its variants return success once the destination accepts the
// requested number of bytes, even if the underlying operation remains active.
package iox
