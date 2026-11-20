// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

import (
	"errors"
)

// Outcome classifies the error returned by an I/O operation. Treat the
// constants as unordered labels in a switch. The byte count reports progress.
type Outcome uint8

const (
	// OutcomeFailure denotes an error other than a semantic signal.
	OutcomeFailure Outcome = iota
	// OutcomeOK denotes a nil error.
	OutcomeOK
	// OutcomeWouldBlock means further progress requires readiness.
	OutcomeWouldBlock
	// OutcomeMore means the operation remains active.
	OutcomeMore
)

func (o Outcome) String() string {
	switch o {
	case OutcomeOK:
		return "OK"
	case OutcomeWouldBlock:
		return "WouldBlock"
	case OutcomeMore:
		return "More"
	default:
		return "Failure"
	}
}

// IsWouldBlock reports whether err carries the iox would-block semantic.
// It returns true for ErrWouldBlock and wrappers (via errors.Is).
func IsWouldBlock(err error) bool { return errors.Is(err, ErrWouldBlock) }

// IsMore reports whether err carries the iox multi-shot (more completions)
// semantic. It returns true for ErrMore and wrappers (via errors.Is).
func IsMore(err error) bool { return errors.Is(err, ErrMore) }

// IsSemantic reports whether err represents an iox semantic signal: either
// ErrWouldBlock or ErrMore (including wrapped forms).
func IsSemantic(err error) bool { return IsWouldBlock(err) || IsMore(err) }

// IsNonFailure reports whether err should be treated as a non-failure in
// non-blocking I/O control flow: nil, ErrWouldBlock, or ErrMore.
//
// Typical usage: decide whether to keep a descriptor active without logging an
// error or tearing down the operation.
func IsNonFailure(err error) bool { return err == nil || IsSemantic(err) }

// IsFailure reports whether err is an actual failure rather than nil or an iox
// semantic control signal.
func IsFailure(err error) bool { return err != nil && !IsSemantic(err) }

// IsProgress reports whether [Classify] selects [OutcomeOK] or [OutcomeMore].
// Use the returned byte count to determine how much data moved.
func IsProgress(err error) bool { return err == nil || Classify(err) == OutcomeMore }

// Classify returns [OutcomeOK] for nil, [OutcomeWouldBlock] for errors
// matching [ErrWouldBlock], [OutcomeMore] for errors matching [ErrMore], and
// [OutcomeFailure] otherwise. [ErrWouldBlock] takes precedence if both match.
// Classify treats [EOF] as [OutcomeFailure]. Copy helpers handle EOF separately.
func Classify(err error) Outcome {
	if err == nil {
		return OutcomeOK
	}
	if IsWouldBlock(err) {
		return OutcomeWouldBlock
	}
	if IsMore(err) {
		return OutcomeMore
	}
	return OutcomeFailure
}
