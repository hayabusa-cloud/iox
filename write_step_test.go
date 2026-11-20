// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package iox_test

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"code.hybscloud.com/iox"
)

type stepWriter func([]byte) (int, error)

func (f stepWriter) Write(p []byte) (int, error) { return f(p) }

func TestWriteStepRacingWakes(t *testing.T) {
	var calls []string
	block := fmt.Errorf("socket: %w", iox.ErrWouldBlock)
	write := stepWriter(func(p []byte) (int, error) {
		calls = append(calls, string(p))
		if len(calls) == 1 {
			return 1, block
		}
		return len(p), nil
	})
	ticket := iox.Ticket{Demand: 9, Attempt: 3}
	var wake iox.Signal[struct{}]
	gate := stepTrigger[struct{}](func(key iox.Ticket, signal iox.Signal[struct{}]) error {
		if key != ticket {
			t.Fatalf("registered ticket %v", key)
		}
		wake = signal
		return nil
	})
	s := iox.WriteStep(write, []byte("AB"), ticket, gate)
	o, seen := s.Observation()
	_, _, wait := s.Next()
	if !seen || o.N != 1 || o.Err != block || wait.IsZero() || wait.Ticket() != ticket {
		t.Fatalf("first boundary %+v, wait %v", o, wait)
	}
	q := make(chan iox.Ready[int64], 2)
	if err := wait.Arm(func(r iox.Ready[int64]) { q <- r }); err != nil {
		t.Fatal(err)
	}

	// An independent live route delivers a present, empty frame while the writer is parked.
	frame := iox.Await(iox.Ticket{Demand: 10}, stepTrigger[[]byte](func(_ iox.Ticket, signal iox.Signal[[]byte]) error {
		signal.Deliver([]byte{})
		return nil
	}), func(p []byte) iox.Step[[]byte] {
		return iox.Finish(p, nil).WithObservation(iox.Observation{N: int64(len(p)), Err: iox.ErrMore})
	})
	_, _, frameWait := frame.Next()
	var frameReady iox.Ready[[]byte]
	if err := frameWait.Arm(func(r iox.Ready[[]byte]) { frameReady = r }); err != nil {
		t.Fatal(err)
	}
	f, claimed := frameReady.TryResume()
	payload, err, pending := f.Next()
	fo, seen := f.Observation()
	if !claimed || err != nil || !pending.IsZero() || payload == nil || len(payload) != 0 ||
		!seen || fo.N != 0 || fo.Err != iox.ErrMore || len(calls) != 1 || len(q) != 0 {
		t.Fatal("independent frame delivery or parked writer", fo, calls)
	}

	accepted := make(chan bool, 2)
	for range 2 { // readiness and the retry timer
		go func() { accepted <- wake.Deliver(struct{}{}) }()
	}
	if a, b := <-accepted, <-accepted; a == b || len(q) != 1 || wait.Withdraw() {
		t.Fatal("one queued owner required", a, b, len(q))
	}
	r := <-q
	type result struct {
		step iox.Step[int64]
		ok   bool
	}
	results := make(chan result, 2)
	for range 2 { // allow different workers to see copies of the ready handle
		go func() { next, ok := r.TryResume(); results <- result{next, ok} }()
	}
	a, b := <-results, <-results
	if a.ok == b.ok {
		t.Fatalf("resume claims %v, %v", a.ok, b.ok)
	}
	if !a.ok {
		a = b
	}
	n, err, next := a.step.Next()
	o, seen = a.step.Observation()
	if n != 2 || err != nil || !next.IsZero() || !seen || o != (iox.Observation{N: 1}) ||
		len(calls) != 2 || calls[0] != "AB" || calls[1] != "B" {
		t.Fatalf("result %d %v, observation %+v, writes %v", n, err, o, calls)
	}
}

func TestWriteStepTerminalObservations(t *testing.T) {
	failed := errors.New("connection lost")
	for _, tc := range []struct {
		name      string
		n         int
		raw, want error
		total     int64
	}{
		{"complete", 2, nil, nil, 2},
		{"short", 1, nil, iox.ErrShortWrite, 1},
		{"failure", 1, failed, failed, 1},
		{"more", 0, iox.ErrMore, iox.ErrMore, 0},
		{"last byte with signal", 2, iox.ErrWouldBlock, iox.ErrWouldBlock, 2},
		{"invalid with failure", -1, failed, failed, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := iox.WriteStep(stepWriter(func([]byte) (int, error) { return tc.n, tc.raw }), []byte("ab"), iox.Ticket{}, nil)
			n, err, wait := s.Next()
			o, seen := s.Observation()
			if !wait.IsZero() || n != tc.total || err != tc.want || !seen || o != (iox.Observation{N: int64(tc.n), Err: tc.raw}) {
				t.Fatalf("result (%d, %v), wait %v, observation %+v", n, err, wait, o)
			}
		})
	}
	s := iox.WriteStep(stepWriter(func([]byte) (int, error) { return 3, nil }), []byte("ab"), iox.Ticket{}, nil)
	o, seen := s.Observation()
	if n, err, w := s.Next(); n != 0 || err == nil || !w.IsZero() || !seen || o != (iox.Observation{N: 3}) {
		t.Fatal("excess count", n, err, w, o)
	}
}

func TestWriteStepLastAttempt(t *testing.T) {
	block := fmt.Errorf("send queue: %w", iox.ErrWouldBlock)
	for _, tc := range []struct {
		name      string
		n         int
		raw, want error
	}{
		{"partial", 1, block, iox.ErrWriteAttemptExhausted},
		{"blocked", 0, block, iox.ErrWriteAttemptExhausted},
		{"accepted under pressure", 2, block, block},
		{"complete", 2, nil, nil},
		{"more", 1, iox.ErrMore, iox.ErrMore},
		{"short", 1, nil, iox.ErrShortWrite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			write := stepWriter(func(p []byte) (int, error) {
				if want := "abcd"[calls:]; string(p) != want {
					t.Fatalf("call %d wrote %q, want %q", calls, p, want)
				}
				calls++
				if calls < 3 {
					return 1, block
				}
				return tc.n, tc.raw
			})
			var source probeSource
			ticket := iox.Ticket{Demand: 7, Attempt: math.MaxUint64 - 1}
			step := iox.WriteStep(write, []byte("abcd"), ticket, &source)
			probe := nextWriteProbe(&step, &source)
			for _, attempt := range []uint64{math.MaxUint64 - 1, math.MaxUint64} {
				_, _, wait := step.Next()
				if want := (iox.Ticket{Demand: ticket.Demand, Attempt: attempt}); wait.IsZero() || wait.Ticket() != want {
					t.Fatalf("wait ticket %+v, want %+v", wait.Ticket(), want)
				}
				probe()
			}
			n, err, wait := step.Next()
			obs, seen := step.Observation()
			if n != 2+int64(tc.n) || err != tc.want || !wait.IsZero() || calls != 3 ||
				!seen || obs != (iox.Observation{N: int64(tc.n), Err: tc.raw}) {
				t.Fatalf("result (%d, %v), wait %v, calls %d, observation %+v", n, err, wait, calls, obs)
			}
		})
	}
}

func TestWriteStepSynchronousRegistrationAndReuse(t *testing.T) {
	both := errors.Join(iox.ErrWouldBlock, iox.ErrMore)
	failure := errors.New("transport closed")
	calls := 0
	write := stepWriter(func(p []byte) (int, error) {
		if want := []string{"abc", "bc", "bc"}[calls]; string(p) != want {
			t.Fatalf("call %d wrote %q, want %q", calls, p, want)
		}
		calls++
		switch calls {
		case 1:
			return 1, both
		case 2:
			return 0, iox.ErrWouldBlock
		default:
			return 1, failure
		}
	})
	first := iox.Ticket{Demand: 7, Attempt: 11}
	arms := 0
	gate := stepTrigger[struct{}](func(ticket iox.Ticket, s iox.Signal[struct{}]) error {
		want := iox.Ticket{Demand: first.Demand, Attempt: first.Attempt + uint64(arms)}
		arms++
		if ticket != want || !s.Deliver(struct{}{}) || s.Deliver(struct{}{}) {
			t.Fatalf("double signal or wrong ticket %v, want %v", ticket, want)
		}
		return nil
	})
	var consume func(iox.Step[int64])
	consume = func(s iox.Step[int64]) {
		o, ok := s.Observation()
		want := []iox.Observation{{N: 1, Err: both}, {Err: iox.ErrWouldBlock}, {N: 1, Err: failure}}[calls-1]
		if !ok || o != want {
			t.Fatalf("call %d observation %+v, want %+v", calls, o, want)
		}
		n, err, w := s.Next()
		if w.IsZero() {
			if n != 2 || err != failure {
				t.Fatalf("terminal (%d, %v)", n, err)
			}
			return
		}
		if err := w.Arm(func(r iox.Ready[int64]) {
			next, ok := r.TryResume()
			if !ok {
				t.Fatal("lost synchronous reply")
			}
			consume(next)
		}); err != nil {
			t.Fatal(err)
		}
	}
	consume(iox.WriteStep(write, []byte("abc"), first, gate))
	if calls != 3 || arms != 2 {
		t.Fatal(calls, arms)
	}
	empty := iox.WriteStep(stepWriter(func(p []byte) (int, error) {
		if len(p) != 0 {
			t.Fatal("unexpected payload")
		}
		return 0, nil
	}), nil, first, nil)
	if o, seen := empty.Observation(); !seen || o != (iox.Observation{}) {
		t.Fatalf("empty write %+v", o)
	}
}

type acceptWrite struct{}

func (acceptWrite) Write(p []byte) (int, error) { return len(p), nil }

func TestWriteStepImmediateAllocations(t *testing.T) {
	p := []byte("ab")
	if got := testing.AllocsPerRun(1000, func() {
		s := iox.WriteStep(acceptWrite{}, p, iox.Ticket{}, nil)
		if n, _, _ := s.Next(); n != 2 {
			panic("short write")
		}
	}); got != 0 {
		t.Errorf("immediate write allocations %v", got)
	}
}

type countedWriter struct{ calls int }

func (w *countedWriter) Write(p []byte) (int, error) {
	w.calls++
	return len(p), nil
}

func BenchmarkWriteStep(b *testing.B) {
	p := []byte("ab")
	var dst iox.Writer = &countedWriter{}
	gate := new(probeSource)
	blocked := stepWriter(func([]byte) (int, error) { return 0, iox.ErrWouldBlock })
	b.Run("Direct", func(b *testing.B) {
		for b.Loop() {
			if n, err := dst.Write(p); n != len(p) || err != nil {
				b.Fatal(n, err)
			}
		}
	})
	b.Run("Immediate", func(b *testing.B) {
		for b.Loop() {
			n, err, w := iox.WriteStep(dst, p, iox.Ticket{}, nil).Next()
			if n != int64(len(p)) || err != nil || !w.IsZero() {
				b.Fatal(n, err)
			}
		}
	})
	b.Run("FirstWait", func(b *testing.B) {
		for b.Loop() {
			_, _, w := iox.WriteStep(blocked, p, iox.Ticket{}, gate).Next()
			w.Withdraw()
		}
	})
	b.Run("Retry", func(b *testing.B) {
		step := iox.WriteStep(blocked, p, iox.Ticket{}, gate)
		probe := nextWriteProbe(&step, gate)
		for b.Loop() {
			probe()
		}
	})
}

// Pre-bind the driver's enqueue function so each turn measures only the
// registration, wake, and resumed write.
func nextWriteProbe(step *iox.Step[int64], source *probeSource) func() {
	var ready iox.Ready[int64]
	enqueue := func(r iox.Ready[int64]) { ready = r }
	return func() {
		_, _, wait := step.Next()
		if err := wait.Arm(enqueue); err != nil || !source.wake.Deliver(struct{}{}) {
			panic("missing write signal")
		}
		var ok bool
		*step, ok = ready.TryResume()
		if !ok {
			panic("lost write")
		}
	}
}

func TestWriteStepProvisionedRetries(t *testing.T) {
	var writable bool
	write := stepWriter(func(p []byte) (int, error) {
		if writable {
			return len(p), nil
		}
		return 0, iox.ErrWouldBlock
	})
	var source probeSource
	step := iox.WriteStep(write, []byte("ab"), iox.Ticket{Demand: 6}, &source)
	probe := nextWriteProbe(&step, &source)
	if n := testing.AllocsPerRun(1000, probe); n != 0 {
		t.Errorf("successive write retry allocations %v", n)
	}
	writable = true
	probe()
	if n, err, w := step.Next(); n != 2 || err != nil || !w.IsZero() {
		t.Fatalf("completed write (%d, %v)", n, err)
	}
}

func TestWriteStepWithContinuouslyReadyFrames(t *testing.T) {
	work := make(chan iox.Ready[int64], 2)
	enqueue := func(r iox.Ready[int64]) { work <- r }
	var slot iox.WaitSlot[[]byte, int64]
	var frames frameSource
	var sequence uint64
	var nextFrame func([]byte) iox.Step[int64]
	waitFrame := func() iox.Step[int64] {
		sequence++
		return slot.Await(iox.Ticket{Demand: 1, Attempt: sequence}, &frames, nextFrame)
	}
	nextFrame = func(p []byte) iox.Step[int64] {
		if p == nil {
			t.Fatal("missing frame")
		}
		return waitFrame().WithObservation(iox.Observation{N: int64(len(p)), Err: iox.ErrMore})
	}
	armFrame := func(w iox.Wait[int64]) {
		if err := w.Arm(enqueue); err != nil || !frames.next.Deliver([]byte{}) {
			t.Fatalf("frame admission: %v", err)
		}
	}
	_, _, w := waitFrame().Next()
	armFrame(w)

	var source probeSource
	probes := 0
	dst := stepWriter(func(p []byte) (int, error) {
		probes++
		if probes == 1 {
			return 1, iox.ErrWouldBlock
		}
		if string(p) != "B" {
			t.Fatalf("retried %q", p)
		}
		return len(p), nil
	})
	_, _, writer := iox.WriteStep(dst, []byte("AB"), iox.Ticket{Demand: 2}, &source).Next()
	if err := writer.Arm(enqueue); err != nil {
		t.Fatal(err)
	}
	const wakeTurn = 16
	var committed, delivered int
	for turn := 1; turn <= 32; turn++ {
		if turn == wakeTurn {
			if probes != 1 || !source.wake.Deliver(struct{}{}) {
				t.Fatal("parked writer probed or lost wake")
			}
		}
		ready := <-work
		step, claimed := ready.TryResume()
		if !claimed {
			t.Fatal("stale runnable")
		}
		n, err, wait := step.Next()
		if wait.IsZero() {
			if n != 2 || err != nil || committed != 0 {
				t.Fatalf("write receipt (%d, %v) on turn %d", n, err, turn)
			}
			committed = turn
			continue
		}
		o, seen := step.Observation()
		if !seen || o != (iox.Observation{Err: iox.ErrMore}) {
			t.Fatalf("empty frame %+v", o)
		}
		delivered++
		if turn < 32 {
			armFrame(wait)
		} else if !wait.Withdraw() {
			t.Fatal("outstanding stream wait")
		}
	}
	if committed != wakeTurn+1 || probes != 2 || delivered != 31 || len(work) != 0 {
		t.Fatalf("commit turn %d, probes %d, frames %d, queue %d", committed, probes, delivered, len(work))
	}
}
