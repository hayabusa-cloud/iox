// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

import (
	"errors"
)

// Outcome classifies an operation result based on iox's extended semantics.
//
// OutcomeOK:            success, no more to come.
// OutcomeWouldBlock:    no progress is possible right now; retry later.
// OutcomeMore:          progress happened and more completions are expected.
// OutcomeFailure:       any other error (including EOF when it's not absorbed by helpers).
type Outcome uint8

const (
	OutcomeFailure Outcome = iota
	OutcomeOK
	OutcomeWouldBlock
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

// IsProgress reports whether the current call produced usable progress now:
// returns true for nil and ErrMore. In both cases caller can proceed with
// delivered data/work; for ErrMore keep polling for subsequent completions.
func IsProgress(err error) bool { return err == nil || IsMore(err) }

// Classify maps err to an Outcome. Use when a compact switch is preferred.
//
// Note: This does not attempt to reinterpret standard library sentinels like
// io.EOF; classification depends solely on the error value the caller passes.
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
