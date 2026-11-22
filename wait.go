// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox

import (
	"errors"
	"sync/atomic"
)

// ErrWaitUnavailable means the handle is stale, withdrawn or already armed.
var ErrWaitUnavailable = errors.New("iox: wait unavailable")

// Trigger registers the event that supplies the evidence for one wait.
// Arm must cover events arriving during registration, for example by
// registering and rechecking readiness. It may signal before returning.
// Return an error only when the source accepted neither registration nor evidence.
// The source owns cancellation, routing and disposal of unaccepted evidence.
type Trigger[X any] interface {
	Arm(Ticket, Signal[X]) error
}

// Signal delivers evidence for one registration. Copies can be used
// concurrently. A zero or stale signal rejects delivery.
type Signal[X any] struct {
	_       [0]func() // The registration may carry a function-valued sink.
	owner   interface{ deliver(uint64, X, any) bool }
	id      uint64
	enqueue any
}

// Deliver offers value to the waiting operation. The first matching delivery
// transfers ownership, calls enqueue, and returns true. On false the source
// still owns value.
// A readiness or timer source can supply struct{} as permission for one probe.
func (s Signal[X]) Deliver(value X) bool {
	return s.owner != nil && s.owner.deliver(s.id, value, s.enqueue)
}

type waitOwner[A any] interface {
	arm(Wait[A], func(Ready[A])) error
	withdraw(uint64) bool
	tryResume(uint64) (Step[A], bool)
}

// Wait is a handle to one pending attempt. Its ticket and identity survive
// transfer to another worker.
type Wait[A any] struct {
	_      [0]func() // A trigger may have a non-comparable dynamic type.
	ticket Ticket
	id     uint64
	owner  waitOwner[A]
	source any
}

// IsZero reports whether this handle describes no wait. A copied handle keeps
// its identity after its attempt has been consumed or withdrawn.
func (w Wait[A]) IsZero() bool { return w.owner == nil }

// Ticket returns the demand and attempt supplied to [Await].
func (w Wait[A]) Ticket() Ticket { return w.ticket }

// Arm registers this wait. enqueue must be non-nil and accept the ready
// value on the signaling goroutine. It can run before Arm returns.
// Once registration starts, Arm returns the trigger's result, even if
// another goroutine withdraws the wait. A refused wait can be armed again
// while it is still pending. A call that cannot claim the pending wait
// returns [ErrWaitUnavailable].
func (w Wait[A]) Arm(enqueue func(Ready[A])) error {
	if w.owner == nil {
		return ErrWaitUnavailable
	}
	return w.owner.arm(w, enqueue)
}

// Withdraw releases the wait if no signal has been accepted. On false the
// handle is stale or the queued work already owns the evidence. The binding
// remains responsible for its external registration and resources, even if
// registration finishes after withdrawal.
func (w Wait[A]) Withdraw() bool { return w.owner != nil && w.owner.withdraw(w.id) }

// Ready carries one accepted signal to a worker. Copies share a single claim.
type Ready[A any] struct {
	owner waitOwner[A]
	id    uint64
}

// TryResume consumes the evidence and runs the continuation on the calling
// goroutine. A zero, stale or already consumed ready value returns false.
func (r Ready[A]) TryResume() (Step[A], bool) {
	if r.owner == nil {
		return Step[A]{}, false
	}
	return r.owner.tryResume(r.id)
}

// Await returns a step waiting for typed evidence. [Wait.Arm] registers
// trigger, and the worker that claims [Ready.TryResume] runs resume.
// Both trigger and resume must be non-nil. Use [WaitSlot.Await] to reuse
// storage across successive attempts.
func Await[X, A any](ticket Ticket, trigger Trigger[X], resume func(X) Step[A]) Step[A] {
	return new(WaitSlot[X, A]).Await(ticket, trigger, resume)
}

// Each new wait and each registration advances the generation. The low bits
// name the phase. busy gives one caller exclusive access to resume and value.
const (
	waitFree uint64 = iota
	waitPending
	waitArmed
	waitQueued
	waitBusy
	phaseMask = 7
)

func nextGeneration(state uint64) uint64 { return (state | phaseMask) + 1 }

// WaitSlot holds one outstanding wait. Its zero value is ready for use.
// It must not be copied after first use. A slot can be reused from inside
// its continuation or after a successful withdrawal. Each wait and arm uses
// one of 2⁶¹ generations. Retire the slot before reusing a live generation.
type WaitSlot[X, A any] struct {
	state  atomic.Uint64
	waitID atomic.Uint64
	resume func(X) Step[A]
	value  X
}

// Await installs a new attempt in the slot. It panics if the previous
// attempt still owns the slot. trigger and resume must be non-nil.
func (s *WaitSlot[X, A]) Await(ticket Ticket, trigger Trigger[X], resume func(X) Step[A]) Step[A] {
	old := s.state.Load()
	id := nextGeneration(old)
	if old&phaseMask != waitFree || !s.state.CompareAndSwap(old, id|waitBusy) {
		panic("iox: wait slot in use")
	}
	s.resume = resume
	s.waitID.Store(id)
	s.state.Store(id | waitPending)
	return Step[A]{wait: Wait[A]{ticket: ticket, id: id, owner: s, source: trigger}}
}

func (s *WaitSlot[X, A]) arm(w Wait[A], enqueue func(Ready[A])) error {
	old := s.state.Load()
	armed := nextGeneration(old) | waitArmed
	if old&phaseMask != waitPending || s.waitID.Load() != w.id || !s.state.CompareAndSwap(old, armed) {
		return ErrWaitUnavailable
	}
	// Await pairs this trigger with this slot's X. Both handles carry their
	// callback so a withdrawn registration can finish independently of reuse.
	signal := Signal[X]{owner: s, id: armed, enqueue: enqueue}
	err := w.source.(Trigger[X]).Arm(w.ticket, signal)
	if err != nil {
		s.state.CompareAndSwap(armed, armed&^phaseMask|waitPending)
	}
	return err
}

func (s *WaitSlot[X, A]) deliver(armed uint64, value X, enqueue any) bool {
	generation := armed &^ phaseMask
	if !s.state.CompareAndSwap(armed, generation|waitBusy) {
		return false
	}
	s.value = value
	queued := generation | waitQueued
	s.state.Store(queued)
	// arm supplies a function of this exact type for the slot's A.
	enqueue.(func(Ready[A]))(Ready[A]{owner: s, id: queued})
	return true
}

func (s *WaitSlot[X, A]) withdraw(id uint64) bool {
	for {
		old := s.state.Load()
		phase := old & phaseMask
		if (phase != waitPending && phase != waitArmed) || s.waitID.Load() != id {
			return false
		}
		generation := old &^ phaseMask
		if s.state.CompareAndSwap(old, generation|waitBusy) {
			s.release(generation)
			return true
		}
	}
}

func (s *WaitSlot[X, A]) tryResume(queued uint64) (Step[A], bool) {
	generation := queued &^ phaseMask
	if !s.state.CompareAndSwap(queued, generation|waitBusy) {
		return Step[A]{}, false
	}
	resume, value := s.resume, s.value
	// Only a queued wait contains a reply. Drop it before reusing the slot.
	var zero X
	s.value = zero
	s.release(generation)
	return resume(value), true
}

func (s *WaitSlot[X, A]) release(generation uint64) {
	s.resume = nil
	s.state.Store(generation | waitFree)
}
