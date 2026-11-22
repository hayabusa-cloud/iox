// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

// Observation is the count and original error from one I/O call.
type Observation struct {
	N   int64
	Err error
}

// Ticket identifies one wait attempt for a demand. A worker handoff carries
// the same ticket. Give each new attempt a fresh identity at its source.
type Ticket struct {
	Demand  uint64
	Attempt uint64
}

// Step describes the result of advancing an operation to its next boundary.
// The zero Step finishes with the zero value of A and a nil error.
type Step[A any] struct {
	value    A
	err      error
	wait     Wait[A]
	obs      Observation
	observed bool
}

// Finish returns a terminal step. The value may include progress on failure.
func Finish[A any](value A, err error) Step[A] {
	return Step[A]{value: value, err: err}
}

// WithObservation returns a copy recording an actual I/O call. It panics if
// the step already records a call. Construct a separate step for each call.
func (s Step[A]) WithObservation(o Observation) Step[A] {
	if s.observed {
		panic("iox: step already has an observation")
	}
	s.obs, s.observed = o, true
	return s
}

// Observation reports the recorded call, including an actual (0, nil).
// A step that only registers or schedules work can have no observation.
func (s Step[A]) Observation() (Observation, bool) { return s.obs, s.observed }

// Next returns the terminal value and error when wait.IsZero().
// Otherwise wait identifies the outstanding work. Copies refer to the same
// single-use attempt even if the operation later reuses its storage.
func (s Step[A]) Next() (value A, err error, wait Wait[A]) {
	return s.value, s.err, s.wait
}
