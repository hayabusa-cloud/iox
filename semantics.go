// ©Hayabusa Cloud Co., Ltd. 2025. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

import (
	"errors"
)

// Outcome classifies an operation result based on iox's extended semantics.
// Its numeric encoding is defensive, not an ordering; use switches, Classify,
// and predicate helpers instead of numeric comparison.
//
// OutcomeOK:            successful control result at this boundary.
// OutcomeWouldBlock:    no further progress without readiness; retry later.
// OutcomeMore:          operation remains active; successor observations expected.
// OutcomeFailure:       any other error (including EOF when it's not absorbed by helpers).
//
// Returned counts report quantitative progress separately from Outcome.
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

// IsFailure reports whether err is an actual failure rather than nil or an iox
// semantic control signal.
func IsFailure(err error) bool { return err != nil && !IsSemantic(err) }

// IsProgress reports whether Classify selects OutcomeOK or OutcomeMore.
// ErrWouldBlock takes precedence when err matches both semantic sentinels.
// This classifies control only; callers must account for returned counts
// separately, including partial progress paired with ErrWouldBlock.
func IsProgress(err error) bool { return err == nil || Classify(err) == OutcomeMore }

// Classify maps err to an Outcome. Use when a compact switch is preferred.
//
// Classify accepts wrapped semantic sentinels through errors.Is. Internal
// hot-path engines may still require exact sentinel identity for policy dispatch.
// ErrWouldBlock takes precedence when err matches both semantic sentinels.
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
