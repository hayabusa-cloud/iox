// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"errors"
	"sync"
	"testing"

	"code.hybscloud.com/iox"
)

type stepTrigger[X any] func(iox.Ticket, iox.Signal[X]) error

func (f stepTrigger[X]) Arm(t iox.Ticket, s iox.Signal[X]) error { return f(t, s) }

func TestWaitRefusalReuseAndStaleSignals(t *testing.T) {
	var slot iox.WaitSlot[int, int]
	var old, current iox.Signal[int]
	refused := errors.New("queue full")
	var refusedOnce bool
	gate := stepTrigger[int](func(_ iox.Ticket, s iox.Signal[int]) error {
		if !refusedOnce {
			refusedOnce = true
			old = s
			return refused
		}
		current = s
		return nil
	})
	second := iox.Ticket{Demand: 1, Attempt: 1}
	s := slot.Await(iox.Ticket{Demand: 1}, gate, func(x int) iox.Step[int] {
		if x != 5 {
			t.Fatalf("received %d from the wrong registration", x)
		}
		return slot.Await(second, gate, func(n int) iox.Step[int] { return iox.Finish(n, nil) })
	})
	if _, seen := s.Observation(); seen {
		t.Fatal("invented observation")
	}
	_, _, w := s.Next()
	var ready iox.Ready[int]
	queue := func(r iox.Ready[int]) { ready = r }
	if err := w.Arm(queue); err != refused || old.Deliver(1) {
		t.Fatal("registration refusal", err)
	}
	if err := w.Arm(queue); err != nil || old.Deliver(1) || !current.Deliver(5) {
		t.Fatal("registration retry", err)
	}
	next, claimed := ready.TryResume()
	_, _, w2 := next.Next()
	if !claimed || w2.Ticket() != second {
		t.Fatal("continuation lost its successor", claimed, w2.Ticket())
	}
	if err := w.Arm(queue); err != iox.ErrWaitUnavailable || w.Withdraw() || current.Deliver(2) {
		t.Fatal("old attempt acted on reused slot", err)
	}
	if _, ok := ready.TryResume(); ok || !w2.Withdraw() || w2.Withdraw() {
		t.Fatal("stale ready or repeated withdrawal")
	}
}

func TestWithdrawDuringRegistration(t *testing.T) {
	t.Run("accepted", func(t *testing.T) { testWithdrawDuringRegistration(t, nil) })
	t.Run("refused", func(t *testing.T) { testWithdrawDuringRegistration(t, errors.New("refused")) })
}

func testWithdrawDuringRegistration(t *testing.T, registrationErr error) {
	t.Helper()
	var slot iox.WaitSlot[int, int]
	started := make(chan iox.Signal[int], 1)
	leave := make(chan struct{})
	gate := stepTrigger[int](func(_ iox.Ticket, s iox.Signal[int]) error {
		started <- s
		<-leave
		return registrationErr
	})
	done := func(x int) iox.Step[int] { return iox.Finish(x, nil) }
	_, _, w := slot.Await(iox.Ticket{Demand: 1}, gate, done).Next()
	rq := make(chan iox.Ready[int], 1)
	queue := func(r iox.Ready[int]) { rq <- r }
	arm := make(chan error, 1)
	go func() { arm <- w.Arm(queue) }()
	old := <-started
	if err := w.Arm(queue); err != iox.ErrWaitUnavailable || !w.Withdraw() {
		t.Fatal("concurrent arm or withdrawal", err)
	}
	var fresh iox.Signal[int]
	g2 := stepTrigger[int](func(_ iox.Ticket, s iox.Signal[int]) error { fresh = s; return nil })
	_, _, next := slot.Await(iox.Ticket{Demand: 2}, g2, done).Next()
	if err := next.Arm(queue); err != nil {
		t.Fatal(err)
	}
	close(leave)
	if err := <-arm; err != registrationErr || old.Deliver(7) || !fresh.Deliver(8) {
		t.Fatal("old registration changed successor", err)
	}
	s, ok := (<-rq).TryResume()
	if n, _, _ := s.Next(); !ok || n != 8 {
		t.Fatal("wrong reply", n)
	}
}

func TestWaitRacingClaims(t *testing.T) {
	ticket := iox.Ticket{Demand: 4, Attempt: 2}
	var wake iox.Signal[int]
	source := stepTrigger[int](func(got iox.Ticket, signal iox.Signal[int]) error {
		if got != ticket {
			t.Fatalf("trigger got ticket %v, want %v", got, ticket)
		}
		wake = signal
		return nil
	})
	step := iox.Await(ticket, source, func(n int) iox.Step[int] { return iox.Finish(n, nil) })
	_, _, wait := step.Next()
	if got := wait.Ticket(); got != ticket {
		t.Fatalf("wait got ticket %v, want %v", got, ticket)
	}
	queue := make(chan iox.Ready[int], 2)
	if err := wait.Arm(func(r iox.Ready[int]) { queue <- r }); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	accepted := make(chan int, 2)
	for value := range 2 {
		workers.Go(func() {
			if wake.Deliver(value) {
				accepted <- value
			}
		})
	}
	workers.Wait()
	if len(accepted) != 1 || len(queue) != 1 || wait.Withdraw() {
		t.Fatal("expected one accepted payload and one ready handle")
	}
	ready := <-queue
	resumed := make(chan int, 2)
	for range 2 {
		workers.Go(func() {
			if next, ok := ready.TryResume(); ok {
				value, _, _ := next.Next()
				resumed <- value
			}
		})
	}
	workers.Wait()
	if len(resumed) != 1 || <-resumed != <-accepted {
		t.Fatal("expected one resumption with the accepted payload")
	}
}

func TestStepZeroValues(t *testing.T) {
	var s iox.Step[int]
	var wake iox.Signal[int]
	var ready iox.Ready[int]
	var w iox.Wait[int]
	if _, seen := s.Observation(); seen || wake.Deliver(1) || w.Withdraw() || w.Arm(nil) != iox.ErrWaitUnavailable {
		t.Fatal("zero handle accepted work")
	}
	if _, ok := ready.TryResume(); ok {
		t.Fatal("zero ready resumed")
	}
	if v, err, pending := s.Next(); v != 0 || err != nil || !pending.IsZero() {
		t.Fatal("zero step", v, err, pending)
	}
	if o, seen := s.WithObservation(iox.Observation{}).Observation(); !seen || o != (iox.Observation{}) {
		t.Fatal("missing real zero observation")
	}
}

func TestStepPreservesObservation(t *testing.T) {
	failure := errors.New("write failed")
	terminal := iox.Finish(17, failure)
	raw := iox.Observation{N: 3, Err: iox.ErrMore}
	step := terminal.WithObservation(raw)
	if _, seen := terminal.Observation(); seen {
		t.Fatal("recording an observation changed the original step")
	}
	if o, seen := step.Observation(); !seen || o != raw {
		t.Fatalf("observation (%v, %v), want %v", o, seen, raw)
	}
	if n, err, wait := step.Next(); n != 17 || err != failure || !wait.IsZero() {
		t.Fatalf("terminal result (%d, %v), wait %v", n, err, wait)
	}
}

func TestStepOwnershipMisuse(t *testing.T) {
	var slot iox.WaitSlot[int, int]
	source := stepTrigger[int](func(iox.Ticket, iox.Signal[int]) error { return nil })
	finish := func(x int) iox.Step[int] { return iox.Finish(x, nil) }
	first := slot.Await(iox.Ticket{}, source, finish)
	for _, tc := range []struct {
		name string
		f    func()
	}{
		{"occupied slot", func() { slot.Await(iox.Ticket{}, source, finish) }},
		{"second observation", func() { first.WithObservation(iox.Observation{}).WithObservation(iox.Observation{}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			tc.f()
		})
	}
	_, _, w := first.Next()
	if !w.Withdraw() {
		t.Fatal("lost wait after rejected use")
	}
}

type probeSource struct{ wake iox.Signal[struct{}] }

func (p *probeSource) Arm(_ iox.Ticket, s iox.Signal[struct{}]) error { p.wake = s; return nil }

type readyQueue struct{ ready iox.Ready[int] }

func (q *readyQueue) enqueue(r iox.Ready[int]) { q.ready = r }
func finishProbe(struct{}) iox.Step[int]       { return iox.Finish(1, nil) }

func reusableTurn() func() {
	var slot iox.WaitSlot[struct{}, int]
	var source probeSource
	var q readyQueue
	enqueue := q.enqueue
	var ticket iox.Ticket
	return func() {
		ticket.Attempt++
		_, _, wait := slot.Await(ticket, &source, finishProbe).Next()
		if err := wait.Arm(enqueue); err != nil || !source.wake.Deliver(struct{}{}) {
			panic("missing admission")
		}
		step, claimed := q.ready.TryResume()
		if n, _, _ := step.Next(); !claimed || n != 1 {
			panic("missing receipt")
		}
	}
}

func TestWaitSlotAllocations(t *testing.T) {
	if got := testing.AllocsPerRun(1000, reusableTurn()); got != 0 {
		t.Errorf("provisioned turn allocations %v", got)
	}
}

func BenchmarkWaitSlotWithdraw(b *testing.B) {
	b.Run("Empty", benchmarkWaitSlotWithdraw[struct{}])
	b.Run("Inline64KiB", benchmarkWaitSlotWithdraw[[64 << 10]byte])
}

func benchmarkWaitSlotWithdraw[X any](b *testing.B) {
	var slot iox.WaitSlot[X, int]
	var ticket iox.Ticket
	trigger := stepTrigger[X](func(iox.Ticket, iox.Signal[X]) error { return nil })
	finish := func(X) iox.Step[int] { return iox.Finish(0, nil) }
	b.ReportAllocs()
	for b.Loop() {
		ticket.Attempt++
		_, _, wait := slot.Await(ticket, trigger, finish).Next()
		if !wait.Withdraw() {
			b.Fatal("could not withdraw pending wait")
		}
	}
}
